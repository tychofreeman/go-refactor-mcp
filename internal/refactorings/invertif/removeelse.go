package invertif

import (
	"encoding/json"
	"fmt"
	"go/ast"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// RemoveElse is the "remove_else" refactoring: when an if branch cannot
// fall through, its else is redundant, and the else branch can follow the
// if, un-nested.
type RemoveElse struct{}

// NewRemoveElse returns the remove_else refactoring.
func NewRemoveElse() *RemoveElse { return &RemoveElse{} }

func (*RemoveElse) Name() string { return "remove_else" }

func (*RemoveElse) Description() string {
	return `Remove a redundant else: when the if branch cannot fall through (it ends in return, continue, break, goto or panic), "if c { X; return } else { B }" becomes "if c { X; return }" followed by B, un-nested. An else-if becomes a separate if statement.
Use it to flatten code after an early exit. (If it is the else branch that exits, use invert_if instead.)
Identify the if by file and the line of its "if" keyword (as shown by inspect); add column only if that line has more than one if.
If the moved statements use variables of the if's init statement, the init statement moves before the if.
Refuses if the if branch can fall through, or if the moved statements (or a moved init statement) declare names that the enclosing block or the code after the if also uses.`
}

func (*RemoveElse) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":   {"type": "string",  "description": "Path of the file, relative to the module root."},
    "line":   {"type": "integer", "description": "1-based line of the if keyword."},
    "column": {"type": "integer", "description": "Optional 1-based column of the if keyword, only needed if the line has more than one if."}
  },
  "required": ["file", "line"],
  "additionalProperties": false
}`)
}

// Plan checks preconditions and computes the edits.
func (*RemoveElse) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.KeepElse {
		return nil, refactor.Refusef("invalid arguments: keep_else is an invert_if argument")
	}
	x, s, err := load(ws, a)
	if err != nil {
		return nil, err
	}
	line := x.tf.Line(s.If)
	if s.Else == nil {
		return nil, refactor.Refusef("the if at line %d has no else", line)
	}
	if !analysis.EndsFlow(x.info, s.Body.List) {
		return nil, refactor.Refusef("the if branch at line %d can fall through to the code after the if, so removing the else would run the else branch in that case too. (If the else branch is the one that exits, use invert_if.)", line)
	}
	p, err := x.placeOf(s)
	if err != nil {
		return nil, err
	}
	if err := x.noCommentsBetween(s); err != nil {
		return nil, err
	}
	indent := x.indentOf(s.Pos())
	var moved, what string
	var hoist bool
	switch e := s.Else.(type) {
	case *ast.BlockStmt:
		if hoist, err = x.checkMove(s, e, p); err != nil {
			return nil, err
		}
		moved, what = x.dedented(e, indent), "the else branch follows, un-nested"
	case *ast.IfStmt:
		// The else-if becomes its own statement at the same depth; its
		// declarations stay in its own scope, but if it uses the first
		// if's init variables, that init statement must move before it.
		if hoist, err = x.prepareInit(s, p, e, nil); err != nil {
			return nil, err
		}
		moved, what = indent+x.text(e), "the else-if is now a separate if statement"
	}
	ifText := string(x.src[x.off(s.Cond.Pos()):x.off(s.Body.End())])
	text := then(x.head(s, hoist, indent)+ifText, moved)
	var notes []string
	if hoist {
		notes = append(notes, "the if's init statement moved before it, because the code after it uses its variables")
	}
	return &refactor.Plan{
		Summary: fmt.Sprintf("removed the else of the if at line %d: its branch always exits, so %s", line, what),
		Edits:   []edit.FileEdit{{Path: x.path, Edits: []edit.TextEdit{{Start: x.off(s.Pos()), End: x.off(s.End()), NewText: text}}}},
		Notes:   notes,
	}, nil
}
