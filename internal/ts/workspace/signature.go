package workspace

import (
	"context"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
)

// ChangeSignature returns the edits that make ch to the function or
// method called name in the file at path, at its declaration and every
// call, in every project that has the file, each edit once; refusal is
// why it cannot be made.
func (w *Workspace) ChangeSignature(path, name string, ch tsapi.SignatureChange) ([]edit.FileEdit, string) {
	byFile := map[string][]edit.TextEdit{}
	seen := map[tsapi.Edit]bool{}
	var order []string
	found := false
	for _, p := range w.projects {
		if !p.Has(path) {
			continue
		}
		found = true
		es, why := p.ChangeSignature(context.Background(), path, name, ch)
		if why != "" {
			return nil, why
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
		return nil, w.none(path)
	}
	var out []edit.FileEdit
	for _, f := range order {
		out = append(out, edit.FileEdit{Path: f, Edits: byFile[f]})
	}
	return out, ""
}
