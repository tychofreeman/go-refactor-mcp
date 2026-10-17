package changesig

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// AddParameter is the "add_parameter" refactoring.
type AddParameter struct{}

// NewAddParameter returns the add_parameter refactoring.
func NewAddParameter() *AddParameter { return &AddParameter{} }

func (*AddParameter) Name() string { return "add_parameter" }

func (*AddParameter) Description() string {
	return `Add a parameter to a function or unexported method, and pass the given value for it at every call in the workspace.
Use it to prepare a feature change: add the parameter first, then change the body to use it.
Identify the function by file, line and name, at its declaration or any call. Give the new parameter's name and type, the value every call passes (written as Go source, e.g. "", nil, 0, or a name in scope at each call such as cfg.OutDir), and optionally before: the existing parameter to insert it ahead of (default: at the end, or just before a variadic parameter).
The body does not use the new parameter yet, so behavior is unchanged; the value must therefore have no side effects and must not be able to panic (no calls, pointer dereferences, indexing or non-constant division).
Refuses if the function is used other than by calling it (as a function value or method expression), if it is an exported or interface method, if the new name clashes with a parameter, result or top-level local or would hide a name the body uses, or if the value does not type-check at some call.`
}

func (*AddParameter) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":      {"type": "string",  "description": "Path of a file containing the function's declaration or a call, relative to the workspace root."},
    "line":      {"type": "integer", "description": "1-based line of that occurrence."},
    "name":      {"type": "string",  "description": "The function's name (for a method, just the method name)."},
    "column":    {"type": "integer", "description": "Optional 1-based column, only needed to disambiguate."},
    "parameter": {"type": "string",  "description": "Name of the new parameter."},
    "type":      {"type": "string",  "description": "Type of the new parameter, as Go source valid in the declaring file, e.g. string or *bytes.Buffer."},
    "value":     {"type": "string",  "description": "Go expression every call passes for the new parameter, e.g. \"\" or nil or cfg.OutDir."},
    "before":    {"type": "string",  "description": "Optional: an existing parameter; the new one goes just before it."}
  },
  "required": ["file", "line", "name", "parameter", "type", "value"],
  "additionalProperties": false
}`)
}

type addArgs struct {
	File      string  `json:"file"`
	Line      int     `json:"line"`
	Column    int     `json:"column,omitempty"`
	Name      string  `json:"name"`
	Parameter string  `json:"parameter"`
	Type      string  `json:"type"`
	Value     literal `json:"value"`
	Before    string  `json:"before,omitempty"`
}

// literal is Go source text, given as a JSON string or, for a number or
// boolean (`value=0` on the command line), as the JSON value itself.
type literal string

func (l *literal) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*l = literal(s)
		return nil
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch v.(type) {
	case float64, bool:
		*l = literal(b)
		return nil
	}
	return fmt.Errorf("value must be a string holding a Go expression, got %s", b)
}

// Plan checks preconditions and computes the edits.
func (*AddParameter) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a addArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	a.Type = strings.TrimSpace(a.Type)
	value := strings.TrimSpace(string(a.Value))
	switch {
	case !token.IsIdentifier(a.Parameter):
		return nil, refactor.Refusef("parameter %q is not a valid Go identifier", a.Parameter)
	case a.Type == "":
		return nil, refactor.Refusef("type is required")
	case value == "":
		return nil, refactor.Refusef("value is required: the expression every call passes for the new parameter")
	}
	t, err := findTarget(ws, locate.IdentQuery{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name})
	if err != nil {
		return nil, err
	}
	at, err := t.insertIndex(a.Before)
	if err != nil {
		return nil, err
	}
	if err := t.checkNewName(a.Parameter); err != nil {
		return nil, err
	}
	typ, err := t.evalType(a.Type)
	if err != nil {
		return nil, err
	}
	for _, c := range t.calls {
		if err := t.checkValue(c, value, typ); err != nil {
			return nil, err
		}
	}

	fes := []edit.FileEdit{{Path: ws.Fset.Position(t.decl.Pos()).Filename, Edits: []edit.TextEdit{t.paramEdit(at, a.Parameter, a.Type)}}}
	for _, c := range t.calls {
		path := ws.Fset.Position(c.expr.Pos()).Filename
		fes = append(fes, edit.FileEdit{Path: path, Edits: []edit.TextEdit{t.argEdit(c.expr, at, value)}})
	}
	return t.newPlan(fmt.Sprintf("added parameter %q to %s", a.Parameter+" "+a.Type, t.obj.Name()), fes), nil
}

// insertIndex returns the position in the flattened parameter list where
// the new parameter goes.
func (t *target) insertIndex(before string) (int, error) {
	if before == "" {
		if t.obj.Signature().Variadic() {
			return len(t.params) - 1, nil
		}
		return len(t.params), nil
	}
	for i, p := range t.params {
		if p.name == before {
			return i, nil
		}
	}
	var names []string
	for _, p := range t.params {
		names = append(names, p.name)
	}
	return 0, refactor.Refusef("%s has no parameter %q; its parameters are %v", t.obj.Name(), before, names)
}

// checkNewName refuses a name that is already declared in the function's
// scope (parameters, results, receiver, type parameters, and locals at the
// body's top level, which share that scope), or that the body uses for
// something declared outside the function, which the parameter would hide.
func (t *target) checkNewName(name string) error {
	info := t.pkg.TypesInfo
	if fs := info.Scopes[t.decl.Type]; fs != nil {
		if o := fs.Lookup(name); o != nil {
			if t.decl.Body.Pos() <= o.Pos() && o.Pos() < t.decl.Body.End() {
				return refactor.Refusef("the body of %s declares %q at its top level (at %s), which a parameter of that name would clash with",
					t.obj.Name(), name, t.ws.Position(o.Pos()))
			}
			return refactor.Refusef("%s already has a parameter or result named %q", t.obj.Name(), name)
		}
	}
	for id, o := range info.Uses {
		if id.Name != name || id.Pos() < t.decl.Body.Pos() || id.Pos() >= t.decl.Body.End() {
			continue
		}
		if o.Parent() == nil {
			continue // a field or method, not found through scope
		}
		if o.Pos().IsValid() && t.decl.Pos() <= o.Pos() && o.Pos() < t.decl.End() {
			continue // declared inside the function, in a nested scope
		}
		where := "the universe"
		if o.Pos().IsValid() {
			where = t.ws.Position(o.Pos())
		}
		return refactor.Refusef("the body of %s uses the %s %q declared at %s; a parameter named %q would hide it",
			t.obj.Name(), kind(o), name, where, name)
	}
	return nil
}

// evalType checks the type expression in the declaring file, where the
// parameter list is (so type parameters are visible).
func (t *target) evalType(src string) (types.Type, error) {
	tv, err := types.Eval(t.ws.Fset, t.pkg.Types, t.decl.Type.Params.Closing, src)
	if err != nil || !tv.IsType() {
		reason := "it is not a type"
		if err != nil {
			reason = err.Error()
		}
		return nil, refactor.Refusef("the type %q is not valid in %s: %s", src, t.ws.Rel(t.ws.Fset.Position(t.decl.Pos()).Filename), reason)
	}
	return tv.Type, nil
}

// checkValue checks that value type-checks at the call, is assignable to
// the new parameter's type, and can be evaluated there without side
// effects or panics: the body ignores the parameter, so evaluating it must
// change nothing.
func (t *target) checkValue(c call, value string, typ types.Type) error {
	where := t.ws.Position(c.expr.Pos())
	if err := checkNoSpread(c, where); err != nil {
		return err
	}
	e, err := parser.ParseExpr(value)
	if err != nil {
		return refactor.Refusef("the value %s is not a Go expression: %v", value, err)
	}
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	if err := types.CheckExpr(t.ws.Fset, c.pkg.Types, c.expr.Lparen, e, info); err != nil {
		msg := err.Error()
		if te, ok := err.(types.Error); ok {
			msg = te.Msg // its position is in the parsed value, not the file
		}
		return refactor.Refusef("the value %s does not work at the call at %s: %s", value, where, msg)
	}
	if bad, why := unsafeValue(info, e); bad != nil {
		return refactor.Refusef("the value %s %s (%s), and the body would not use it, so passing it would change behavior; use a value without calls, dereferences, indexing or division",
			value, why, types.ExprString(bad.(ast.Expr)))
	}
	if vt := info.Types[e].Type; !containsTypeParam(typ) && !assignable(vt, typ) {
		return refactor.Refusef("the value %s (of type %s) cannot be used as %s at the call at %s", value, vt, typ, where)
	}
	return nil
}

// assignable is types.AssignableTo, also accepting a type that matches by
// name: a call in a package's test variant sees distinct but equally named
// types. (A generic parameter type is left to the engine's type check.)
func assignable(v, p types.Type) bool {
	if types.AssignableTo(v, p) || types.TypeString(v, nil) == types.TypeString(p, nil) {
		return true
	}
	if b, ok := v.(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
		return types.AssignableTo(v, p.Underlying())
	}
	return false
}

func containsTypeParam(t types.Type) bool {
	found := false
	var walk func(types.Type)
	walk = func(t types.Type) {
		if found || t == nil {
			return
		}
		switch t := t.(type) {
		case *types.TypeParam:
			found = true
		case *types.Pointer:
			walk(t.Elem())
		case *types.Slice:
			walk(t.Elem())
		case *types.Array:
			walk(t.Elem())
		case *types.Map:
			walk(t.Key())
			walk(t.Elem())
		case *types.Chan:
			walk(t.Elem())
		case *types.Named:
			for i := 0; i < t.TypeArgs().Len(); i++ {
				walk(t.TypeArgs().At(i))
			}
		case *types.Signature:
			for i := 0; i < t.Params().Len(); i++ {
				walk(t.Params().At(i).Type())
			}
			for i := 0; i < t.Results().Len(); i++ {
				walk(t.Results().At(i).Type())
			}
		}
	}
	walk(t)
	return found
}

// unsafeValue returns the first part of e whose evaluation could have a
// side effect or panic, and what it could do.
func unsafeValue(info *types.Info, e ast.Expr) (ast.Node, string) {
	if tv := info.Types[e]; tv.Value != nil {
		return nil, "" // a constant
	}
	var bad ast.Node
	var why string
	ast.Inspect(e, func(n ast.Node) bool {
		if bad != nil {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false // defining a closure runs nothing
		}
		if ex, ok := n.(ast.Expr); ok && info.Types[ex].Value != nil {
			return false
		}
		switch n := n.(type) {
		case *ast.StarExpr:
			if !info.Types[n].IsType() {
				bad, why = n, "could panic: it dereferences a pointer"
			}
		case *ast.SelectorExpr:
			if sel := info.Selections[n]; sel != nil && sel.Kind() == types.FieldVal && sel.Indirect() {
				bad, why = n, "could panic: it reads a field through a pointer"
			}
		case *ast.IndexExpr:
			if tv := info.Types[n.X]; !tv.IsType() {
				if _, isMap := tv.Type.Underlying().(*types.Map); !isMap {
					bad, why = n, "could panic: it indexes"
				}
			}
		case *ast.SliceExpr:
			bad, why = n, "could panic: it slices"
		case *ast.TypeAssertExpr:
			bad, why = n, "could panic: it is a type assertion"
		case *ast.BinaryExpr:
			if (n.Op == token.QUO || n.Op == token.REM) && info.Types[n.Y].Value == nil {
				bad, why = n, "could panic: it divides by a value that could be zero"
			}
		default:
			if analysis.HasEffect(info, n) {
				bad, why = n, "has side effects"
			}
		}
		return bad == nil
	})
	return bad, why
}

// paramEdit inserts the parameter "name typ" into the parameter list at
// index at. Next to a group of the same type it joins the group
// (`src, dst, skip string`); inside a group of another type it splits it
// (`a int, s string, b int`).
func (t *target) paramEdit(at int, name, typ string) edit.TextEdit {
	insert := func(p token.Pos, text string) edit.TextEdit {
		o := t.off(p)
		return edit.TextEdit{Start: o, End: o, NewText: text}
	}
	if len(t.params) == 0 {
		return insert(t.decl.Type.Params.Opening+1, name+" "+typ)
	}
	if at == len(t.params) {
		last := t.params[at-1].field
		if t.text(last.Type) == typ {
			return insert(last.Names[len(last.Names)-1].End(), ", "+name)
		}
		return insert(last.End(), ", "+name+" "+typ)
	}
	p := t.params[at]
	same := t.text(p.field.Type) == typ
	switch {
	case p.index == 0 && same:
		return insert(p.field.Pos(), name+", ")
	case p.index == 0:
		return insert(p.field.Pos(), name+" "+typ+", ")
	case same:
		return insert(p.field.Names[p.index-1].End(), ", "+name)
	}
	return insert(p.field.Names[p.index-1].End(), " "+t.text(p.field.Type)+", "+name+" "+typ)
}

// argEdit inserts value into the call's arguments at index at.
func (t *target) argEdit(ce *ast.CallExpr, at int, value string) edit.TextEdit {
	if at < len(ce.Args) {
		o := t.off(ce.Args[at].Pos())
		return edit.TextEdit{Start: o, End: o, NewText: value + ", "}
	}
	if len(ce.Args) == 0 {
		o := t.off(ce.Lparen) + 1
		return edit.TextEdit{Start: o, End: o, NewText: value}
	}
	o := t.off(ce.Args[len(ce.Args)-1].End())
	return edit.TextEdit{Start: o, End: o, NewText: ", " + value}
}

func kind(o types.Object) string {
	switch o := o.(type) {
	case *types.Const:
		return "constant"
	case *types.Var:
		return "variable"
	case *types.Func:
		return "function"
	case *types.TypeName:
		return "type"
	case *types.PkgName:
		return "import"
	case *types.Builtin:
		return "builtin"
	case *types.Nil:
		return "predeclared " + o.Name()
	}
	return "name"
}
