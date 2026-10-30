package workspace

import (
	"context"

	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
)

// Stubs returns a stub for each of names that the file at path uses and
// nothing declares, or why one cannot be written; results gives types the
// uses do not tell (tsapi.Project.Stubs).
func (w *Workspace) Stubs(path string, names []string, results map[string]string) ([]tsapi.Stub, string) {
	p := w.project(path)
	if p == nil {
		return nil, w.none(path)
	}
	return p.Stubs(context.Background(), path, names, results)
}

// ImportedFrom reports whether the file at path imports name, and from
// which file of the solution ("" for a module outside it).
func (w *Workspace) ImportedFrom(path, name string) (string, bool) {
	p := w.project(path)
	if p == nil {
		return "", false
	}
	return p.ImportedFrom(context.Background(), path, name)
}
