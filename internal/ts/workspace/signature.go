package workspace

import (
	"context"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
)

// ChangeSignature returns the edits that make ch to the function or
// method called name in the file at path, at its declaration and every
// call; refusal is why it cannot be made.
func (w *Workspace) ChangeSignature(path, name string, ch tsapi.SignatureChange) ([]edit.FileEdit, string) {
	return w.merged(path, func(p *tsapi.Project) ([]tsapi.Edit, string) {
		return p.ChangeSignature(context.Background(), path, name, ch)
	})
}

// ReplacedNote says why adding a parameter that replaces a variable is
// not proven to keep behavior (tsapi.Project.ReplacedNote), in the first
// project that has the file.
func (w *Workspace) ReplacedNote(path, name, param, value string) string {
	for _, p := range w.projects {
		if p.Has(path) {
			return p.ReplacedNote(context.Background(), path, name, param, value)
		}
	}
	return ""
}

// FillLiterals returns the edits that give the property name the value
// value in every object literal of the type typeName that lacks it
// (tsapi.Project.FillLiterals).
func (w *Workspace) FillLiterals(path, typeName, name, value string) ([]edit.FileEdit, string) {
	return w.merged(path, func(p *tsapi.Project) ([]tsapi.Edit, string) {
		return p.FillLiterals(context.Background(), path, typeName, name, value)
	})
}

// IntroduceParameter returns the edits that replace expr in the body of
// the function called name with a new parameter param, which every call
// passes the expression for in its own terms.
func (w *Workspace) IntroduceParameter(path, name, expr, param string) ([]edit.FileEdit, string) {
	return w.merged(path, func(p *tsapi.Project) ([]tsapi.Edit, string) {
		return p.IntroduceParameter(context.Background(), path, name, expr, param)
	})
}

// ParamsToObject returns the edits that bundle the parameters of the
// function called name into one object, at its declaration and every
// call.
func (w *Workspace) ParamsToObject(path, name, typeName string) ([]edit.FileEdit, string) {
	return w.merged(path, func(p *tsapi.Project) ([]tsapi.Edit, string) {
		return p.ParamsToObject(context.Background(), path, name, typeName)
	})
}

// AddResult returns the edits that add a result field to the function
// called name, at its returns and at every call that uses its result.
func (w *Workspace) AddResult(path, name, field, typ, value, kept string) ([]edit.FileEdit, string) {
	return w.merged(path, func(p *tsapi.Project) ([]tsapi.Edit, string) {
		return p.AddResult(context.Background(), path, name, field, typ, value, kept)
	})
}

// Edits returns the edits change makes in every project that has the
// file at path, each once; or the first refusal.
func (w *Workspace) Edits(path string, change func(*tsapi.Project) ([]tsapi.Edit, string)) ([]edit.FileEdit, string) {
	return w.merged(path, change)
}

// merged runs change in every project that has the file at path and
// returns its edits, each once, in files under the root; or the first
// refusal.
func (w *Workspace) merged(path string, change func(*tsapi.Project) ([]tsapi.Edit, string)) ([]edit.FileEdit, string) {
	byFile := map[string][]edit.TextEdit{}
	seen := map[tsapi.Edit]bool{}
	var order []string
	found := false
	for _, p := range w.projects {
		if !p.Has(path) {
			continue
		}
		found = true
		es, why := change(p)
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
