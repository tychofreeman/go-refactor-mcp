// Package replaceexpr is replace_expression for TypeScript: an edit that
// replaces an expression, or every expression of a file that means the
// same, with a new one.
package replaceexpr

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

// ReplaceExpression replaces expressions.
type ReplaceExpression struct{}

// New returns replace_expression.
func New() *ReplaceExpression { return &ReplaceExpression{} }

func (*ReplaceExpression) ChangesBehavior() {}

func (*ReplaceExpression) Name() string { return "replace_expression" }

func (*ReplaceExpression) Description() string {
	return `Replace an expression with a new one: an edit, to change part of a statement without restating it. Replacing p.height with p.height * 2 on its line changes that read alone. With all_occurrences, every expression in the file that means the same is replaced: the same tokens, each name referring to the same thing, so this.plants in another method of Bed matches and a parameter p of another function does not.
Identify the expression by file, the line it starts on and its source text (whitespace may differ; column if the text starts on the line twice). new is parenthesized where its place needs it (by becomes (by + 1) in h - by).`
}

func (*ReplaceExpression) InputSchema() json.RawMessage {
	return refactor.Schema[replaceExpressionArgs]()
}

// replaceExpressionArgs are the arguments of replace_expression.
type replaceExpressionArgs struct {
	File           string `json:"file" desc:"Relative to the root."`
	Line           int    `json:"line" desc:"1-based line on which the expression starts."`
	Expression     string `json:"expression" desc:"Source text of the expression to replace."`
	Column         int    `json:"column,omitempty" desc:"Optional 1-based column where it starts, if the text starts on the line more than once."`
	New            string `json:"new" desc:"The new expression."`
	AllOccurrences bool   `json:"all_occurrences,omitempty" desc:"Also replace every other expression in the file that means the same."`
}

func (*ReplaceExpression) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a replaceExpressionArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	if _, err := os.Stat(path); err != nil {
		return nil, refactor.Refusef("%s: no such file", a.File)
	}
	fe, n, why := ws.ReplaceExpression(path, a.Line, a.Expression, a.Column, a.New, a.AllOccurrences)
	if why != "" {
		return nil, refactor.Refusef("%s", why)
	}
	return &refactor.Plan{
		Summary: fmt.Sprintf("replaced %d occurrence%s of %s with %s in %s", n, textutil.Plural(n), strings.TrimSpace(a.Expression), strings.TrimSpace(a.New), ws.Rel(path)),
		Edits:   []edit.FileEdit{fe},
	}, nil
}
