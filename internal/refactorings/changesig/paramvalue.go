package changesig

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/types"
	"os"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// Expand makes a removal with value two steps for each function it
// names (the function, then also): the body's uses of the parameter
// become value, then, once every body has, the parameter goes from each,
// as each is shown to get from value what every call passed. A call
// among them that passes on its own parameter then passes the value.
func (r *RemoveParameter) Expand(ws *workspace.Workspace, raw json.RawMessage) ([]refactor.Step, error) {
	var a removeArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.Value == "" {
		return nil, nil
	}
	names := append([]string{a.Name}, a.Also...)
	return refactor.WriteThenRemove[*workspace.Workspace](len(names), valueForParam, removeShown, func(i int) (json.RawMessage, error) {
		b := a
		if i > 0 {
			b.Name, b.Line, b.Column = names[i], 0, 0
		}
		b.Also = append(append([]string{}, names[:i]...), names[i+1:]...)
		return json.Marshal(b)
	})
}

// valueForParam is the first step of remove_parameter with value: an edit
// that writes value for each use of the parameter in the body. The step
// after it shows that this changed nothing.
var valueForParam = refactor.InnerEdit{InnerOf: refactor.Inner{Tool: "remove_parameter", Schema: refactor.Schema[removeArgs](), Do: writeValueForParam}}

// writeValueForParam plans valueForParam.
func writeValueForParam(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a removeArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	t, err := findFamily(ws, locate.IdentQuery{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name})
	if err != nil {
		return nil, err
	}
	if t.members != nil || t.decl == nil {
		return nil, refactor.Refusef("value is not supported for an interface method and its implementations yet; remove the uses of %s in each body first", a.Parameter)
	}
	at, err := t.paramIndex(a.Parameter)
	if err != nil {
		return nil, err
	}
	p := t.params[at]
	if t.obj.Signature().Variadic() && at == len(t.params)-1 {
		return nil, refactor.Refusef("%s is variadic, so no one expression is what every call passes", p.name)
	}
	v, err := parser.ParseExpr(a.Value)
	if err != nil {
		return nil, refactor.Refusef("value %q is not a Go expression: %v", a.Value, err)
	}
	mentions := false
	var visit func(n ast.Node) bool
	visit = func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			ast.Inspect(n.X, visit) // n.Sel names a field or method, not the parameter
			return false
		case *ast.Ident:
			mentions = mentions || n.Name == p.name
		}
		return !mentions
	}
	ast.Inspect(v, visit)
	if mentions {
		return nil, refactor.Refusef("value %s uses %s itself; give the expression every call passes in terms of the receiver and the other parameters", a.Value, p.name)
	}
	text := a.Value
	if !simpleOperand(v) {
		text = "(" + text + ")"
	}
	info := t.Pkg.TypesInfo
	pv := info.Defs[p.field.Names[p.index]]
	var tes []edit.TextEdit
	var refusal error
	ast.Inspect(t.decl.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || pv == nil || info.Uses[id] != pv || refusal != nil {
			return refusal == nil
		}
		if why := analysis.UsedAsVariable(info, analysis.PathTo(t.File, id)); why != "" {
			refusal = refactor.Refusef("%s at %s %s, so no expression can stand for it", p.name, t.WS.Position(id.Pos()), why)
		}
		tes = append(tes, edit.TextEdit{Start: t.Off(id.Pos()), End: t.Off(id.End()), NewText: text})
		return true
	})
	if refusal != nil {
		return nil, refusal
	}
	path := t.WS.Fset.Position(t.decl.Pos()).Filename
	return &refactor.Plan{
		Summary: fmt.Sprintf("wrote %s for %s in %s", a.Value, p.name, t.obj.Name()),
		Edits:   []edit.FileEdit{{Path: path, Edits: tes}},
	}, nil
}

// removeShown is the second step of remove_parameter with value: the
// parameter goes, now that the body no longer uses it, once every call is
// shown to pass value and the body to get from it what the call passed.
var removeShown = refactor.Inner{Tool: "remove_parameter", Schema: refactor.Schema[removeArgs](), Do: func(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a removeArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	return planRemove(ws, a)
}}

// valueIn returns the expressions of m's body that are value, but not an
// argument that goes with the parameter (dropped, by position: one of a
// call of m or of a function losing it in the same step).
func (m *target) valueIn(value string, dropped map[string]bool) ([]ast.Expr, error) {
	want, err := locate.ExprTokens(value)
	if err != nil {
		return nil, err
	}
	var found []ast.Expr
	ast.Inspect(m.decl.Body, func(n ast.Node) bool {
		if e, ok := n.(ast.Expr); ok && locate.TokensMatch(m.TF, m.Src, e, want) {
			if !dropped[m.WS.Fset.Position(e.Pos()).String()] {
				found = append(found, e)
			}
			return false
		}
		return true
	})
	return found, nil
}

// checkShown checks that each of found, the places m's body evaluates
// value, gives what it would give at the call, as introduce_parameter
// checks an expression it moves there.
func (m *target) checkShown(value string, p param, found []ast.Expr) error {
	info := m.Pkg.TypesInfo
	ptype := info.TypeOf(p.field.Type)
	for _, e := range found {
		if t := info.TypeOf(e); t == nil || !types.Identical(t, ptype) {
			return refactor.Refusef("%s is of type %s, not %s's type %s", value, t, p.name, ptype)
		}
		if why := analysis.UsedAsVariable(info, analysis.PathTo(m.File, e)); why != "" {
			return refactor.Refusef("%s %s in the body of %s (at %s), so it does not keep the value the calls passed", value, why, m.obj.Name(), m.WS.Position(e.Pos()))
		}
		if err := m.checkIntroduced(info, m.File, e); err != nil {
			return err
		}
	}
	return nil
}

// droppedArgs returns, by position, the arguments for parameter of every
// call of the functions a removal with value names, which go with it.
func droppedArgs(ws *workspace.Workspace, a removeArgs, t *target) (map[string]bool, error) {
	dropped := map[string]bool{}
	for _, n := range append([]string{""}, a.Also...) {
		g := t
		if n != "" {
			var err error
			if g, err = findFamily(ws, locate.IdentQuery{File: a.File, Name: n}); err != nil {
				return nil, err
			}
		}
		i, err := g.named().paramIndex(a.Parameter)
		if err != nil && n != "" {
			continue // removed already, by an earlier step
		}
		if err != nil {
			return nil, err
		}
		for _, m := range g.group() {
			for _, c := range m.calls {
				if i < len(c.expr.Args) {
					dropped[ws.Fset.Position(c.expr.Args[i].Pos()).String()] = true
				}
			}
		}
	}
	return dropped, nil
}

// checkPasses checks that the call c passes, at index at, the expression
// e of m's body written in the call's own terms.
func (m *target) checkPasses(e ast.Expr, c call, at int) error {
	info := m.Pkg.TypesInfo
	csrc, err := os.ReadFile(m.WS.Fset.Position(c.expr.Pos()).Filename)
	if err != nil {
		return err
	}
	if err := m.checkCall(info, e, c, csrc); err != nil {
		return err
	}
	want, err := m.substitute(info, m.Src, e, c, csrc, map[string]*imports.Adder{})
	if err != nil {
		return err
	}
	arg := c.expr.Args[at]
	got := string(csrc[m.Off(arg.Pos()):m.Off(arg.End())])
	gt, err1 := locate.ExprTokens(got)
	wt, err2 := locate.ExprTokens(want)
	if err1 != nil || err2 != nil || !locate.EqualTokens(unparen(gt), unparen(wt)) {
		return refactor.Refusef("the call at %s passes %s, not %s (%s as the call would write it); change what it passes first, or, if its function passes on a parameter of its own, name that function in also", m.WS.Position(c.expr.Pos()), got, want, m.Text(e))
	}
	return nil
}

// unparen drops the parentheses around a whole expression's tokens.
func unparen(ts []string) []string {
	for len(ts) >= 2 && ts[0] == "(" && ts[len(ts)-1] == ")" {
		depth, whole := 0, true
		for i, t := range ts {
			switch t {
			case "(":
				depth++
			case ")":
				depth--
			}
			if depth == 0 && i < len(ts)-1 {
				whole = false
				break
			}
		}
		if !whole {
			break
		}
		ts = ts[1 : len(ts)-1]
	}
	return ts
}
