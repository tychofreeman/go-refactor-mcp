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
	"hash/fnv"
	"io/fs"
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
	Root     string   // module root (absolute); only files under it are edited
	Base     string   // directory the caller pointed at; relative paths resolve here first
	Patterns []string // package patterns relative to Root, e.g. "./..."

	Fset *token.FileSet
	// Pkgs holds every loaded package, including test variants
	// ("p [p.test]", "p_test"). A file may therefore appear in more than one
	// package; refactorings must deduplicate by file position.
	Pkgs []*packages.Package

	undo []Change
	fp   uint64 // fingerprint of the module's files at the last load
}

// Refresh reloads the workspace if any Go source, go.mod, go.sum or go.work
// file under the root was added, removed or modified since the last load.
// A long-running server must call it before every operation: the model (or
// a person) may edit files directly between tool calls, and edits computed
// against stale syntax trees would land at the wrong byte offsets.
func (w *Workspace) Refresh() (reloaded bool, err error) {
	fp, err := fingerprint(w.Root)
	if err != nil {
		return false, err
	}
	if fp == w.fp {
		return false, nil
	}
	return true, w.Reload()
}

// fingerprint hashes the path, size and modification time of every file
// that affects package loading. Walking a module this way is far cheaper
// than re-type-checking it.
func fingerprint(root string) (uint64, error) {
	h := fnv.New64a()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
				name == "testdata" || name == "vendor" || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") && name != "go.mod" && name != "go.sum" && name != "go.work" {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil // vanished during the walk; the next refresh will see it
		}
		fmt.Fprintf(h, "%s\x00%d\x00%d\n", p, fi.Size(), fi.ModTime().UnixNano())
		return nil
	})
	return h.Sum64(), err
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

// Load loads and type-checks the module containing dir, including tests.
//
// The root is always the enclosing module (the nearest directory with a
// go.mod), not dir itself: a rename must see every package that might
// reference the symbol, or it would silently miss those references and the
// post-change type check would not notice. Patterns (default "./...") are
// relative to the module root. Relative file paths given to Abs are
// resolved against dir first, so callers can keep using paths relative to
// where they pointed the tool.
func Load(dir string, patterns ...string) (*Workspace, error) {
	base, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	root := moduleRoot(base)
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	w := &Workspace{Root: root, Base: base, Patterns: patterns}
	if err := w.Reload(); err != nil {
		return nil, err
	}
	return w, nil
}

// Reload re-reads and re-type-checks all packages from disk.
func (w *Workspace) Reload() error {
	// Fingerprint before loading: if a file changes during the load, the
	// next Refresh sees a different fingerprint and loads again.
	fp, err := fingerprint(w.Root)
	if err != nil {
		return err
	}
	w.fp = fp
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

// Abs resolves a path given relative to Base (where the caller pointed the
// tool) or, failing that, to the module root. Absolute paths are cleaned.
func (w *Workspace) Abs(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	if w.Base != "" && w.Base != w.Root {
		if p := filepath.Join(w.Base, path); fileExists(p) {
			return p
		}
	}
	return filepath.Join(w.Root, path)
}

// moduleRoot returns the nearest ancestor of dir (inclusive) containing a
// go.mod, or dir itself if there is none.
func moduleRoot(dir string) string {
	for d := dir; ; {
		if fileExists(filepath.Join(d, "go.mod")) {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return dir
		}
		d = parent
	}
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
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
