package changesig

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// AddParameter is the "add_parameter" refactoring.
type AddParameter struct{}

// NewAddParameter returns the add_parameter refactoring.
func NewAddParameter() *AddParameter { return &AddParameter{} }

func (*AddParameter) Name() string { return "add_parameter" }

func (*AddParameter) Description() string {
	return `Add a parameter to a function or method, and pass the same value for it at every call in the workspace: with name retries, type int and value 3, func send(to string) becomes func send(to string, retries int), and send(addr) becomes send(addr, 3). The body does not use it yet, so behavior is unchanged; add it, then change the body.
Identify the function by file and name, or by line at its declaration or any call. value is Go source that every call can see (a name of the function's package is qualified at calls in other packages: Dose{} becomes garden.Dose{}); it must have no side effects and not be able to panic. before puts the parameter ahead of that one (default: last).
replace_variable turns a package variable the body reads into a parameter: with parameter rate, type float64 and value rate, func cost(n int) float64 { return float64(n) * rate } becomes func cost(rate float64, n int) float64 with the same body, and each call passes rate. A call passing another value (defaultRate()) is an edit, and the result lists it.
Refuses, saying why: a function used as a value, a method that would start or stop implementing an interface from outside the module, a name that clashes, a value that does not type-check at some call.`
}

func (*AddParameter) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":      {"type": "string",  "description": "Path of a file containing the function's declaration or a call, relative to the workspace root."},
    "line":      {"type": "integer", "description": "1-based line of that occurrence. Omit it to find name as a symbol of the package of file."},
    "name":      {"type": "string",  "description": "The function's name: with line, just the method name for a method; without line, the package-level name or Type.Method."},
    "column":    {"type": "integer", "description": "Optional 1-based column, only needed to disambiguate."},
    "parameter": {"type": "string",  "description": "Name of the new parameter."},
    "type":      {"type": "string",  "description": "Type of the new parameter, as Go source valid in the declaring file, e.g. string or *bytes.Buffer."},
    "value":     {"type": "string",  "description": "Go expression every call passes for the new parameter, e.g. \"\" or nil or cfg.OutDir."},
    "before":    {"type": "string",  "description": "Optional: an existing parameter; the new one goes just before it."},
    "replace_variable": {"type": "boolean", "description": "The parameter is named like a package variable the body reads, and takes its place: the body reads the parameter instead. Calls whose value is not that variable are edits, listed in a note."}
  },
  "required": ["file", "name", "parameter", "type", "value"],
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
	// ReplaceVariable: the parameter takes the place of the package
	// variable of the same name that the body reads.
	ReplaceVariable bool `json:"replace_variable,omitempty"`
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
	t, err := findFamily(ws, locate.IdentQuery{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name})
	if err != nil {
		return nil, err
	}
	at, err := t.named().insertIndex(a.Before)
	if err != nil {
		return nil, err
	}
	typ, err := t.evalType(a.Type)
	if err != nil {
		return nil, err
	}
	var fes []edit.FileEdit
	adders := map[string]*imports.Adder{}
	var unproven []string
	for _, m := range t.group() {
		v, err := m.hides(a.Parameter, a.ReplaceVariable)
		if err != nil {
			return nil, err
		}
		if err := m.checkReplaced(v, a.ReplaceVariable, typ); err != nil {
			return nil, err
		}
		if err := m.checkInterfaces(func(ps []string) []string { return slices.Insert(ps, at, typeKey(typ)) }); err != nil {
			return nil, err
		}
		values := make([]string, len(m.calls))
		for i, c := range m.calls {
			if values[i], err = m.valueAt(c, value, adders); err != nil {
				return nil, err
			}
			if err := m.checkValue(c, values[i], typ); err != nil {
				return nil, err
			}
			if v != nil && !passes(c, values[i], v) {
				unproven = append(unproven, fmt.Sprintf("at %s: %s", ws.Position(c.expr.Pos()), values[i]))
			}
		}
		fes = append(fes, edit.FileEdit{Path: m.declFile(), Edits: []edit.TextEdit{m.paramEdit(at, a.Parameter, a.Type)}})
		for i, c := range m.calls {
			path := ws.Fset.Position(c.expr.Pos()).Filename
			fes = append(fes, edit.FileEdit{Path: path, Edits: []edit.TextEdit{m.argEdit(c.expr, at, values[i])}})
		}
	}
	for path, ad := range adders {
		fes = append(fes, edit.FileEdit{Path: path, Edits: ad.Edits()})
	}
	plan := t.newPlan(fmt.Sprintf("added parameter %q to %s", a.Parameter+" "+a.Type, t.groupName()), fes)
	if a.ReplaceVariable {
		plan.Notes = append(plan.Notes, replacedNote(ws, t, a.Parameter, unproven)...)
	}
	return plan, nil
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
	_, err := t.hides(name, false)
	return err
}

// hides checks name as checkNewName does. With replace, the body may use
// one package variable of the function's package by that name, which the
// parameter is to take the place of; hides returns it.
func (t *target) hides(name string, replace bool) (*types.Var, error) {
	if t.decl == nil {
		return nil, nil // an interface method: a parameter's name there is only documentation
	}
	info := t.pkg.TypesInfo
	if fs := info.Scopes[t.decl.Type]; fs != nil {
		if o := fs.Lookup(name); o != nil {
			if t.decl.Body.Pos() <= o.Pos() && o.Pos() < t.decl.Body.End() {
				return nil, refactor.Refusef("the body of %s declares %q at its top level (at %s), which a parameter of that name would clash with",
					t.obj.Name(), name, t.ws.Position(o.Pos()))
			}
			return nil, refactor.Refusef("%s already has a parameter or result named %q", t.obj.Name(), name)
		}
	}
	var hidden *types.Var
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
		v, isVar := o.(*types.Var)
		pkgVar := isVar && o.Parent() == t.pkg.Types.Scope()
		if replace && pkgVar {
			hidden = v
			continue
		}
		where := "the universe"
		if o.Pos().IsValid() {
			where = t.ws.Position(o.Pos())
		}
		hint := ""
		if pkgVar {
			hint = "; with replace_variable, the parameter takes its place"
		}
		return nil, refactor.Refusef("the body of %s uses the %s %q declared at %s; a parameter named %q would hide it%s",
			t.obj.Name(), kind(o), name, where, name, hint)
	}
	return hidden, nil
}

// checkReplaced refuses replace_variable when typ is not the type of the
// package variable v it replaces, or when the body writes v, since the
// parameter would then take those writes. Without v there is nothing to
// replace, and the parameter is added as without the option.
func (t *target) checkReplaced(v *types.Var, replace bool, typ types.Type) error {
	switch {
	case !replace || v == nil:
		return nil
	case !types.Identical(v.Type(), typ):
		return refactor.Refusef("the parameter's type %s is not the type %s of the variable %s it would replace",
			types.TypeString(typ, types.RelativeTo(t.pkg.Types)), types.TypeString(v.Type(), types.RelativeTo(t.pkg.Types)), v.Name())
	}
	if pos := writeTo(t.pkg.TypesInfo, t.decl.Body, v); pos.IsValid() {
		return refactor.Refusef("the body of %s assigns %s (at %s); a parameter would take that write from the variable",
			t.obj.Name(), v.Name(), t.ws.Position(pos))
	}
	return nil
}

// passes reports whether the value a call passes is the variable v itself.
func passes(c call, value string, v *types.Var) bool {
	if value != v.Name() || c.pkg.Types == nil {
		return false
	}
	scope := c.pkg.Types.Scope().Innermost(c.expr.Lparen)
	if scope == nil {
		return false
	}
	_, o := scope.LookupParent(value, c.expr.Lparen)
	return o == v
}

// replacedNote says which calls of replace_variable are edits: those that
// pass another value than the variable, or every call when the module
// assigns the variable somewhere, since a call's value may then differ
// from what the body read.
func replacedNote(ws *workspace.Workspace, t *target, name string, unproven []string) []string {
	var v *types.Var
	for _, m := range t.group() {
		if hv, _ := m.hides(name, true); hv != nil {
			v = hv
		}
	}
	if v == nil {
		return nil
	}
	var notes []string
	for _, p := range ws.Pkgs {
		for _, f := range p.Syntax {
			if pos := writeTo(p.TypesInfo, f, v); pos.IsValid() {
				notes = append(notes, fmt.Sprintf("%s is assigned at %s, so no call is proven to pass what the body read: the step is an edit; check that each call passes the variable's value", v.Name(), ws.Position(pos)))
				return notes
			}
		}
	}
	if len(unproven) > 0 {
		notes = append(notes, fmt.Sprintf("the body now reads the parameter %s where it read the package variable: an edit at %d call%s whose value is not the variable, so not proven the same (%s); check that each passes what %s holds",
			name, len(unproven), textutil.Plural(len(unproven)), strings.Join(unproven, "; "), v.Name()))
	}
	return notes
}

// writeTo returns the position of the first place in n that assigns v or
// part of it (a field, an array element), takes its address, or calls a
// pointer method on it; or token.NoPos.
func writeTo(info *types.Info, n ast.Node, v *types.Var) token.Pos {
	is := func(e ast.Expr) bool {
		for {
			switch x := ast.Unparen(e).(type) {
			case *ast.Ident:
				return info.Uses[x] == v
			case *ast.SelectorExpr:
				if sel := info.Selections[x]; sel == nil || sel.Kind() != types.FieldVal || sel.Indirect() {
					return false
				}
				e = x.X
			case *ast.IndexExpr:
				if !analysis.IsArray(info.TypeOf(x.X)) {
					return false
				}
				e = x.X
			default:
				return false
			}
		}
	}
	pos := token.NoPos
	ast.Inspect(n, func(n ast.Node) bool {
		if pos.IsValid() {
			return false
		}
		var written []ast.Expr
		switch n := n.(type) {
		case *ast.AssignStmt:
			written = n.Lhs
		case *ast.IncDecStmt:
			written = []ast.Expr{n.X}
		case *ast.RangeStmt:
			if n.Tok == token.ASSIGN {
				written = []ast.Expr{n.Key, n.Value}
			}
		case *ast.UnaryExpr:
			if n.Op == token.AND {
				written = []ast.Expr{n.X}
			}
		case *ast.SelectorExpr:
			if sel := info.Selections[n]; sel != nil && sel.Kind() == types.MethodVal && analysis.PtrRecv(sel) && !analysis.IsPointer(info.TypeOf(n.X)) {
				written = []ast.Expr{n.X}
			}
		}
		for _, e := range written {
			if e != nil && is(e) {
				pos = e.Pos()
				break
			}
		}
		return true
	})
	return pos
}

// evalType checks the type expression in the declaring file, where the
// parameter list is (so type parameters are visible).
func (t *target) evalType(src string) (types.Type, error) {
	tv, err := types.Eval(t.ws.Fset, t.pkg.Types, t.ftype.Params.Closing, src)
	if err != nil || !tv.IsType() {
		reason := "it is not a type"
		if err != nil {
			reason = err.Error()
		}
		return nil, refactor.Refusef("the type %q is not valid in %s: %s", src, t.ws.Rel(t.declFile()), reason)
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
	if bad, why := unsafeValue(info, e, t.ws.Effects()); bad != nil {
		return refactor.Refusef("the value %s %s (%s), and the body would not use it, so passing it would change behavior; use a value without calls (other than known ones, like strings.ToUpper), dereferences, indexing or division",
			value, why, types.ExprString(bad.(ast.Expr)))
	}
	if vt := info.Types[e].Type; !containsTypeParam(typ) && !assignable(vt, typ) {
		return refactor.Refusef("the value %s (of type %s) cannot be used as %s at the call at %s", value, vt, typ, where)
	}
	return nil
}

// valueAt is value as the call c writes it. A name of the function's
// package that c's package does not see is qualified there (Batch is
// notify.Batch in another package), and in the function's package a
// qualified one is not (notify.Batch is Batch); keys of composite
// literals are left alone. adders give c's file the import it needs.
func (t *target) valueAt(c call, value string, adders map[string]*imports.Adder) (string, error) {
	home := t.obj.Pkg()
	fset := token.NewFileSet()
	e, err := parser.ParseExprFrom(fset, "", value, 0)
	if err != nil || home == nil || c.pkg.Types == nil {
		return value, nil // checkValue reports a value that does not parse
	}
	base := fset.File(e.Pos()).Base()
	scope := c.pkg.Types.Scope().Innermost(c.expr.Lparen)
	visible := func(name string) bool {
		if scope == nil {
			return false
		}
		_, o := scope.LookupParent(name, c.expr.Lparen)
		return o != nil
	}
	atHome := c.pkg.Types.Path() == home.Path()
	type repl struct {
		start, end int
		text       string
	}
	var rs []repl
	skip := map[*ast.Ident]bool{}
	ast.Inspect(e, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.KeyValueExpr:
			if id, ok := n.Key.(*ast.Ident); ok {
				skip[id] = true
			}
		case *ast.SelectorExpr:
			skip[n.Sel] = true
			if x, ok := n.X.(*ast.Ident); ok && atHome && x.Name == home.Name() && !visible(x.Name) && home.Scope().Lookup(n.Sel.Name) != nil {
				skip[x] = true
				rs = append(rs, repl{int(x.Pos()) - base, int(n.Sel.Pos()) - base, ""})
			}
		case *ast.Ident:
			if skip[n] || atHome || visible(n.Name) || !n.IsExported() || home.Scope().Lookup(n.Name) == nil {
				return true
			}
			q, err := t.importName(c, home, adders)
			if err != nil {
				return false
			}
			rs = append(rs, repl{int(n.Pos()) - base, int(n.Pos()) - base, q + "."})
		}
		return true
	})
	sort.Slice(rs, func(i, j int) bool { return rs[i].start > rs[j].start })
	for _, r := range rs {
		value = value[:r.start] + r.text + value[r.end:]
	}
	return value, nil
}

// importName is the name c's file imports pkg as, importing it if it
// does not.
func (t *target) importName(c call, pkg *types.Package, adders map[string]*imports.Adder) (string, error) {
	path := t.ws.Fset.Position(c.expr.Pos()).Filename
	a := adders[path]
	if a == nil {
		src, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		var file *ast.File
		for _, f := range c.pkg.Syntax {
			if f.Pos() <= c.expr.Pos() && c.expr.Pos() < f.End() {
				file = f
			}
		}
		a = imports.New(t.ws, c.pkg, file, src)
		adders[path] = a
	}
	return a.Name(pkg)
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
		return insert(t.ftype.Params.Opening+1, name+" "+typ)
	}
	if t.params[0].name == "" {
		// An interface method's parameters without names: the type alone.
		if at == len(t.params) {
			return insert(t.params[at-1].field.End(), ", "+typ)
		}
		return insert(t.params[at].field.Pos(), typ+", ")
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
