package invertif

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// RemoveElse is the "remove_else" refactoring: when an if branch cannot
// fall through, its else is redundant, and the else branch can follow the
// if, un-nested.
type RemoveElse struct{ refactor.Named[args] }

// NewRemoveElse returns the remove_else refactoring.
func NewRemoveElse() *RemoveElse {
	return &RemoveElse{Named: refactor.Called[args]("remove_else", "keep_else")}
}

func (*RemoveElse) Description() string {
	return `Remove an else after an if branch that cannot fall through, un-nesting it: if err != nil { return 0, err } else { return use(v), nil } becomes if err != nil { return 0, err } followed by return use(v), nil. An else-if becomes a separate if. When it is the else branch that exits, use invert_if instead. With wrap, the inverse: the statements after an if whose branch cannot fall through become its else, each outcome in a branch of its own.
Identify the if by file and the line of its if keyword (column if the line has two).
Refuses, saying why: an if branch that can fall through, un-nested names that would clash.`
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
	if a.Wrap {
		return wrapElse(x, s)
	}
	line := x.TF.Line(s.If)
	if s.Else == nil {
		return nil, refactor.Refusef("the if at line %d has no else", line)
	}
	if !analysis.EndsFlow(x.Info, s.Body.List) {
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
		moved, what = indent+x.Text(e), "the else-if is now a separate if statement"
	}
	ifText := string(x.Src[x.Off(s.Cond.Pos()):x.Off(s.Body.End())])
	text := then(x.head(s, hoist, indent)+ifText, moved)
	var notes []string
	if hoist {
		notes = append(notes, "the if's init statement moved before it, because the code after it uses its variables")
	}
	return &refactor.Plan{
		Summary: fmt.Sprintf("removed the else of the if at line %d: its branch always exits, so %s", line, what),
		Edits:   []edit.FileEdit{{Path: x.path, Edits: []edit.TextEdit{{Start: x.Off(s.Pos()), End: x.Off(s.End()), NewText: text}}}},
		Notes:   notes,
	}, nil
}

// wrapElse plans the inverse of removing an else: the statements after
// s, whose branch cannot fall through, become its else branch.
func wrapElse(x *inversion, s *ast.IfStmt) (*refactor.Plan, error) {
	line := x.TF.Line(s.If)
	switch {
	case s.Else != nil:
		return nil, refactor.Refusef("the if at line %d has an else already", line)
	case !analysis.EndsFlow(x.Info, s.Body.List):
		return nil, refactor.Refusef("the if branch at line %d can fall through to the code after the if, so in an else that code would no longer run after it", line)
	}
	p, err := x.placeOf(s)
	if err != nil {
		return nil, err
	}
	rest := p.list[p.index+1:]
	if len(rest) == 0 {
		return nil, refactor.Refusef("nothing follows the if at line %d in its block", line)
	}
	// In the else, the rest is in the scope of the if's init statement.
	if as, ok := s.Init.(*ast.AssignStmt); ok && as.Tok == token.DEFINE {
		for _, l := range as.Lhs {
			id, ok := l.(*ast.Ident)
			own := x.Info.Defs[id]
			if !ok || own == nil {
				continue
			}
			var hidden *ast.Ident
			for _, r := range rest {
				ast.Inspect(r, func(n ast.Node) bool {
					if u, ok := n.(*ast.Ident); ok && hidden == nil && u.Name == id.Name && x.Info.Uses[u] != nil && x.Info.Uses[u] != own {
						hidden = u
					}
					return hidden == nil
				})
			}
			if hidden != nil {
				return nil, refactor.Refusef("%s at %s would mean the if's own %s in its else; rename one first", hidden.Name, x.WS.Position(hidden.Pos()), id.Name)
			}
		}
	}
	rb, end := x.Off(s.Body.Rbrace), x.Off(rest[len(rest)-1].End())
	return &refactor.Plan{
		Summary: fmt.Sprintf("moved the %d statement%s after the if at line %d into its else", len(rest), textutil.Plural(len(rest)), line),
		Edits:   []edit.FileEdit{{Path: x.path, Edits: []edit.TextEdit{{Start: rb, End: rb + 1, NewText: "} else {"}, {Start: end, End: end, NewText: "\n" + x.indentOf(s.Pos()) + "}"}}}},
	}, nil
}
