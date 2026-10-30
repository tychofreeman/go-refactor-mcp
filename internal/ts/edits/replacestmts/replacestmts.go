// Package replacestmts is replace_statements for TypeScript: an edit that
// replaces whole statements of one block, or a function's whole body.
package replacestmts

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

// ReplaceStatements replaces statements with new ones.
type ReplaceStatements struct{}

// New returns replace_statements.
func New() *ReplaceStatements { return &ReplaceStatements{} }

func (*ReplaceStatements) ChangesBehavior() {}

func (*ReplaceStatements) Name() string { return "replace_statements" }

func (*ReplaceStatements) Description() string {
	return `Replace whole statements of one block with new TypeScript statements: an edit, to write a function's body (a stub becomes the implementation) or change what statements do. With function "Bed.plant" and content this.plants.push(p);, plant's whole body becomes that statement.
Give start_line and end_line (inclusive, as inspect numbers them) covering whole statements of one block, comments and blank lines included; or function (name, or Class.member, an arrow function bound to a const too) for the whole body, even an empty one, which stays valid after earlier steps; with case too ("fern", or default), the body of that case of its switch. content is re-indented to the block; empty content deletes the statements. wrap instead puts them, as they are, inside new statements, at the line $body: wrap "if (ok) {\n$body\n}".
Refuses, saying why: lines that cut a statement (naming the lines that would not); content that is not statements, or does not type-check; a wrap that leaves a name the statements declare out of scope where it is used after them.`
}

func (*ReplaceStatements) InputSchema() json.RawMessage {
	return refactor.Schema[replaceStatementsArgs]()
}

// replaceStatementsArgs are the arguments of replace_statements.
type replaceStatementsArgs struct {
	File      string  `json:"file" desc:"Relative to the root."`
	Function  string  `json:"function,omitempty" desc:"Instead of the lines: the function (name or Class.member) whose body to replace."`
	Case      string  `json:"case,omitempty" desc:"With function: replace the body of this case of its switch instead (\"fern\", or default)."`
	StartLine int     `json:"start_line,omitempty" desc:"First line (1-based)."`
	EndLine   int     `json:"end_line,omitempty" desc:"Last line (inclusive)."`
	Content   *string `json:"content,omitempty" desc:"The new statements."`
	Wrap      string  `json:"wrap,omitempty" desc:"Instead of content: statements around the ones given, with $body on a line of its own where they go."`
}

func (*ReplaceStatements) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a replaceStatementsArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.Function != "" && (a.StartLine != 0 || a.EndLine != 0) {
		return nil, refactor.Refusef("give either function or start_line and end_line, not both")
	}
	switch {
	case a.Content != nil && a.Wrap != "":
		return nil, refactor.Refusef("give content or wrap, not both")
	case a.Content == nil && a.Wrap == "":
		return nil, refactor.Refusef("give content, or wrap to put the statements inside new ones")
	}
	path := ws.Abs(a.File)
	if _, err := os.Stat(path); err != nil {
		return nil, refactor.Refusef("%s: no such file", a.File)
	}
	if a.Case != "" {
		// A case's body: its lines, then as lines.
		if a.Function == "" {
			return nil, refactor.Refusef("give the function whose switch has the case")
		}
		first, last, why := ws.CaseLines(path, a.Function, a.Case, true)
		if why != "" {
			return nil, refactor.Refusef("%s", why)
		}
		a.Function, a.StartLine, a.EndLine = "", first, last
	}
	if a.Wrap != "" {
		return wrap(ws, path, a.Function, a.StartLine, a.EndLine, a.Wrap)
	}
	content := *a.Content
	empty := strings.TrimSpace(content) == ""
	var n int
	var multiline bool
	if !empty {
		var why string
		if n, multiline, why = tsapi.ParseStatements(content); why != "" {
			return nil, refactor.Refusef("%s", why)
		}
	}
	plan := &refactor.Plan{Supplied: content}
	var e edit.TextEdit
	if a.Function != "" {
		var d *tsapi.Decl
		for _, x := range ws.Outline(path) {
			if x.Name == a.Function {
				d = &x
				break
			}
		}
		if d == nil || d.BlockEnd == 0 {
			return nil, refactor.Refusef("%s declares no function %q with a body in braces; give name, or Class.member for a method", a.File, a.Function)
		}
		pl, why := ws.Body(path, d.BlockStart, d.BlockEnd)
		if why != "" {
			return nil, refactor.Refusef("%s", why)
		}
		e = edit.TextEdit{Start: pl.At, End: pl.End}
		if !empty {
			e.NewText = pl.Prefix + gocode.Reindent(content, pl.Indent, multiline) + pl.Suffix
		}
		plan.Summary = fmt.Sprintf("replaced the body of %s in %s with %d statement%s", a.Function, ws.Rel(path), n, textutil.Plural(n))
	} else {
		start, end, indent, old, why := ws.Statements(path, a.StartLine, a.EndLine)
		if why != "" {
			return nil, refactor.Refusef("%s", why)
		}
		e = edit.TextEdit{Start: start, End: end}
		if !empty {
			e.NewText = gocode.Reindent(content, indent, multiline) + "\n"
		}
		plan.Summary = fmt.Sprintf("replaced lines %d-%d (%d statement%s) of %s", a.StartLine, a.EndLine, old, textutil.Plural(old), ws.Rel(path))
	}
	plan.Edits = []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{e}}}
	return plan, nil
}

// wrap puts the statements of lines first-last, or the whole body of
// function, at the line $body of template.
func wrap(ws *workspace.Workspace, path, function string, first, last int, template string) (*refactor.Plan, error) {
	if function != "" {
		d := declaration(ws, path, function)
		if d == nil {
			return nil, refactor.Refusef("%s declares no function %q with a body in braces; give name, or Class.member for a method", ws.Rel(path), function)
		}
		var why string
		if first, last, why = ws.BodyLines(path, d.BlockStart, d.BlockEnd); why != "" {
			return nil, refactor.Refusef("%s", why)
		}
	}
	start, end, text, notes, why := ws.Wrap(path, first, last, template)
	if why != "" {
		return nil, refactor.Refusef("%s", why)
	}
	return &refactor.Plan{
		Summary: fmt.Sprintf("wrapped lines %d-%d of %s", first, last, ws.Rel(path)),
		Edits:   []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{Start: start, End: end, NewText: text}}}},
		Notes:   notes,
	}, nil
}

// declaration returns the function (name, or Class.member) of the file at
// path that has a body in braces, or nil.
func declaration(ws *workspace.Workspace, path, function string) *tsapi.Decl {
	for _, x := range ws.Outline(path) {
		if x.Name == function && x.BlockEnd != 0 {
			return &x
		}
	}
	return nil
}
