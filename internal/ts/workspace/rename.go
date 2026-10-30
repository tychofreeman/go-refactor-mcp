package workspace

import (
	"context"

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
	edits, refusal = w.merged(path, func(p *tsapi.Project) ([]tsapi.Edit, string) {
		es, why, e := p.Rename(path, offset, newName)
		if e != nil {
			err = e
			return nil, e.Error()
		}
		return es, why
	})
	if err != nil {
		return nil, "", err
	}
	return edits, refusal, nil
}

// KeyedUses checks renaming the member at offset in the file at path in
// every project that has the file (tsapi.Project.KeyedUses): why it could
// change what code that finds properties by name does, and the calls that
// print a value of its type.
func (w *Workspace) KeyedUses(path string, offset int) (refusal string, printed []string) {
	seen := map[string]bool{}
	for _, p := range w.projects {
		if !p.Has(path) {
			continue
		}
		why, ps := p.KeyedUses(context.Background(), path, offset)
		if why != "" {
			return why, nil
		}
		for _, pr := range ps {
			if !seen[pr] {
				seen[pr] = true
				printed = append(printed, pr)
			}
		}
	}
	return "", printed
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
