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
	return `Move a parameter to another position, and its argument at every call in the workspace: with parameter to and before from, func copy(from, to string) becomes func copy(to, from string), and copy(a, b) becomes copy(b, a).
Identify the function by file and name, or by line at its declaration or any call; before is the parameter it goes ahead of (default: last).
Refuses, saying why: a call where the new order of evaluation could matter (arguments with side effects, or that could panic), a variadic parameter, a function used as a value, a method that would start or stop implementing an interface from outside the module.`
}

func (*MoveParameter) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":      {"type": "string",  "description": "Path of a file containing the function's declaration or a call, relative to the workspace root."},
    "line":      {"type": "integer", "description": "1-based line of that occurrence. Omit it to find name as a symbol of the package of file."},
    "name":      {"type": "string",  "description": "The function's name: with line, just the method name for a method; without line, the package-level name or Type.Method."},
    "column":    {"type": "integer", "description": "Optional 1-based column, only needed to disambiguate."},
    "parameter": {"type": "string",  "description": "Name of the parameter to move."},
    "before":    {"type": "string",  "description": "Optional: the parameter it should go just before. Default: the end (before a variadic parameter)."}
  },
  "required": ["file", "name", "parameter"],
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
	t, err := findFamily(ws, locate.IdentQuery{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name})
	if err != nil {
		return nil, err
	}
	from, err := t.named().paramIndex(a.Parameter)
	if err != nil {
		return nil, err
	}
	if from == len(t.params)-1 && t.obj.Signature().Variadic() {
		return nil, refactor.Refusef("%s is variadic and must stay last", a.Parameter)
	}
	to, err := t.named().insertIndex(a.Before)
	if err != nil {
		return nil, err
	}
	if to == from || to == from+1 {
		return nil, refactor.Refusef("%s is already there: nothing to move", a.Parameter)
	}

	var fes []edit.FileEdit
	srcs := map[string][]byte{}
	for _, m := range t.group() {
		if err := m.checkInterfaces(func(ps []string) []string {
			k := ps[from]
			ps = slices.Delete(ps, from, from+1)
			if to > from {
				return slices.Insert(ps, to-1, k)
			}
			return slices.Insert(ps, to, k)
		}); err != nil {
			return nil, err
		}
		p := m.params[from]
		fes = append(fes, m.paramCut(p).fileEdit(), edit.FileEdit{Path: m.declFile(), Edits: []edit.TextEdit{m.paramEdit(to, p.name, m.text(p.field.Type))}})
		for _, c := range m.calls {
			if err := checkNoSpread(c, m.ws.Position(c.expr.Pos())); err != nil {
				return nil, err
			}
			moved := c.expr.Args[from]
			if err := m.checkOrder(c, from, to); err != nil {
				return nil, err
			}
			if err := m.checkNoCallIn(moved); err != nil {
				return nil, err
			}
			path := m.ws.Fset.Position(moved.Pos()).Filename
			if srcs[path] == nil {
				if srcs[path], err = os.ReadFile(path); err != nil {
					return nil, err
				}
			}
			text := string(srcs[path][m.off(moved.Pos()):m.off(moved.End())])
			fes = append(fes, m.argCut(c.expr, from, false).fileEdit(), edit.FileEdit{Path: path, Edits: []edit.TextEdit{m.argEdit(c.expr, to, text)}})
		}
	}
	return t.newPlan(fmt.Sprintf("moved parameter %q of %s", a.Parameter, t.groupName()), fes), nil
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
	private := privateLocals(c)
	moved := args[from]
	for _, b := range passed {
		why := ""
		switch {
		case hasEffect(info, moved) && readsVar(info, b, private), hasEffect(info, b) && readsVar(info, moved, private):
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
	bad, _ := unsafeValue(info, e, nil)
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
// side effect could change, other than one private says no other code can
// reach. (A function literal captures variables rather than reading them.)
func readsVar(info *types.Info, e ast.Expr, private func(*types.Var) bool) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok || found {
			return false
		}
		if id, ok := n.(*ast.Ident); ok {
			v, isVar := info.Uses[id].(*types.Var)
			found = isVar && !private(v)
		}
		return !found
	})
	return found
}

// privateLocals returns a test for the variables of the function around c
// that only its own reads and writes reach (no pointer to them, no closure
// capturing them) and whose values hold no pointers: no call can change
// or observe them.
func privateLocals(c call) func(*types.Var) bool {
	for _, f := range c.pkg.Syntax {
		if f.FileStart <= c.expr.Pos() && c.expr.Pos() < f.FileEnd {
			if fn := analysis.OutermostFunc(analysis.PathTo(f, c.expr)); fn != nil {
				fa := analysis.AnalyzeFunc(c.pkg.TypesInfo, fn)
				return func(v *types.Var) bool { return fa.IsLocal(v) && !fa.Escapes(v) && plainValue(v.Type()) }
			}
		}
	}
	return func(*types.Var) bool { return false }
}

// plainValue reports whether values of t hold no pointers, so that a copy
// shares nothing with the original.
func plainValue(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return u.Kind() != types.UnsafePointer
	case *types.Array:
		return plainValue(u.Elem())
	case *types.Struct:
		for i := range u.NumFields() {
			if !plainValue(u.Field(i).Type()) {
				return false
			}
		}
		return true
	}
	return false
}
