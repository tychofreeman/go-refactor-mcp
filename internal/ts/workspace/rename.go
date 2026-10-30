package workspace

import (
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
)

// Identifiers returns the identifiers of the file at the absolute path,
// from the first project that has it, or nil.
func (w *Workspace) Identifiers(path string) []tsapi.Ident {
	for _, p := range w.projects {
		if p.Has(path) {
			return p.Identifiers(path)
		}
	}
	return nil
}

// TopLevel returns the offset of the name of the file's top-level
// declaration called name.
func (w *Workspace) TopLevel(path, name string) (int, bool) {
	for _, p := range w.projects {
		if p.Has(path) {
			return p.TopLevel(path, name)
		}
	}
	return 0, false
}

// Local returns the offset of the name of the declaration called name in
// the function fn of the file at path (tsapi.Project.Local), or why there
// is none.
func (w *Workspace) Local(path, fn, name string) (int, string) {
	p := w.project(path)
	if p == nil {
		return 0, w.none(path)
	}
	return p.Local(path, fn, name)
}

// Rename renames the symbol at offset in the file at path to newName in
// every project that has the file, and returns the edits, each once: a
// test file only one project has is renamed too. refusal is why it
// cannot, when it cannot.
func (w *Workspace) Rename(path string, offset int, newName string) (edits []edit.FileEdit, refusal string, err error) {
	byFile := map[string][]edit.TextEdit{}
	seen := map[tsapi.Edit]bool{}
	var order []string
	found := false
	for _, p := range w.projects {
		if !p.Has(path) {
			continue
		}
		found = true
		es, why, err := p.Rename(path, offset, newName)
		if err != nil {
			return nil, "", err
		}
		if why != "" {
			return nil, why, nil
		}
		for _, e := range es {
			if seen[e] || !w.Contains(e.File) {
				continue
			}
			seen[e] = true
			if byFile[e.File] == nil {
				order = append(order, e.File)
			}
			byFile[e.File] = append(byFile[e.File], edit.TextEdit{Start: e.Start, End: e.End, NewText: e.NewText})
		}
	}
	if !found {
		return nil, w.Rel(path) + " is in none of the solution's projects (" + joinComma(w.Projects()) + ")", nil
	}
	for _, f := range order {
		edits = append(edits, edit.FileEdit{Path: f, Edits: byFile[f]})
	}
	return edits, "", nil
}

func joinComma(s []string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += ", "
		}
		out += x
	}
	return out
}
