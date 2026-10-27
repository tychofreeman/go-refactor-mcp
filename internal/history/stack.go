package history

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
)

// Stack is the undo stack: the changes applied to the files, the last
// on top. Its zero value is empty and ready to use.
type Stack struct {
	changes []Change
}

// Push puts c on top of the stack.
func (s *Stack) Push(c Change) { s.changes = append(s.changes, c) }

// Len is the number of changes on the stack.
func (s *Stack) Len() int { return len(s.changes) }

// Last returns the change on top of the stack: the one applied last.
func (s *Stack) Last() (Change, bool) {
	if len(s.changes) == 0 {
		return Change{}, false
	}
	return s.changes[len(s.changes)-1], true
}

// Squash replaces the last n changes with one, labelled label, that
// undoes them all, and returns it; its Diff goes from before the first to
// after the last, with paths relative to root.
func (s *Stack) Squash(n int, label, root string) (Change, error) {
	if n < 1 || n > len(s.changes) {
		return Change{}, fmt.Errorf("cannot squash %d changes: the undo stack holds %d", n, len(s.changes))
	}
	c := Change{Label: label, Before: map[string][]byte{}, After: map[string][]byte{}, Created: map[string]bool{}, Deleted: map[string]bool{}}
	for _, part := range s.changes[len(s.changes)-n:] {
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
	c.Diff = c.DiffContext(root, 3)
	s.changes = append(s.changes[:len(s.changes)-n], c)
	return c, nil
}

// Undo puts the files back as they were before the change on top, takes
// it off the stack, and returns its inverse, with paths in its diff
// relative to root. It refuses if a file was modified on disk since the
// change was applied, and then returns no change.
func (s *Stack) Undo(root string) (Change, error) {
	if len(s.changes) == 0 {
		return Change{}, fmt.Errorf("nothing to undo")
	}
	c := s.changes[len(s.changes)-1]
	for p, want := range c.After {
		if c.Deleted[p] {
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return Change{}, fmt.Errorf("cannot undo %q: %s was created again since", c.Label, Rel(root, p))
			}
			continue
		}
		got, err := os.ReadFile(p)
		if err != nil {
			return Change{}, err
		}
		if !bytes.Equal(got, want) {
			return Change{}, fmt.Errorf("cannot undo %q: %s was modified since", c.Label, Rel(root, p))
		}
	}
	if err := c.Restore(); err != nil {
		return Change{}, err
	}
	s.changes = s.changes[:len(s.changes)-1]
	var diffs []string
	for p := range c.Before {
		diffs = append(diffs, edit.Unified(Rel(root, p), c.After[p], c.Before[p]))
	}
	sort.Strings(diffs)
	return Change{Label: "undo " + c.Label, Diff: strings.Join(diffs, ""), Before: c.After, After: c.Before, Created: c.Deleted, Deleted: c.Created}, nil
}

// maxSaved bounds the changes Save keeps, the oldest dropped first: each
// holds the whole contents of the files it touched.
const maxSaved = 50

// Save writes the stack to path, its last maxSaved changes, so that
// another process working on the same files can undo them after Load.
func (s *Stack) Save(path string) error {
	stack := s.changes
	if len(stack) > maxSaved {
		stack = stack[len(stack)-maxSaved:]
	}
	b, err := json.Marshal(stack)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Load replaces the stack with the one Save wrote to path; a missing file
// is an empty stack.
func (s *Stack) Load(path string) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		s.changes = nil
		return nil
	}
	if err != nil {
		return err
	}
	var stack []Change
	if err := json.Unmarshal(b, &stack); err != nil {
		return fmt.Errorf("the undo stack in %s: %v", path, err)
	}
	s.changes = stack
	return nil
}
