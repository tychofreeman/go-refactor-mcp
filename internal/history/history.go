package history

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
)

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
		out = append(out, Rel(root, p))
	}
	sort.Strings(out)
	return out
}

// DiffContext is the change's diff, paths relative to root, with n lines
// of context instead of Diff's three.
func (c Change) DiffContext(root string, n int) string {
	var diffs []string
	for p := range c.Before {
		diffs = append(diffs, edit.UnifiedContext(Rel(root, p), c.Before[p], c.After[p], n))
	}
	sort.Strings(diffs)
	return strings.Join(diffs, "")
}

// Restore puts back the files as they were before the change, removing
// the ones it created, and the directories it created if they are empty.
func (c Change) Restore() error {
	before := map[string][]byte{}
	for p, data := range c.Before {
		if !c.Created[p] {
			before[p] = data
			// A deleted file's directory may have gone with it.
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
		} else if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := writeAll(before); err != nil {
		return err
	}
	// The directories it created go last, and only if empty: one that has
	// since gained another file stays, with that file.
	for i := len(c.Dirs) - 1; i >= 0; i-- {
		if entries, err := os.ReadDir(c.Dirs[i]); err == nil && len(entries) > 0 {
			continue
		}
		if err := os.Remove(c.Dirs[i]); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// AddDirs records the directories from dir up that do not exist yet,
// outermost first. A path in the way that is not a directory is an error.
func (c *Change) AddDirs(dir string) error {
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

// Write writes the files the change leaves and deletes those it deletes,
// with the directories that leaves empty.
func (c Change) Write() error {
	after := map[string][]byte{}
	for p, data := range c.After {
		if !c.Deleted[p] {
			after[p] = data
		} else if err := os.Remove(p); err != nil {
			return err
		}
	}
	if err := writeAll(after); err != nil {
		return err
	}
	for p := range c.Deleted {
		if err := removeEmpty(filepath.Dir(p)); err != nil {
			return err
		}
	}
	return nil
}

// removeEmpty removes dir if it is empty, and then each directory above
// it that this leaves empty.
func removeEmpty(dir string) error {
	for {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return nil // gone already, or still in use
		}
		if err := os.Remove(dir); err != nil {
			return err
		}
		dir = filepath.Dir(dir)
	}
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

func Rel(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(r, "..") {
		return filepath.ToSlash(r)
	}
	return p
}
