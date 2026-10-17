package changesig

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"slices"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// MoveParameter is the "move_parameter" refactoring.
type MoveParameter struct{}

// NewMoveParameter returns the move_parameter refactoring.
func NewMoveParameter() *MoveParameter { return &MoveParameter{} }

func (*MoveParameter) Name() string { return "move_parameter" }

func (*MoveParameter) Description() string {
	return `Move a parameter of a function or method to another position, and move its argument to match at every call in the workspace.
Use it to prepare a feature change, for example to put parameters in the order a new caller or an interface wants.
Identify the function by file, line and name, at its declaration or any call. Give the parameter to move and optionally before: the parameter it should go just before (default: the end, or just before a variadic parameter).
Moving an argument changes the order in which a call evaluates its arguments, so this is refused at any call where that order could matter: the moved argument and one it passes over where one has side effects and the other reads a variable, or both could have side effects or panic.
Refuses if the function is used other than by calling it (as a function value or method expression), if it is an interface method, if a method would stop or start implementing an interface the program can see (which could silently change a type assertion), if the parameter is variadic, or if a moved argument contains another call of the function.`
}

func (*MoveParameter) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":      {"type": "string",  "description": "Path of a file containing the function's declaration or a call, relative to the workspace root."},
    "line":      {"type": "integer", "description": "1-based line of that occurrence."},
    "name":      {"type": "string",  "description": "The function's name (for a method, just the method name)."},
    "column":    {"type": "integer", "description": "Optional 1-based column, only needed to disambiguate."},
    "parameter": {"type": "string",  "description": "Name of the parameter to move."},
    "before":    {"type": "string",  "description": "Optional: the parameter it should go just before. Default: the end (before a variadic parameter)."}
  },
  "required": ["file", "line", "name", "parameter"],
  "additionalProperties": false
}`)
}

type moveArgs struct {
	File      string `json:"file"`
	Line      int    `json:"line"`
	Column    int    `json:"column,omitempty"`
	Name      string `json:"name"`
	Parameter string `json:"parameter"`
	Before    string `json:"before,omitempty"`
}

// Plan checks preconditions and computes the edits.
func (*MoveParameter) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a moveArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	t, err := findTarget(ws, locate.IdentQuery{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name})
	if err != nil {
		return nil, err
	}
	from, err := t.paramIndex(a.Parameter)
	if err != nil {
		return nil, err
	}
	if from == len(t.params)-1 && t.obj.Signature().Variadic() {
		return nil, refactor.Refusef("%s is variadic and must stay last", a.Parameter)
	}
	to, err := t.insertIndex(a.Before)
	if err != nil {
		return nil, err
	}
	if to == from || to == from+1 {
		return nil, refactor.Refusef("%s is already there: nothing to move", a.Parameter)
	}

	if err := t.checkInterfaces(func(ps []string) []string {
		k := ps[from]
		ps = slices.Delete(ps, from, from+1)
		if to > from {
			return slices.Insert(ps, to-1, k)
		}
		return slices.Insert(ps, to, k)
	}); err != nil {
		return nil, err
	}
	p := t.params[from]
	fes := []edit.FileEdit{t.paramCut(p).fileEdit(), {Path: t.ws.Fset.Position(t.decl.Pos()).Filename, Edits: []edit.TextEdit{t.paramEdit(to, p.name, t.text(p.field.Type))}}}
	srcs := map[string][]byte{}
	for _, c := range t.calls {
		if err := checkNoSpread(c, t.ws.Position(c.expr.Pos())); err != nil {
			return nil, err
		}
		moved := c.expr.Args[from]
		if err := t.checkOrder(c, from, to); err != nil {
			return nil, err
		}
		if err := t.checkNoCallIn(moved); err != nil {
			return nil, err
		}
		path := t.ws.Fset.Position(moved.Pos()).Filename
		if srcs[path] == nil {
			if srcs[path], err = os.ReadFile(path); err != nil {
				return nil, err
			}
		}
		text := string(srcs[path][t.off(moved.Pos()):t.off(moved.End())])
		fes = append(fes, t.argCut(c.expr, from, false).fileEdit(), edit.FileEdit{Path: path, Edits: []edit.TextEdit{t.argEdit(c.expr, to, text)}})
	}
	return t.newPlan(fmt.Sprintf("moved parameter %q of %s", a.Parameter, t.obj.Name()), fes), nil
}

// checkOrder refuses a call where moving the argument at index from to
// index to could change what the call does: the arguments it passes over
// are then evaluated in a different order relative to it.
func (t *target) checkOrder(c call, from, to int) error {
	args := c.expr.Args
	var passed []ast.Expr
	if to < from {
		passed = args[to:from]
	} else {
		passed = args[from+1 : min(to, len(args))]
	}
	info := c.pkg.TypesInfo
	moved := args[from]
	for _, b := range passed {
		why := ""
		switch {
		case hasEffect(info, moved) && readsVar(info, b), hasEffect(info, b) && readsVar(info, moved):
			why = "one has side effects the other could observe"
		case unsafeArg(info, moved) && unsafeArg(info, b):
			why = "both could have side effects or panic"
		}
		if why != "" {
			return refactor.Refusef("at %s, moving %s past %s would change the order in which they are evaluated, and %s; assign one of them to a variable before the call first",
				t.ws.Position(c.expr.Pos()), types.ExprString(moved), types.ExprString(b), why)
		}
	}
	return nil
}

// checkNoCallIn refuses an argument that contains another call of the
// target, whose own edit would have to move with it.
func (t *target) checkNoCallIn(arg ast.Expr) error {
	a := t.ws.Fset.Position(arg.Pos())
	end := t.off(arg.End())
	for _, d := range t.calls {
		if p := t.ws.Fset.Position(d.expr.Pos()); p.Filename == a.Filename && a.Offset <= p.Offset && p.Offset < end {
			return refactor.Refusef("the argument %s at %s contains another call of %s, which is not supported yet; assign it to a variable before the call first",
				types.ExprString(arg), t.ws.Position(arg.Pos()), t.obj.Name())
		}
	}
	return nil
}

func unsafeArg(info *types.Info, e ast.Expr) bool {
	bad, _ := unsafeValue(info, e)
	return bad != nil
}

// hasEffect reports whether evaluating e could have a side effect.
// (Defining a function literal runs nothing.)
func hasEffect(info *types.Info, e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok || found {
			return false
		}
		found = analysis.HasEffect(info, n)
		return !found
	})
	return found
}

// readsVar reports whether evaluating e reads a variable, whose value a
// side effect could change. (A function literal captures variables rather
// than reading them.)
func readsVar(info *types.Info, e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok || found {
			return false
		}
		if id, ok := n.(*ast.Ident); ok {
			_, found = info.Uses[id].(*types.Var)
		}
		return !found
	})
	return found
}
