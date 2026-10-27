package workspace

import (
	"context"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
)

// project is the first project that has the file at the absolute path, or
// nil.
func (w *Workspace) project(path string) *tsapi.Project {
	for _, p := range w.projects {
		if p.Has(path) {
			return p
		}
	}
	return nil
}

func (w *Workspace) none(path string) string {
	return w.Rel(path) + " is in none of the solution's projects (" + joinComma(w.Projects()) + ")"
}

// Statements returns the bytes of lines first-last of the file at path
// when they hold whole statements of one block, with their indentation
// and how many there are; refusal says why they do not.
func (w *Workspace) Statements(path string, first, last int) (start, end int, indent string, n int, refusal string) {
	p := w.project(path)
	if p == nil {
		return 0, 0, "", 0, w.none(path)
	}
	return p.Statements(path, first, last)
}

// Body returns where the statements of the function body whose braces are
// at [blockStart, blockEnd) go.
func (w *Workspace) Body(path string, blockStart, blockEnd int) (tsapi.Place, string) {
	p := w.project(path)
	if p == nil {
		return tsapi.Place{}, w.none(path)
	}
	return p.Body(path, blockStart, blockEnd)
}

// Insertion returns where statements go before, after or into what is on
// line of the file at path.
func (w *Workspace) Insertion(path string, line int, where string) (tsapi.Place, string) {
	p := w.project(path)
	if p == nil {
		return tsapi.Place{}, w.none(path)
	}
	return p.Insertion(path, line, where)
}

// ReplaceExpression returns the edit that replaces the expression expr
// starting on line (at column, if not 0) of the file at path with
// newText, and with all every expression of the file that means the
// same; n is how many it replaces.
func (w *Workspace) ReplaceExpression(path string, line int, expr string, column int, newText string, all bool) (fe edit.FileEdit, n int, refusal string) {
	p := w.project(path)
	if p == nil {
		return edit.FileEdit{}, 0, w.none(path)
	}
	es, refusal := p.ReplaceExpression(context.Background(), path, line, expr, column, newText, all)
	fe.Path = path
	for _, e := range es {
		fe.Edits = append(fe.Edits, edit.TextEdit{Start: e.Start, End: e.End, NewText: e.NewText})
	}
	return fe, len(es), refusal
}
