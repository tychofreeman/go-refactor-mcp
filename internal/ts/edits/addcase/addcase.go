// Package addcase is add_case for TypeScript: an edit that adds clauses to a switch, or branches to an if chain.
package addcase

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

// AddCase is add_case.
type AddCase struct{}

// New returns add_case.
func New() *AddCase { return &AddCase{} }

func (*AddCase) ChangesBehavior() {}

func (*AddCase) Name() string { return "add_case" }

func (*AddCase) Description() string {
	return `Add case clauses to a switch, or branches to an if chain: an edit, to handle a new value. At the line of switch (p.kind) {, content case "moss": return 1; adds that clause at the end of the switch (before a default that ends it, and the cases that fall into it); at the line of one of its clauses, where "before" or "after" (the default) puts them next to it. content is one or more clauses, each case x: or default: with its statements, re-indented. For the statements inside a clause, use insert_statements or replace_statements.
Branches of an if, likewise: at the line of if (p.height > 10) {, content else if (p.height > 5) { return 2; } goes at the end of its chain (before its else); at the line of an else if, before or after it. else { ... } goes last, once.`
}

func (*AddCase) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":    {"type": "string",  "description": "The file, relative to the root."},
    "line":    {"type": "integer", "description": "1-based line of the switch or if (they go at its end), or of one of its clauses or else ifs."},
    "where":   {"type": "string",  "enum": ["before", "after"], "description": "For the line of a clause or else if: before or after it (default after)."},
    "content": {"type": "string",  "description": "One or more clauses, each case x: or default: with its statements; or branches, else if (c) { ... } or else { ... }."}
  },
  "required": ["file", "line", "content"],
  "additionalProperties": false
}`)
}

func (*AddCase) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File    string `json:"file"`
		Line    int    `json:"line"`
		Where   string `json:"where"`
		Content string `json:"content"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	if _, err := os.Stat(path); err != nil {
		return nil, refactor.Refusef("%s: no such file", a.File)
	}
	edits, why := ws.Edits(path, func(p *tsapi.Project) ([]tsapi.Edit, string) { return p.AddCase(path, a.Line, a.Where, a.Content) })
	if why != "" {
		return nil, refactor.Refusef("%s", why)
	}
	return &refactor.Plan{Summary: fmt.Sprintf("added to the code on line %d", a.Line), Edits: edits}, nil
}
