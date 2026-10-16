// Package workspace loads a set of Go packages with full type information
// and applies edits to them, keeping an undo stack.
//
// A Workspace is the unit a refactoring session operates on: the model asks
// for one refactoring, the workspace applies it and reloads, and the next
// refactoring sees the updated code.
package workspace

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/cwfreeman/gorefactor/internal/edit"
)

const loadMode = packages.NeedName |
	packages.NeedFiles |
	packages.NeedCompiledGoFiles |
	packages.NeedImports |
	packages.NeedSyntax |
	packages.NeedTypes |
	packages.NeedTypesInfo |
	packages.NeedModule

// Workspace is a loaded, type-checked set of packages rooted at Root.
// It is not safe for concurrent use.
type Workspace struct {
	Root     string   // absolute directory; only files under it are edited
	Patterns []string // package patterns, e.g. "./..."

	Fset *token.FileSet
	// Pkgs holds every loaded package, including test variants
	// ("p [p.test]", "p_test"). A file may therefore appear in more than one
	// package; refactorings must deduplicate by file position.
	Pkgs []*packages.Package

	undo []Change
}

// Change records one applied set of edits, enough to show it and undo it.
type Change struct {
	Label  string
	Diff   string            // unified diff, paths relative to Root
	Before map[string][]byte // absolute path -> contents before the change
	After  map[string][]byte // absolute path -> contents after the change
}

// Files returns the changed paths, relative to root, sorted.
func (c Change) Files(root string) []string {
	var out []string
	for p := range c.After {
		out = append(out, rel(root, p))
	}
	sort.Strings(out)
	return out
}

// Load loads and type-checks the packages matching patterns (default
// "./...") in dir, including tests.
func Load(dir string, patterns ...string) (*Workspace, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	w := &Workspace{Root: root, Patterns: patterns}
	if err := w.Reload(); err != nil {
		return nil, err
	}
	return w, nil
}

// Reload re-reads and re-type-checks all packages from disk.
func (w *Workspace) Reload() error {
	fset := token.NewFileSet()
	cfg := &packages.Config{
		Mode:  loadMode,
		Dir:   w.Root,
		Fset:  fset,
		Tests: true,
	}
	pkgs, err := packages.Load(cfg, w.Patterns...)
	if err != nil {
		return fmt.Errorf("loading packages: %w", err)
	}
	if len(pkgs) == 0 {
		return fmt.Errorf("no Go packages found in %s matching %v", w.Root, w.Patterns)
	}
	w.Fset, w.Pkgs = fset, pkgs
	return nil
}

// Errors returns the deduplicated list/parse/type errors of all loaded
// packages, sorted. Refactorings only run on code that type-checks, and
// must not introduce new errors.
func (w *Workspace) Errors() []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range w.Pkgs {
		// `go list` repeats compile errors as a ListError; prefer the precise
		// parse/type errors when a package has them.
		hasPrecise := false
		for _, e := range p.Errors {
			if e.Kind != packages.ListError {
				hasPrecise = true
			}
		}
		for _, e := range p.Errors {
			if hasPrecise && e.Kind == packages.ListError {
				continue
			}
			msg := e.Error()
			if after, ok := strings.CutPrefix(msg, w.Root+string(filepath.Separator)); ok {
				msg = after
			}
			if !seen[msg] {
				seen[msg] = true
				out = append(out, msg)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Contains reports whether the absolute path is inside the workspace root.
// Files outside it (standard library, module cache) are never edited.
func (w *Workspace) Contains(path string) bool {
	r, err := filepath.Rel(w.Root, path)
	return err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}

// Abs resolves a path given relative to the root (or already absolute).
func (w *Workspace) Abs(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(w.Root, path)
}

// Rel returns path relative to the workspace root, for display.
func (w *Workspace) Rel(path string) string { return rel(w.Root, path) }

// Position formats pos as root-relative "file:line:col".
func (w *Workspace) Position(pos token.Pos) string {
	p := w.Fset.Position(pos)
	if !p.IsValid() {
		return "-"
	}
	return fmt.Sprintf("%s:%d:%d", w.Rel(p.Filename), p.Line, p.Column)
}

// FileSyntax returns the parsed file at the absolute path and the first
// package that contains it.
func (w *Workspace) FileSyntax(path string) (*packages.Package, *ast.File) {
	for _, p := range w.Pkgs {
		for _, f := range p.Syntax {
			if w.Fset.File(f.Pos()).Name() == path {
				return p, f
			}
		}
	}
	return nil, nil
}

// Preview computes the change the edits would make without writing it.
func (w *Workspace) Preview(label string, fes []edit.FileEdit) (Change, error) {
	c := Change{Label: label, Before: map[string][]byte{}, After: map[string][]byte{}}
	var diffs []string
	for _, fe := range edit.Merge(fes) {
		if !w.Contains(fe.Path) {
			return Change{}, fmt.Errorf("refusing to edit %s: outside workspace %s", fe.Path, w.Root)
		}
		before, err := os.ReadFile(fe.Path)
		if err != nil {
			return Change{}, err
		}
		after, err := edit.Apply(before, fe.Edits)
		if err != nil {
			return Change{}, fmt.Errorf("%s: %w", w.Rel(fe.Path), err)
		}
		after = gofmtIfClean(fe.Path, before, after)
		if bytes.Equal(before, after) {
			continue
		}
		c.Before[fe.Path], c.After[fe.Path] = before, after
		diffs = append(diffs, edit.Unified(w.Rel(fe.Path), before, after))
	}
	sort.Strings(diffs)
	c.Diff = strings.Join(diffs, "")
	return c, nil
}

// Apply writes the edits to disk, pushes the change on the undo stack and
// reloads the workspace.
func (w *Workspace) Apply(label string, fes []edit.FileEdit) (Change, error) {
	c, err := w.Preview(label, fes)
	if err != nil {
		return Change{}, err
	}
	if err := writeAll(c.After); err != nil {
		// Best effort to restore what was already written.
		_ = writeAll(c.Before)
		return Change{}, err
	}
	w.undo = append(w.undo, c)
	return c, w.Reload()
}

// CanUndo reports whether there is a change to undo.
func (w *Workspace) CanUndo() bool { return len(w.undo) > 0 }

// Undo reverts the most recent change and reloads. It refuses if a file
// was modified on disk since the change was applied.
func (w *Workspace) Undo() (Change, error) {
	if len(w.undo) == 0 {
		return Change{}, fmt.Errorf("nothing to undo")
	}
	c := w.undo[len(w.undo)-1]
	for p, want := range c.After {
		got, err := os.ReadFile(p)
		if err != nil {
			return Change{}, err
		}
		if !bytes.Equal(got, want) {
			return Change{}, fmt.Errorf("cannot undo %q: %s was modified since", c.Label, w.Rel(p))
		}
	}
	if err := writeAll(c.Before); err != nil {
		return Change{}, err
	}
	w.undo = w.undo[:len(w.undo)-1]
	var diffs []string
	for p := range c.Before {
		diffs = append(diffs, edit.Unified(w.Rel(p), c.After[p], c.Before[p]))
	}
	sort.Strings(diffs)
	inverse := Change{Label: "undo " + c.Label, Diff: strings.Join(diffs, ""), Before: c.After, After: c.Before}
	return inverse, w.Reload()
}

// gofmtIfClean formats the edited file, but only if the original was
// already gofmt-clean; otherwise a one-identifier change could produce a
// whole-file diff. Edits that change identifier lengths can misalign struct
// fields and comments, which gofmt fixes.
func gofmtIfClean(path string, before, after []byte) []byte {
	if !strings.HasSuffix(path, ".go") {
		return after
	}
	if fb, err := format.Source(before); err != nil || !bytes.Equal(fb, before) {
		return after
	}
	if fa, err := format.Source(after); err == nil {
		return fa
	}
	return after
}

func writeAll(files map[string][]byte) error {
	for p, data := range files {
		mode := os.FileMode(0o644)
		if fi, err := os.Stat(p); err == nil {
			mode = fi.Mode().Perm()
		}
		if err := os.WriteFile(p, data, mode); err != nil {
			return err
		}
	}
	return nil
}

func rel(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(r, "..") {
		return filepath.ToSlash(r)
	}
	return p
}
