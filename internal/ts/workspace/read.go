package workspace

import (
	"sort"

	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
)

// Outline returns the declarations of the file at the absolute path, from
// the first project that has it, or nil.
func (w *Workspace) Outline(path string) []tsapi.Decl {
	for _, p := range w.projects {
		if p.Has(path) {
			return p.Outline(path)
		}
	}
	return nil
}

// Has reports whether a project of the solution has the file at the
// absolute path.
func (w *Workspace) Has(path string) bool {
	for _, p := range w.projects {
		if p.Has(path) {
			return true
		}
	}
	return false
}

// Files returns the source files of every project under the root, each
// once, sorted.
func (w *Workspace) Files() []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range w.projects {
		for _, f := range p.Files(w.Root) {
			if !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
	}
	sort.Strings(out)
	return out
}
