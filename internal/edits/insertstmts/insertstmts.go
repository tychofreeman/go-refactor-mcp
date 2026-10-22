// Package insertstmts implements insert_statements: an edit that inserts
// new Go statements before or after a statement, or into a block.
package insertstmts

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// InsertStatements is the "insert_statements" edit.
type InsertStatements struct{}

// New returns the insert_statements edit.
func New() *InsertStatements { return &InsertStatements{} }

func (*InsertStatements) Name() string { return "insert_statements" }

func (*InsertStatements) Description() string {
	return `Insert new Go statements before or after a whole statement, or at the start of a block: an edit, to add a step to a function or write an empty body. With where "into" and the line of func (c *Cart) Reset() {}, content c.Items = nil writes Reset's body.
where is "before" the statement that starts on line, "after" the statement that ends on it (the outermost, so after a closing brace means after the whole if or loop), or "into" the block whose opening brace is on line (or the case clause whose colon is). content is re-indented to the block; packages it names are imported. To change statements that are there, use replace_statements.`
}

func (*InsertStatements) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":    {"type": "string",  "description": "Path of the file, relative to the workspace root."},
    "line":    {"type": "integer", "description": "1-based line: where a statement starts (before), ends (after), or a block opens (into)."},
    "where":   {"type": "string",  "enum": ["before", "after", "into"], "description": "before or after the statement on line, or into the block opened on line."},
    "content": {"type": "string",  "description": "The new statements, as Go source."},
    "imports": {"type": "array", "items": {"type": "string"}, "description": "Optional: import paths to use for package names the content uses, when the name alone is ambiguous."}
  },
  "required": ["file", "line", "where", "content"],
  "additionalProperties": false
}`)
}

// ChangesBehavior marks this as an edit.
func (*InsertStatements) ChangesBehavior() {}

type args struct {
	File    string   `json:"file"`
	Line    int      `json:"line"`
	Where   string   `json:"where"`
	Content string   `json:"content"`
	Imports []string `json:"imports,omitempty"`
}

// Plan checks preconditions and computes the edits.
func (*InsertStatements) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	pkg, file := ws.FileSyntax(path)
	if file == nil {
		return nil, refactor.Refusef("file %s is not part of the loaded packages", a.File)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	tf := ws.Fset.File(file.Pos())
	if a.Line < 1 || a.Line > tf.LineCount() {
		return nil, refactor.Refusef("line %d out of range: %s has %d lines", a.Line, a.File, tf.LineCount())
	}
	var p *place
	switch a.Where {
	case "before":
		p, err = before(tf, file, src, a.Line)
	case "after":
		p, err = after(tf, file, src, a.Line)
	case "into":
		p, err = into(tf, file, src, a.Line)
	default:
		return nil, refactor.Refusef("where must be before, after or into, not %q", a.Where)
	}
	if err != nil {
		return nil, err
	}
	code, err := gocode.ParseStatements(a.Content)
	if err != nil {
		return nil, err
	}
	if len(code.Body.List) == 0 {
		return nil, refactor.Refusef("content has no statements")
	}
	adder := imports.New(ws, pkg, file, src)
	added, err := gocode.AddImports(ws, pkg, file, adder, p.pos, code, a.Imports)
	if err != nil {
		return nil, err
	}
	n := len(code.Body.List)
	plan := &refactor.Plan{
		Summary:  fmt.Sprintf("inserted %d statement%s %s %d of %s", n, textutil.Plural(n), p.what, a.Line, a.File),
		Edits:    []edit.FileEdit{{Path: path, Edits: append([]edit.TextEdit{{Start: p.at, End: p.end, NewText: p.text(a.Content, code.Raw)}}, adder.Edits()...)}},
		Supplied: a.Content,
	}
	if len(added) > 0 {
		plan.Notes = append(plan.Notes, gocode.ImportNote("added", added))
	}
	return plan, nil
}
