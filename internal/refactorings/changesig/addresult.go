package changesig

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/ast/astutil"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// AddResult is the "add_result" refactoring.
type AddResult struct{}

// NewAddResult returns the add_result refactoring.
func NewAddResult() *AddResult { return &AddResult{} }

func (*AddResult) Name() string { return "add_result" }

func (*AddResult) Description() string {
	return `Add a result to a function or method, last, given the same value at every return of its body, and update every call: v := f() becomes v, _ := f(); a call as a statement, or in go or defer, stays as it is.
Use it to give a function an error result before making it fail, or to return something more, then change the body.
Identify the function by file, line and name, at its declaration or any call, or by file and name without line (Name or Type.Method). type is the new result's type; value is the expression every return gives it, e.g. nil for an error. Callers ignore the new result, so behavior is unchanged; the value must therefore have no side effects and must not be able to panic. A function that had no results gets a return at its end.
Refuses named results, a call whose result is used inside an expression (assign it to a variable first) or is returned by the caller, a var declaration with a type, a return of another call's results, a function used other than by calling it, and a method that would stop or start implementing an interface declared outside the module.
An interface method of the module changes with every method of the module's types that implements it, and such a method with the interface: name any of them.`
}

func (*AddResult) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":   {"type": "string",  "description": "Path of a file containing the function's declaration or a call, relative to the workspace root."},
    "line":   {"type": "integer", "description": "1-based line of that occurrence. Omit it to find name as a symbol of the package of file."},
    "name":   {"type": "string",  "description": "The function's name: with line, just the method name for a method; without line, the package-level name or Type.Method."},
    "column": {"type": "integer", "description": "Optional 1-based column, only needed to disambiguate."},
    "type":   {"type": "string",  "description": "Type of the new result, as Go source valid in the declaring file, e.g. error."},
    "value":  {"type": "string",  "description": "Go expression every return gives the new result, e.g. nil."}
  },
  "required": ["file", "name", "type", "value"],
  "additionalProperties": false
}`)
}

type resultArgs struct {
	File   string  `json:"file"`
	Line   int     `json:"line"`
	Column int     `json:"column,omitempty"`
	Name   string  `json:"name"`
	Type   string  `json:"type"`
	Value  literal `json:"value"`
}

// Plan checks preconditions and computes the edits.
func (*AddResult) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a resultArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	a.Type = strings.TrimSpace(a.Type)
	value := strings.TrimSpace(string(a.Value))
	switch {
	case a.Type == "":
		return nil, refactor.Refusef("type is required")
	case value == "":
		return nil, refactor.Refusef("value is required: the expression every return gives the new result")
	}
	t, err := findFamily(ws, locate.IdentQuery{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name})
	if err != nil {
		return nil, err
	}
	typ, err := t.evalType(a.Type)
	if err != nil {
		return nil, err
	}
	var fes []edit.FileEdit
	for _, m := range t.group() {
		if r := m.ftype.Results; r != nil && len(r.List) > 0 && len(r.List[0].Names) > 0 {
			return nil, refactor.Refusef("%s has named results; adding one to them is not supported yet", m.obj.Name())
		}
		if err := m.checkSignatures(func(sig *types.Signature) string {
			var results []string
			for i := range sig.Results().Len() {
				results = append(results, typeKey(sig.Results().At(i).Type()))
			}
			results = append(results, typeKey(typ))
			return "(" + strings.Join(paramKeys(sig), ", ") + ") (" + strings.Join(results, ", ") + ")"
		}); err != nil {
			return nil, err
		}
		fes = append(fes, edit.FileEdit{Path: m.declFile(), Edits: m.resultEdits(a.Type)})
		if m.decl != nil {
			if err := m.checkResultValue(value, typ); err != nil {
				return nil, err
			}
			es, err := m.returnEdits(value)
			if err != nil {
				return nil, err
			}
			fes = append(fes, edit.FileEdit{Path: m.declFile(), Edits: es})
		}
		for _, c := range m.calls {
			e, err := m.callResultEdit(c)
			if err != nil {
				return nil, err
			}
			if e != nil {
				fes = append(fes, edit.FileEdit{Path: ws.Fset.Position(c.expr.Pos()).Filename, Edits: []edit.TextEdit{*e}})
			}
		}
	}
	return t.newPlan(fmt.Sprintf("added result %s to %s, %s at every return", a.Type, t.groupName(), value), fes), nil
}

// resultEdits adds typ to t's results, last.
func (t *target) resultEdits(typ string) []edit.TextEdit {
	insert := func(p token.Pos, text string) edit.TextEdit {
		return edit.TextEdit{Start: t.off(p), End: t.off(p), NewText: text}
	}
	switch r := t.ftype.Results; {
	case r == nil || len(r.List) == 0:
		return []edit.TextEdit{insert(t.ftype.Params.Closing+1, " "+typ)}
	case r.Opening.IsValid():
		return []edit.TextEdit{insert(r.Closing, ", "+typ)}
	default:
		return []edit.TextEdit{insert(r.Pos(), "("), insert(r.End(), ", "+typ+")")}
	}
}

// checkResultValue checks that value type-checks at the top of t's body,
// is assignable to typ, and has no side effects and cannot panic: callers
// ignore it, so evaluating it must change nothing.
func (t *target) checkResultValue(value string, typ types.Type) error {
	e, err := parser.ParseExpr(value)
	if err != nil {
		return refactor.Refusef("the value %s is not a Go expression: %v", value, err)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
	if err := types.CheckExpr(t.ws.Fset, t.pkg.Types, t.decl.Body.Lbrace+1, e, info); err != nil {
		msg := err.Error()
		if te, ok := err.(types.Error); ok {
			msg = te.Msg
		}
		return refactor.Refusef("the value %s does not work in %s: %s", value, t.obj.Name(), msg)
	}
	if bad, why := unsafeValue(info, e, t.effects()); bad != nil {
		return refactor.Refusef("the value %s %s (%s), and callers would not use it, so returning it would change behavior; use a value without calls, dereferences, indexing or division", value, why, types.ExprString(bad.(ast.Expr)))
	}
	if vt := info.Types[e].Type; !assignable(vt, typ) {
		return refactor.Refusef("the value %s (of type %s) cannot be returned as %s", value, vt, typ)
	}
	return nil
}

// returnEdits gives value as the new result at every return of t's body
// (not of function literals in it), and, if t had no results, returns it
// at the end of the body.
func (t *target) returnEdits(value string) ([]edit.TextEdit, error) {
	before := t.obj.Signature().Results().Len()
	var es []edit.TextEdit
	var err error
	ast.Inspect(t.decl.Body, func(n ast.Node) bool {
		if err != nil {
			return false
		}
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			switch {
			case len(n.Results) == 0:
				at := t.off(n.Return) + len("return")
				es = append(es, edit.TextEdit{Start: at, End: at, NewText: " " + value})
			case len(n.Results) < before:
				err = refactor.Refusef("the return at %s gives the results of another call; assign them to variables and return those first", t.ws.Position(n.Pos()))
			default:
				at := t.off(n.Results[len(n.Results)-1].End())
				es = append(es, edit.TextEdit{Start: at, End: at, NewText: ", " + value})
			}
		}
		return true
	})
	if err != nil || before > 0 {
		return es, err
	}
	body := t.decl.Body
	if n := len(body.List); n > 0 {
		if _, ok := body.List[n-1].(*ast.ReturnStmt); ok {
			return es, nil
		}
		last := body.List[n-1]
		indent := strings.Repeat("\t", t.ws.Fset.Position(last.Pos()).Column-1)
		at := t.off(last.End())
		return append(es, edit.TextEdit{Start: at, End: at, NewText: "\n" + indent + "return " + value}), nil
	}
	at := t.off(body.Lbrace) + 1
	return append(es, edit.TextEdit{Start: at, End: at, NewText: " return " + value + " "}), nil
}

// callResultEdit makes call c take the new result too: v, _ := f() for
// v := f(); nothing for a call as a statement. It refuses a call whose
// results the caller uses other than by assigning them.
func (t *target) callResultEdit(c call) (*edit.TextEdit, error) {
	var file *ast.File
	for _, f := range c.pkg.Syntax {
		if f.FileStart <= c.expr.Pos() && c.expr.Pos() < f.FileEnd {
			file = f
		}
	}
	path, _ := astutil.PathEnclosingInterval(file, c.expr.Pos(), c.expr.End())
	i := 1
	for i < len(path) {
		if _, ok := path[i].(*ast.ParenExpr); !ok {
			break
		}
		i++
	}
	where := t.ws.Position(c.expr.Pos())
	blank := func(after ast.Node) (*edit.TextEdit, error) {
		at := t.off(after.End())
		return &edit.TextEdit{Start: at, End: at, NewText: ", _"}, nil
	}
	switch p := path[i].(type) {
	case *ast.ExprStmt, *ast.GoStmt, *ast.DeferStmt:
		return nil, nil
	case *ast.AssignStmt:
		if len(p.Rhs) == 1 && (p.Tok == token.DEFINE || p.Tok == token.ASSIGN) {
			return blank(p.Lhs[len(p.Lhs)-1])
		}
	case *ast.ValueSpec:
		if len(p.Values) == 1 && p.Type == nil {
			return blank(p.Names[len(p.Names)-1])
		}
		if p.Type != nil {
			return nil, refactor.Refusef("the declaration at %s gives its type, which the new result does not have; declare the variables without it first", where)
		}
	case *ast.ReturnStmt:
		return nil, refactor.Refusef("the call at %s returns its results as the caller's; assign them to variables and return those first", where)
	}
	return nil, refactor.Refusef("the call at %s uses its result inside an expression; assign it to a variable first", where)
}
