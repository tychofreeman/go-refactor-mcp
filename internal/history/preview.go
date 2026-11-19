package history

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
)

// Preview computes the change the edits would make without writing it,
// in the workspace at root: contains says which files it may edit, rel
// names a file for display, and format formats what is written.
func Preview(label string, fes []edit.FileEdit, contains func(path string) bool, root string, rel func(path string) string, format Format) (Change, error) {
	c := Change{Label: label, Before: map[string][]byte{}, After: map[string][]byte{}, Created: map[string]bool{}, Deleted: map[string]bool{}}
	var diffs []string
	for _, fe := range edit.Merge(fes) {
		if !contains(fe.Path) {
			return Change{}, fmt.Errorf("refusing to edit %s: outside workspace %s", fe.Path, root)
		}
		if err := Outside(root, fe.Path); err != nil {
			return Change{}, fmt.Errorf("refusing to edit %s: %v; only files inside the workspace are edited", rel(fe.Path), err)
		}
		d, err := c.Add(rel(fe.Path), fe, format)
		if err != nil {
			return Change{}, err
		}
		diffs = append(diffs, d)
	}
	sort.Strings(diffs)
	c.Diff = strings.Join(diffs, "")
	return c, nil
}
