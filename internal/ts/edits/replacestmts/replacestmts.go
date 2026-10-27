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
Give start_line and end_line (inclusive, as inspect numbers them) covering whole statements of one block, comments and blank lines included; or function (name, or Class.member, an arrow function bound to a const too) for the whole body, even an empty one, which stays valid after earlier steps. content is re-indented to the block; empty content deletes the statements.
Refuses, saying why: lines that cut a statement (naming the lines that would not); content that is not statements, or does not type-check.`
}

func (*ReplaceStatements) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":       {"type": "string",  "description": "Relative to the root."},
    "function":   {"type": "string",  "description": "Instead of the lines: the function (name or Class.member) whose body to replace."},
    "start_line": {"type": "integer", "description": "First line (1-based)."},
    "end_line":   {"type": "integer", "description": "Last line (inclusive)."},
    "content":    {"type": "string",  "description": "The new statements."}
  },
  "required": ["file", "content"],
  "additionalProperties": false
}`)
}

func (*ReplaceStatements) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File      string `json:"file"`
		Function  string `json:"function"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
		Content   string `json:"content"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.Function != "" && (a.StartLine != 0 || a.EndLine != 0) {
		return nil, refactor.Refusef("give either function or start_line and end_line, not both")
	}
	path := ws.Abs(a.File)
	if _, err := os.Stat(path); err != nil {
		return nil, refactor.Refusef("%s: no such file", a.File)
	}
	empty := strings.TrimSpace(a.Content) == ""
	var n int
	var multiline bool
	if !empty {
		var why string
		if n, multiline, why = tsapi.ParseStatements(a.Content); why != "" {
			return nil, refactor.Refusef("%s", why)
		}
	}
	plan := &refactor.Plan{Supplied: a.Content}
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
			e.NewText = pl.Prefix + gocode.Reindent(a.Content, pl.Indent, multiline) + pl.Suffix
		}
		plan.Summary = fmt.Sprintf("replaced the body of %s in %s with %d statement%s", a.Function, ws.Rel(path), n, textutil.Plural(n))
	} else {
		start, end, indent, old, why := ws.Statements(path, a.StartLine, a.EndLine)
		if why != "" {
			return nil, refactor.Refusef("%s", why)
		}
		e = edit.TextEdit{Start: start, End: end}
		if !empty {
			e.NewText = gocode.Reindent(a.Content, indent, multiline) + "\n"
		}
		plan.Summary = fmt.Sprintf("replaced lines %d-%d (%d statement%s) of %s", a.StartLine, a.EndLine, old, textutil.Plural(old), ws.Rel(path))
	}
	plan.Edits = []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{e}}}
	return plan, nil
}
