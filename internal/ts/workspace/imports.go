package workspace

import (
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
)

// Exporters returns the imports of name that the file at path could
// have: a file other than path that every project of path has, which
// exports name at its top level (not as its default).
func (w *Workspace) Exporters(path, name string) []tsapi.Import {
	var out []tsapi.Import
	for _, f := range w.Files() {
		if f == path || !w.sameProjects(path, f) {
			continue
		}
		for _, d := range w.Outline(f) {
			if d.Name == name && strings.HasPrefix(d.Signature, "export ") && !strings.HasPrefix(d.Signature, "export default") {
				out = append(out, tsapi.Import{Name: name, From: f, TypeOnly: d.Kind == "interface" || d.Kind == "type"})
				break
			}
		}
	}
	return out
}

// sameProjects reports whether every project that has the file at path
// has the file at other too.
func (w *Workspace) sameProjects(path, other string) bool {
	for _, p := range w.projects {
		if p.Has(path) && !p.Has(other) {
			return false
		}
	}
	return true
}

// AddImports returns the edit that imports each of imports into the file
// at path.
func (w *Workspace) AddImports(path string, imports []tsapi.Import) (edit.FileEdit, string) {
	p := w.project(path)
	if p == nil {
		return edit.FileEdit{}, w.none(path)
	}
	es, refusal := p.AddImports(path, imports)
	return FileEdit(path, es), refusal
}

// RemoveUnusedImports returns the edit that removes the imports of the
// file at path that nothing uses.
func (w *Workspace) RemoveUnusedImports(path string) edit.FileEdit {
	p := w.project(path)
	if p == nil {
		return edit.FileEdit{Path: path}
	}
	return FileEdit(path, p.RemoveUnusedImports(path))
}

// FileEdit is the edit of the file at path that es make.
func FileEdit(path string, es []tsapi.Edit) edit.FileEdit {
	fe := edit.FileEdit{Path: path}
	for _, e := range es {
		fe.Edits = append(fe.Edits, edit.TextEdit{Start: e.Start, End: e.End, NewText: e.NewText})
	}
	return fe
}
