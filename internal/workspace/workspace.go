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
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/history"
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
	Base     string   // directory the caller pointed at; a relative path that names no file at the root resolves here
	Patterns []string // package patterns relative to Root, e.g. "./..."

	Fset *token.FileSet
	// Pkgs holds every loaded package, including test variants
	// ("p [p.test]", "p_test"). A file may therefore appear in more than one
	// package; refactorings must deduplicate by file position.
	Pkgs []*packages.Package

	// FullLoads and IncrementalUpdates count how the workspace has been
	// brought up to date, for tests and benchmarks.
	FullLoads, IncrementalUpdates int

	// History holds the changes applied, for undo; workspaces of
	// several languages may share one.
	History *history.Stack

	stamps  map[string]stamp  // size and mtime of each relevant file at the last update
	effects *analysis.Effects // made on first use after each update
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
	err := walkLoadFiles(root, func(p string, d fs.DirEntry) error {
		fi, err := d.Info()
		if err != nil {
			return nil // vanished during the walk; the next refresh will see it
		}
		out[p] = stamp{fi.Size(), fi.ModTime().UnixNano()}
		return nil
	})
	return out, err
}

// walkLoadFiles calls visit for each file under root that affects package
// loading: Go source, go.mod, go.sum and go.work, outside the directories
// go list ignores.
func walkLoadFiles(root string, visit func(path string, d fs.DirEntry) error) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
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
		return visit(p, d)
	})
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
	w := &Workspace{Root: root, Base: base, Patterns: patterns, History: &history.Stack{}}
	if err := w.Reload(); err != nil {
		return nil, err
	}
	return w, nil
}

// Reload re-reads and re-type-checks all packages from disk.
func (w *Workspace) Reload() error {
	w.effects = nil
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
		// Without -trimpath, go's build cache keys a package by its
		// directory, so a module copied to a new path (each A/B run)
		// compiles again: minutes for Hugo, 2 s with it.
		BuildFlags: []string{"-trimpath"},
	}
	pkgs, err := packages.Load(cfg, w.Patterns...)
	if err != nil {
		return fmt.Errorf("loading packages: %w", err)
	}
	if len(pkgs) == 0 {
		return fmt.Errorf("no Go packages found in %s matching %v", w.Root, w.Patterns)
	}
	w.Fset, w.Pkgs, w.stamps = fset, withoutTestMains(pkgs), stamps
	w.FullLoads++
	return nil
}

// withoutTestMains leaves out the main packages go generates to run a
// package's tests ("p.test"): their source is in the build cache, not the
// module, and goes stale when a test is renamed or removed.
func withoutTestMains(pkgs []*packages.Package) []*packages.Package {
	var out []*packages.Package
	for _, p := range pkgs {
		if p.Name == "main" && strings.HasSuffix(p.ID, ".test") {
			continue
		}
		out = append(out, p)
	}
	return out
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

// Abs resolves a path given relative to the module root, as the tools
// print paths, or, when no file is there but one is relative to Base
// (where the caller pointed the tool), that one. A new file is at the
// root. Absolute paths are cleaned.
func (w *Workspace) Abs(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	p := filepath.Join(w.Root, path)
	if !fileExists(p) && w.Base != "" && w.Base != w.Root {
		if b := filepath.Join(w.Base, path); fileExists(b) {
			return b
		}
	}
	return p
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
func (w *Workspace) Rel(path string) string { return history.Rel(w.Root, path) }

// Dir is the module root, Root.
func (w *Workspace) Dir() string { return w.Root }

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

// Effects returns the summaries of what the workspace's code can do, made
// once for each state of the packages: the standard library's functions
// it knows, and the module's own from their bodies.
func (w *Workspace) Effects() *analysis.Effects {
	if w.effects == nil {
		w.effects = analysis.NewEffects()
		for _, p := range w.Pkgs {
			w.effects.AddPackage(p.Syntax, p.TypesInfo)
		}
	}
	return w.effects
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
func (w *Workspace) Preview(label string, fes []edit.FileEdit) (history.Change, error) {
	c := history.Change{Label: label, Before: map[string][]byte{}, After: map[string][]byte{}, Created: map[string]bool{}, Deleted: map[string]bool{}}
	var diffs []string
	for _, fe := range edit.Merge(fes) {
		if !w.Contains(fe.Path) {
			return history.Change{}, fmt.Errorf("refusing to edit %s: outside workspace %s", fe.Path, w.Root)
		}
		before, err := os.ReadFile(fe.Path)
		created := os.IsNotExist(err)
		if created {
			// A new file, in a directory that may have to be created too.
			if err := c.AddDirs(filepath.Dir(fe.Path)); err != nil {
				return history.Change{}, fmt.Errorf("cannot create %s: %w", w.Rel(fe.Path), err)
			}
		} else if err != nil {
			return history.Change{}, err
		}
		if fe.Delete {
			if created {
				return history.Change{}, fmt.Errorf("cannot delete %s: it does not exist", w.Rel(fe.Path))
			}
			c.Deleted[fe.Path] = true
			c.Before[fe.Path], c.After[fe.Path] = before, nil
			diffs = append(diffs, edit.Unified(w.Rel(fe.Path), before, nil))
			continue
		}
		after, err := edit.Apply(before, fe.Edits)
		if err != nil {
			return history.Change{}, fmt.Errorf("%s: %w", w.Rel(fe.Path), err)
		}
		if created {
			c.Created[fe.Path] = true
			// Only Go source: a golden file may parse as Go too.
			if fa, err := format.Source(after); err == nil && strings.HasSuffix(fe.Path, ".go") && !fe.Verbatim {
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

// ErrBreaksLoading is the error of an Apply whose change the module could
// not be loaded with (a broken go.mod, say): the files were put back and
// the change is not on the undo stack.
var ErrBreaksLoading = errors.New("the module would no longer load, so the change was not applied")

// Apply writes the edits to disk, reloads the workspace and pushes the
// change on the undo stack. If the module no longer loads with the change,
// it puts the files back and returns ErrBreaksLoading.
func (w *Workspace) Apply(label string, fes []edit.FileEdit) (history.Change, error) {
	c, err := w.Preview(label, fes)
	if err != nil {
		return history.Change{}, err
	}
	for _, d := range c.Dirs {
		if err := os.Mkdir(d, 0o755); err != nil && !os.IsExist(err) {
			_ = c.Restore()
			return history.Change{}, err
		}
	}
	if err := c.Write(); err != nil {
		// Best effort to restore what was already written.
		_ = c.Restore()
		return history.Change{}, err
	}
	if err := w.update(paths(c.After)); err != nil {
		// The module does not load with the change: put the files back, and
		// the workspace as it was with them.
		if rerr := c.Restore(); rerr != nil {
			w.History.Push(c) // still on disk: undo can try again
			return history.Change{}, fmt.Errorf("%w: %v; putting the files back failed too: %v", ErrBreaksLoading, err, rerr)
		}
		if uerr := w.update(paths(c.Before)); uerr != nil {
			return history.Change{}, fmt.Errorf("%w: %v; the files are back, but reloading them failed: %v", ErrBreaksLoading, err, uerr)
		}
		return history.Change{}, fmt.Errorf("%w: %v", ErrBreaksLoading, err)
	}
	w.History.Push(c)
	return c, nil
}

// CanUndo reports whether there is a change to undo.
func (w *Workspace) CanUndo() bool {
	return w.History.Len() > 0
}

// LastChange returns the change on top of the undo stack: the one applied
// last.
func (w *Workspace) LastChange() (history.Change, bool) {
	return w.History.Last()
}

// Squash replaces the last n changes on the undo stack with one, labelled
// label, that undoes them all, and returns it; its Diff goes from before
// the first to after the last.
func (w *Workspace) Squash(n int, label string) (history.Change, error) {
	return w.History.Squash(n, label, w.Root)
}

// Undo reverts the most recent change and reloads. It refuses if a file
// was modified on disk since the change was applied, and then returns no
// change. If the files are restored but reloading them fails, it returns
// the inverse change with the error.
func (w *Workspace) Undo() (history.Change, error) {
	inverse, err := w.History.Undo(w.Root)
	if err != nil {
		return history.Change{}, err
	}
	return inverse, w.update(paths(inverse.After))
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

// SaveUndo writes the undo stack to path, its last 50 changes,
// so that another process working on the same files can undo them after
// LoadUndo.
func (w *Workspace) SaveUndo(path string) error {
	return w.History.Save(path)
}

// LoadUndo replaces the undo stack with the one SaveUndo wrote to path;
// a missing file is an empty stack.
func (w *Workspace) LoadUndo(path string) error {
	return w.History.Load(path)
}
