// Package workspace loads a TypeScript solution, the projects its root
// tsconfig.json stands for, and applies edits to it, keeping the programs
// up to date and the changes on an undo stack. It is the TypeScript
// counterpart of the Go workspace, for the engine's refactor.Workspace.
package workspace

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/history"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
)

// Workspace is a loaded TypeScript solution rooted at Root.
// It is not safe for concurrent use.
type Workspace struct {
	Root string // the directory of the root tsconfig.json; only files under it are edited
	// History holds the changes applied, for undo; workspaces of several
	// languages may share one.
	History *history.Stack

	projects []*tsapi.Project
	stamps   map[string]stamp // size and mtime of each relevant file at the last update
}

// stamp identifies a version of a file cheaply.
type stamp struct{ size, mtime int64 }

// Load loads the solution of the nearest tsconfig.json at or above dir:
// that config's project, when it names files, and every project it
// references.
func Load(dir string) (*Workspace, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	root := abs
	for !fileExists(filepath.Join(root, "tsconfig.json")) {
		parent := filepath.Dir(root)
		if parent == root {
			return nil, fmt.Errorf("no tsconfig.json at or above %s", abs)
		}
		root = parent
	}
	w := &Workspace{Root: root, History: &history.Stack{}}
	return w, w.reload()
}

// reload loads every project of the solution anew.
func (w *Workspace) reload() error {
	stamps, err := snapshot(w.Root)
	if err != nil {
		return err
	}
	configs, err := tsapi.Solution(filepath.Join(w.Root, "tsconfig.json"))
	if err != nil {
		return err
	}
	var projects []*tsapi.Project
	for _, c := range configs {
		p, err := tsapi.Load(c)
		if err != nil {
			return err
		}
		projects = append(projects, p)
	}
	w.projects, w.stamps = projects, stamps
	return nil
}

// Projects returns the tsconfigs of the solution's projects, relative to
// the root.
func (w *Workspace) Projects() []string {
	var out []string
	for _, p := range w.projects {
		out = append(out, w.Rel(p.Config))
	}
	return out
}

// Refresh brings the workspace up to date with the files on disk, if any
// source file, tsconfig or package.json under the root was added, removed
// or modified since the last update. A long-running server calls it
// before every operation, as for Go.
func (w *Workspace) Refresh() (changed bool, err error) {
	now, err := snapshot(w.Root)
	if err != nil {
		return false, err
	}
	var paths []string
	configs := false
	for p, s := range now {
		if old, ok := w.stamps[p]; !ok || old != s {
			paths = append(paths, p)
		}
	}
	for p := range w.stamps {
		if _, ok := now[p]; !ok {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return false, nil
	}
	for _, p := range paths {
		if !isSource(p) {
			configs = true
		}
	}
	if configs {
		return true, w.reload()
	}
	return true, w.update(paths)
}

// update brings each project up to date after the files at paths changed.
func (w *Workspace) update(paths []string) error {
	for _, p := range w.projects {
		if err := p.Update(paths); err != nil {
			return err
		}
	}
	stamps, err := snapshot(w.Root)
	if err != nil {
		return err
	}
	w.stamps = stamps
	return nil
}

// Errors returns the errors of every project, each once, sorted, with
// paths relative to the root.
func (w *Workspace) Errors() []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range w.projects {
		for _, d := range p.Diagnostics(context.Background(), w.Root) {
			if d.File != "" {
				d.File = w.Rel(d.File)
			}
			s := d.String()
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Dir is the root.
func (w *Workspace) Dir() string { return w.Root }

// Rel returns path relative to the root, for display.
func (w *Workspace) Rel(path string) string { return history.Rel(w.Root, path) }

// Abs resolves a path given relative to the root; absolute paths are
// cleaned.
func (w *Workspace) Abs(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(w.Root, path)
}

// Contains reports whether the absolute path is inside the root and not
// in node_modules: only such files are edited.
func (w *Workspace) Contains(path string) bool {
	rel, err := filepath.Rel(w.Root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../") && !strings.Contains(filepath.ToSlash(rel), "node_modules/")
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
		}
		if bytes.Equal(before, after) && !created {
			continue
		}
		c.Before[fe.Path], c.After[fe.Path] = before, after
		diffs = append(diffs, edit.Unified(w.Rel(fe.Path), before, after))
	}
	sort.Strings(diffs)
	c.Diff = strings.Join(diffs, "")
	return c, nil
}

// Apply writes the edits to disk, updates the projects and pushes the
// change on the undo stack.
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
		_ = c.Restore()
		return history.Change{}, err
	}
	if err := w.update(paths(c.After)); err != nil {
		if rerr := c.Restore(); rerr != nil {
			w.History.Push(c) // still on disk: undo can try again
			return history.Change{}, fmt.Errorf("%v; putting the files back failed too: %v", err, rerr)
		}
		return history.Change{}, err
	}
	w.History.Push(c)
	return c, nil
}

// Undo reverts the most recent change and updates the projects.
func (w *Workspace) Undo() (history.Change, error) {
	inverse, err := w.History.Undo(w.Root)
	if err != nil {
		return history.Change{}, err
	}
	return inverse, w.update(paths(inverse.After))
}

// Squash replaces the last n changes on the undo stack with one.
func (w *Workspace) Squash(n int, label string) (history.Change, error) {
	return w.History.Squash(n, label, w.Root)
}

// LastChange returns the change on top of the undo stack.
func (w *Workspace) LastChange() (history.Change, bool) { return w.History.Last() }

// CanUndo reports whether there is a change to undo.
func (w *Workspace) CanUndo() bool { return w.History.Len() > 0 }

// ToleratesErrors marks the workspace as one whose code may have errors
// before a step, as code written for TypeScript 6 has under 7: a step
// must add none (the user's choice, 2026-10-27, to revisit).
func (*Workspace) ToleratesErrors() {}

func paths(files map[string][]byte) []string {
	out := make([]string, 0, len(files))
	for p := range files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// isSource reports whether path is a TypeScript source file.
func isSource(path string) bool {
	for _, ext := range []string{".ts", ".tsx", ".mts", ".cts"} {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}

// snapshot records the size and modification time of every source file,
// tsconfig*.json and package.json under root, outside node_modules and
// hidden directories.
func snapshot(root string) (map[string]stamp, error) {
	out := map[string]stamp{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (name == "node_modules" || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isSource(p) && name != "package.json" && !(strings.HasPrefix(name, "tsconfig") && strings.HasSuffix(name, ".json")) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out[p] = stamp{info.Size(), info.ModTime().UnixNano()}
		return nil
	})
	return out, err
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}
