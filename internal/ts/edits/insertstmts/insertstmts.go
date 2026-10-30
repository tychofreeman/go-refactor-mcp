// Package insertstmts is insert_statements for TypeScript: an edit that
// inserts statements before or after a statement, or into a block.
package insertstmts

import (
	"encoding/json"
	"fmt"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

// InsertStatements inserts statements.
type InsertStatements struct{}

// New returns insert_statements.
func New() *InsertStatements { return &InsertStatements{} }

func (*InsertStatements) ChangesBehavior() {}

func (*InsertStatements) Name() string { return "insert_statements" }

func (*InsertStatements) Description() string {
	return `Insert new TypeScript statements before or after a whole statement, or at the start of a block: an edit, to add a step to a function or write an empty body. With where "into" and the line of constructor(soil: string) {}, content this.soil = soil; writes the constructor's body.
where is "before" the statement that starts on line, "after" the statement that ends on it (the outermost, so after a closing brace means after the whole if or loop), or "into" the block whose opening brace is on line (or the case clause whose colon is). content is re-indented to the block. To change statements that are there, use replace_statements.`
}

func (*InsertStatements) InputSchema() json.RawMessage {
	return refactor.Schema[insertStatementsArgs]()
}

// insertStatementsArgs are the arguments of insert_statements.
type insertStatementsArgs struct {
	File    string `json:"file" desc:"Relative to the root."`
	Line    int    `json:"line" desc:"1-based line: where a statement starts (before), ends (after), or a block opens (into)."`
	Where   string `json:"where" desc:"before or after the statement on line, or into the block opened on line." enum:"before,after,into"`
	Content string `json:"content" desc:"The new statements."`
}

func (*InsertStatements) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a insertStatementsArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path, p, why := ws.Project(a.File)
	if why != "" {
		return nil, refactor.Refusef("%s", why)
	}
	pl, why := p.Insertion(path, a.Line, a.Where)
	if why != "" {
		return nil, refactor.Refusef("%s", why)
	}
	n, multiline, why := tsapi.ParseStatements(a.Content)
	switch {
	case why != "":
		return nil, refactor.Refusef("%s", why)
	case n == 0:
		return nil, refactor.Refusef("content has no statements")
	}
	text := pl.Prefix + gocode.Reindent(a.Content, pl.Indent, multiline) + pl.Suffix
	return &refactor.Plan{
		Summary:  fmt.Sprintf("inserted %d statement%s %s line %d of %s", n, textutil.Plural(n), a.Where, a.Line, ws.Rel(path)),
		Edits:    []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{Start: pl.At, End: pl.End, NewText: text}}}},
		Supplied: a.Content,
	}, nil
}
