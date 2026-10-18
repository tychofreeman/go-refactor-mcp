// Package workspace loads a set of Go packages with full type information
// and applies edits to them, keeping an undo stack.
//
// A Workspace is the unit a refactoring session operates on: the model asks
// for one refactoring, the workspace applies it and updates, and the next
// refactoring sees the updated code. Updates after edits are incremental
// (see incremental.go): only the changed files are re-parsed and only the
// packages that can be affected are re-type-checked.
package workspace

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
)

const loadMode = packages.NeedName |
	packages.NeedFiles |
	packages.NeedCompiledGoFiles |
	packages.NeedImports |
	packages.NeedSyntax |
	packages.NeedTypes |
	packages.NeedTypesInfo |
	packages.NeedTypesSizes |
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

	// FullLoads and IncrementalUpdates count how the workspace has been
	// brought up to date, for tests and benchmarks.
	FullLoads, IncrementalUpdates int

	undo   []Change
	stamps map[string]stamp // size and mtime of each relevant file at the last update
}

// Refresh brings the workspace up to date with the files on disk, if any
// Go source, go.mod, go.sum or go.work file under the root was added,
// removed or modified since the last update. A long-running server must
// call it before every operation: the model (or a person) may edit files
// directly between tool calls, and edits computed against stale syntax
// trees would land at the wrong byte offsets.
//
// Edits to existing Go files are applied incrementally (see update);
// anything else triggers a full reload.
func (w *Workspace) Refresh() (changed bool, err error) {
	now, err := snapshot(w.Root)
	if err != nil {
		return false, err
	}
	var edited []string
	structural := len(now) != len(w.stamps)
	for p, st := range now {
		old, ok := w.stamps[p]
		switch {
		case !ok:
			structural = true
		case old != st:
			if !strings.HasSuffix(p, ".go") {
				structural = true // go.mod, go.sum or go.work
			}
			edited = append(edited, p)
		}
	}
	switch {
	case structural:
		return true, w.Reload()
	case len(edited) == 0:
		return false, nil
	}
	return true, w.update(edited)
}

// stamp identifies a version of a file cheaply.
type stamp struct {
	size, mtime int64
}

// snapshot records the size and modification time of every file that
// affects package loading. Walking a module this way is far cheaper than
// type-checking it.
func snapshot(root string) (map[string]stamp, error) {
	out := map[string]stamp{}
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
		out[p] = stamp{fi.Size(), fi.ModTime().UnixNano()}
		return nil
	})
	return out, err
}

// Change records one applied set of edits, enough to show it and undo it.
type Change struct {
	Label  string
	Diff   string            // unified diff, paths relative to Root
	Before map[string][]byte // absolute path -> contents before the change
	After  map[string][]byte // absolute path -> contents after the change
	// Created holds the files the change creates: their Before is nil,
	// and undoing the change removes them.
	Created map[string]bool
	// Deleted holds the files the change deletes: their After is nil, and
	// undoing the change writes them back.
	Deleted map[string]bool
	// Dirs holds the directories the change creates for them, each
	// before the ones inside it; undoing the change removes them.
	Dirs []string
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

// DiffContext is the change's diff, paths relative to root, with n lines
// of context instead of Diff's three.
func (c Change) DiffContext(root string, n int) string {
	var diffs []string
	for p := range c.Before {
		diffs = append(diffs, edit.UnifiedContext(rel(root, p), c.Before[p], c.After[p], n))
	}
	sort.Strings(diffs)
	return strings.Join(diffs, "")
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
	// Snapshot before loading: if a file changes during the load, the next
	// Refresh sees a different stamp and updates again.
	stamps, err := snapshot(w.Root)
	if err != nil {
		return err
	}
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
	w.Fset, w.Pkgs, w.stamps = fset, pkgs, stamps
	w.FullLoads++
	return nil
}

// Errors returns the deduplicated list/parse/type errors of all loaded
// packages, sorted. Refactorings only run on code that type-checks, and
// must not introduce new errors.
func (w *Workspace) Errors() []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range w.sourcePackages() {
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

// TypesInfos returns the type information of every loaded package
// variant that has it.
func (w *Workspace) TypesInfos() []*types.Info {
	var out []*types.Info
	for _, p := range w.Pkgs {
		if p.TypesInfo != nil {
			out = append(out, p.TypesInfo)
		}
	}
	return out
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
	c := Change{Label: label, Before: map[string][]byte{}, After: map[string][]byte{}, Created: map[string]bool{}, Deleted: map[string]bool{}}
	var diffs []string
	for _, fe := range edit.Merge(fes) {
		if !w.Contains(fe.Path) {
			return Change{}, fmt.Errorf("refusing to edit %s: outside workspace %s", fe.Path, w.Root)
		}
		before, err := os.ReadFile(fe.Path)
		created := os.IsNotExist(err)
		if created {
			// A new file, in a directory that may have to be created too.
			if err := c.addDirs(filepath.Dir(fe.Path)); err != nil {
				return Change{}, fmt.Errorf("cannot create %s: %w", w.Rel(fe.Path), err)
			}
		} else if err != nil {
			return Change{}, err
		}
		if fe.Delete {
			if created {
				return Change{}, fmt.Errorf("cannot delete %s: it does not exist", w.Rel(fe.Path))
			}
			c.Deleted[fe.Path] = true
			c.Before[fe.Path], c.After[fe.Path] = before, nil
			diffs = append(diffs, edit.Unified(w.Rel(fe.Path), before, nil))
			continue
		}
		after, err := edit.Apply(before, fe.Edits)
		if err != nil {
			return Change{}, fmt.Errorf("%s: %w", w.Rel(fe.Path), err)
		}
		if created {
			c.Created[fe.Path] = true
			if fa, err := format.Source(after); err == nil {
				after = fa
			}
		} else {
			after = gofmtIfClean(fe.Path, before, after)
		}
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
	for _, d := range c.Dirs {
		if err := os.Mkdir(d, 0o755); err != nil && !os.IsExist(err) {
			_ = c.restore()
			return Change{}, err
		}
	}
	if err := c.write(); err != nil {
		// Best effort to restore what was already written.
		_ = c.restore()
		return Change{}, err
	}
	w.undo = append(w.undo, c)
	return c, w.update(paths(c.After))
}

// CanUndo reports whether there is a change to undo.
func (w *Workspace) CanUndo() bool { return len(w.undo) > 0 }

// Squash replaces the last n changes on the undo stack with one, labelled
// label, that undoes them all, and returns it; its Diff goes from before
// the first to after the last.
func (w *Workspace) Squash(n int, label string) (Change, error) {
	if n < 1 || n > len(w.undo) {
		return Change{}, fmt.Errorf("cannot squash %d changes: the undo stack holds %d", n, len(w.undo))
	}
	c := Change{Label: label, Before: map[string][]byte{}, After: map[string][]byte{}, Created: map[string]bool{}, Deleted: map[string]bool{}}
	for _, part := range w.undo[len(w.undo)-n:] {
		for p, before := range part.Before {
			if _, seen := c.Before[p]; !seen {
				c.Before[p], c.Created[p] = before, part.Created[p]
			}
		}
		for p, after := range part.After {
			c.After[p], c.Deleted[p] = after, part.Deleted[p]
		}
		for _, d := range part.Dirs {
			if !slices.Contains(c.Dirs, d) {
				c.Dirs = append(c.Dirs, d)
			}
		}
	}
	// A file a later step put back as it was is not part of the change.
	for p := range c.Before {
		if bytes.Equal(c.Before[p], c.After[p]) && c.Created[p] == c.Deleted[p] {
			delete(c.Before, p)
			delete(c.After, p)
			delete(c.Created, p)
			delete(c.Deleted, p)
		}
	}
	c.Diff = c.DiffContext(w.Root, 3)
	w.undo = append(w.undo[:len(w.undo)-n], c)
	return c, nil
}

// Undo reverts the most recent change and reloads. It refuses if a file
// was modified on disk since the change was applied.
func (w *Workspace) Undo() (Change, error) {
	if len(w.undo) == 0 {
		return Change{}, fmt.Errorf("nothing to undo")
	}
	c := w.undo[len(w.undo)-1]
	for p, want := range c.After {
		if c.Deleted[p] {
			if fileExists(p) {
				return Change{}, fmt.Errorf("cannot undo %q: %s was created again since", c.Label, w.Rel(p))
			}
			continue
		}
		got, err := os.ReadFile(p)
		if err != nil {
			return Change{}, err
		}
		if !bytes.Equal(got, want) {
			return Change{}, fmt.Errorf("cannot undo %q: %s was modified since", c.Label, w.Rel(p))
		}
	}
	if err := c.restore(); err != nil {
		return Change{}, err
	}
	w.undo = w.undo[:len(w.undo)-1]
	var diffs []string
	for p := range c.Before {
		diffs = append(diffs, edit.Unified(w.Rel(p), c.After[p], c.Before[p]))
	}
	sort.Strings(diffs)
	inverse := Change{Label: "undo " + c.Label, Diff: strings.Join(diffs, ""), Before: c.After, After: c.Before, Created: c.Deleted, Deleted: c.Created}
	return inverse, w.update(paths(c.Before))
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

func paths(files map[string][]byte) []string {
	var out []string
	for p := range files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// restore puts back the files as they were before the change, removing
// the ones it created.
func (c Change) restore() error {
	before := map[string][]byte{}
	for p, data := range c.Before {
		if !c.Created[p] {
			before[p] = data
		} else if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	for i := len(c.Dirs) - 1; i >= 0; i-- {
		if err := os.Remove(c.Dirs[i]); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return writeAll(before)
}

// addDirs records the directories from dir up that do not exist yet,
// outermost first. A path in the way that is not a directory is an error.
func (c *Change) addDirs(dir string) error {
	var missing []string
	for d := dir; ; d = filepath.Dir(d) {
		fi, err := os.Stat(d)
		if err == nil {
			if !fi.IsDir() {
				return fmt.Errorf("%s is not a directory", d)
			}
			break
		}
		if !os.IsNotExist(err) {
			return err
		}
		missing = append(missing, d)
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if !slices.Contains(c.Dirs, missing[i]) {
			c.Dirs = append(c.Dirs, missing[i])
		}
	}
	return nil
}

// write writes the files the change leaves and deletes those it deletes.
func (c Change) write() error {
	after := map[string][]byte{}
	for p, data := range c.After {
		if !c.Deleted[p] {
			after[p] = data
		} else if err := os.Remove(p); err != nil {
			return err
		}
	}
	return writeAll(after)
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
