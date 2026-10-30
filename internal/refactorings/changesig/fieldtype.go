package changesig

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/site"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// ChangeFieldType is the "change_field_type" refactoring.
type ChangeFieldType struct{ refactor.Named[fieldArgs] }

// NewChangeFieldType returns the change_field_type refactoring.
func NewChangeFieldType() *ChangeFieldType {
	return &ChangeFieldType{Named: refactor.Called[fieldArgs]("change_field_type")}
}

func (*ChangeFieldType) Description() string {
	return `Change a struct field's type so that every value keeps its meaning, converting where the field is written and, where its type matters, where it is read: ID string to ID PlantID turns Plant{ID: s} into Plant{ID: PlantID(s)} and strings.ToUpper(p.ID) into strings.ToUpper(string(p.ID)). The new type must hold every old value: the same underlying type, a number type that holds every old value, or an interface the old type implements. Widened to an interface (Species string to any), a switch on it, == with it and passing it as an interface stay; an untyped constant compared with or written to it becomes the old type where that is not its default (species("cactus")). Narrowed to an interface (Species any to Species), every value written to it must already be one, and a switch on it loses the cases no such value matches (case "fern" once every fern is a Fern{}); with values {"fern": "Fern{}"}, Plant{Species: "fern"} becomes Plant{Species: Fern{}} and p.Species == "fern" p.Species == (Fern{}) as it narrows.
Identify the field by file and Type.Field.
A use that cannot follow (its address taken, ++ on an interface) is listed in the refusal, and nothing changes; an embedded field and a field of a generic type are refused.`
}

type fieldArgs struct {
	File   string            `json:"file" desc:"Path of a file containing the field's declaration or a use, relative to the workspace root."`
	Line   int               `json:"line,omitempty" desc:"1-based line of that occurrence. Omit it to find name as Type.Field in the package of file."`
	Name   string            `json:"name" desc:"Type.Field without line; with line, the field's name."`
	Column int               `json:"column,omitempty" desc:"Optional 1-based column, only needed to disambiguate."`
	Type   string            `json:"type" desc:"The new type, as Go source valid in the declaring file, e.g. UserID or int64."`
	Values map[string]string `json:"values,omitempty" desc:"Only narrowing to an interface: for each old constant (\"fern\" or fern), the value of the new type, as the declaring file writes it, put wherever the field is given or compared with that constant, e.g. {\"fern\": \"Fern{}\"}."`
}

// fieldChange is a field whose type changes, and what its uses need.
type fieldChange struct {
	ws       *workspace.Workspace
	field    *types.Var
	key      string
	old, new types.Type
	mode     fieldMode // how its uses follow the new type
	name     string    // new, as the declaring file writes it
	dropped  []string  // the case values a switch on the field lost, "file:line:col value"
	files    *fileSet
	cannot   []string     // uses that cannot follow, "file:line:col: why"
	values   []fieldValue // narrowing: the values given for old constants
	mapped   int          // the uses given one of them
}

// fieldValue is a value of the new type given for an old constant.
type fieldValue struct {
	old  constant.Value
	text string   // as the declaring package writes it
	expr ast.Expr // text, parsed
}

// narrowHint says what comes before narrowing a field to iface: a type
// for each kind, written where a use has the kind's old value.
func narrowHint(ws *workspace.Workspace, iface types.Type, name string) string {
	it := iface.Underlying().(*types.Interface)
	var impl []string
	for _, p := range ws.Pkgs {
		if p.Types == nil {
			continue
		}
		for _, n := range p.Types.Scope().Names() {
			tn, ok := p.Types.Scope().Lookup(n).(*types.TypeName)
			if !ok || tn.IsAlias() || types.IsInterface(tn.Type()) {
				continue
			}
			if types.Implements(tn.Type(), it) || types.Implements(types.NewPointer(tn.Type()), it) {
				impl = append(impl, n)
			}
		}
	}
	if len(impl) == 0 {
		return fmt.Sprintf("No type implements %s yet: make a type for each kind first, its cases becoming the type's methods, then write the types where these uses have the old values, and narrow last.", name)
	}
	slices.Sort(impl)
	impl = slices.Compact(impl)
	return fmt.Sprintf("Write each kind's type (%s) where these uses have its old value, then narrow; or narrow with values for the constants listed (values {\"<constant>\": \"%s{}\"}, one for each), which writes them there.", strings.Join(impl, ", "), impl[0])
}

// fieldMode is how the uses of a field follow its new type.
type fieldMode interface {
	// write handles v, a value written to the field.
	write(c *fieldChange, p *packages.Package, v ast.Expr) error
	// read handles e, a read of the field at path[0], whose position is where.
	read(c *fieldChange, p *packages.Package, path []ast.Node, e ast.Expr, where string) error
}

// converting is a new type that holds every old value: uses are converted.
type converting struct{}

// widening is a new type that is an interface the old one implements:
// nothing is converted.
type widening struct{}

// narrowing is a new type that is an interface implementing the old one:
// every value written must already be one.
type narrowing struct{}

// Plan checks preconditions and computes the edits.
func (*ChangeFieldType) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a fieldArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	pkg, id, err := locate.Ident(ws, locate.IdentQuery{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name})
	if err != nil {
		return nil, err
	}
	field, ok := pkg.TypesInfo.ObjectOf(id).(*types.Var)
	if !ok || !field.IsField() {
		return nil, refactor.Refusef("%q at %s is not a struct field", id.Name, ws.Position(id.Pos()))
	}
	field = field.Origin()
	if field.Embedded() {
		return nil, refactor.Refusef("%s is an embedded field; changing its type is not supported", field.Name())
	}
	if !ws.Contains(ws.Fset.Position(field.Pos()).Filename) {
		return nil, refactor.Refusef("%s is declared outside the workspace", field.Name())
	}
	declPkg, decl, err := fieldDecl(ws, field)
	if err != nil {
		return nil, err
	}
	t := &target{Site: site.Site{WS: ws, Pkg: declPkg}, obj: types.NewFunc(field.Pos(), field.Pkg(), field.Name(), nil)}
	t.ftype = &ast.FuncType{Params: &ast.FieldList{Closing: decl.End()}}
	newT, err := t.parseType(a.Type)
	if err != nil {
		return nil, err
	}
	if containsTypeParam(field.Type()) || containsTypeParam(newT) {
		return nil, refactor.Refusef("changing a field to or from a type with type parameters is not supported")
	}
	if types.Identical(field.Type(), newT) {
		return nil, refactor.Refusef("%s is already of type %s", field.Name(), t.typeName(newT))
	}
	widen, err := t.checkConversion(field.Type(), newT)
	narrow := err != nil && types.IsInterface(field.Type()) && types.IsInterface(newT) && types.AssignableTo(newT, field.Type())
	if err != nil && !narrow {
		return nil, refactor.Refusef("%s", strings.Replace(err.Error(), "change_parameter_type", "change_field_type", 1))
	}
	var mode fieldMode = converting{}
	switch {
	case narrow:
		mode = narrowing{}
	case widen:
		mode = widening{}
	}
	c := &fieldChange{ws: ws, field: field, key: keyOf(ws.Fset, field), old: field.Type(), new: newT, mode: mode, name: t.typeName(newT), files: newFileEdits(ws)}
	if len(a.Values) > 0 {
		if !narrow {
			return nil, refactor.Refusef("values is for narrowing a field to an interface; %s to %s is not", t.typeName(field.Type()), c.name)
		}
		if c.values, err = parseValues(ws, declPkg, decl.Pos(), a.Values, newT, c.name); err != nil {
			return nil, err
		}
	}
	if err := c.declEdit(declPkg, decl); err != nil {
		return nil, err
	}
	uses, err := c.uses()
	if err != nil {
		return nil, err
	}
	if len(c.cannot) > 0 {
		var hint string
		if narrow {
			hint = "\n" + narrowHint(ws, newT, t.typeName(newT))
		}
		return nil, refactor.Refusef("these uses of %s cannot follow its new type %s, so nothing changed:\n  %s%s", field.Name(), t.typeName(newT), strings.Join(c.cannot, "\n  "), hint)
	}
	plan := &refactor.Plan{
		Summary: fmt.Sprintf("changed field %s from %s to %s (%d use%s)", field.Name(), t.typeName(field.Type()), t.typeName(newT), uses, map[bool]string{true: "", false: "s"}[uses == 1]),
		Edits:   edit.Merge(c.files.all()),
	}
	if c.mapped > 0 {
		plan.Notes = append(plan.Notes, fmt.Sprintf("wrote the values given for the field's old constants in %d places: an edit, which keeps behavior if each value's methods do what the cases of its constant did", c.mapped))
	}
	if len(c.dropped) > 0 {
		plan.Notes = append(plan.Notes, fmt.Sprintf("removed the cases no %s matches: %s", c.name, strings.Join(c.dropped, ", ")))
	}
	if analysis.PublicAPI(field, ws.Pkgs) {
		plan.Notes = append(plan.Notes, refactor.APINote(field.Name(), "changing its type"))
	}
	return plan, nil
}

// fieldDecl finds the field list entry that declares field.
func fieldDecl(ws *workspace.Workspace, field *types.Var) (*packages.Package, *ast.Field, error) {
	key := keyOf(ws.Fset, field)
	for _, p := range ws.Pkgs {
		for _, f := range p.Syntax {
			var found *ast.Field
			ast.Inspect(f, func(n ast.Node) bool {
				st, ok := n.(*ast.StructType)
				if !ok || found != nil {
					return found == nil
				}
				for _, fl := range st.Fields.List {
					for _, nm := range fl.Names {
						if keyOf(ws.Fset, p.TypesInfo.Defs[nm]) == key {
							found = fl
						}
					}
				}
				return true
			})
			if found != nil {
				return p, found, nil
			}
		}
	}
	return nil, nil, refactor.Refusef("cannot find the declaration of %s", field.Name())
}

// declEdit gives the field its new type, splitting its group if it
// shares one (n, max int32 to n int64 and max int32, on lines of their
// own).
func (c *fieldChange) declEdit(pkg *packages.Package, f *ast.Field) error {
	fe, err := c.files.of(pkg, f.Pos())
	if err != nil {
		return err
	}
	typ := fe.write(c.new)
	off := func(p token.Pos) int { return c.ws.Fset.Position(p).Offset }
	if len(f.Names) == 1 {
		fe.edits = append(fe.edits, edit.TextEdit{Start: off(f.Type.Pos()), End: off(f.Type.End()), NewText: typ})
		return nil
	}
	old := types.ExprString(f.Type)
	var parts []string
	var rest []string
	for _, n := range f.Names {
		if n.Name == c.field.Name() {
			if len(rest) > 0 {
				parts = append(parts, strings.Join(rest, ", ")+" "+old)
				rest = nil
			}
			parts = append(parts, n.Name+" "+typ)
			continue
		}
		rest = append(rest, n.Name)
	}
	if len(rest) > 0 {
		parts = append(parts, strings.Join(rest, ", ")+" "+old)
	}
	fe.edits = append(fe.edits, edit.TextEdit{Start: off(f.Names[0].Pos()), End: off(f.Type.End()), NewText: strings.Join(parts, "\n")})
	return nil
}

// uses adjusts every use of the field, noting those that cannot follow,
// and returns how many there are.
func (c *fieldChange) uses() (int, error) {
	seen := map[token.Position]bool{}
	n := 0
	for _, p := range c.ws.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, f := range p.Syntax {
			var err error
			ast.Inspect(f, func(node ast.Node) bool {
				if err != nil {
					return false
				}
				switch node := node.(type) {
				case *ast.Ident:
					if keyOf(c.ws.Fset, p.TypesInfo.Uses[node]) != c.key || seen[c.ws.Fset.Position(node.Pos())] {
						return true
					}
					seen[c.ws.Fset.Position(node.Pos())] = true
					n++
					err = c.use(p, f, node)
				case *ast.CompositeLit:
					if len(node.Elts) == 0 || seen[c.ws.Fset.Position(node.Pos())] {
						return true
					}
					if _, keyed := node.Elts[0].(*ast.KeyValueExpr); keyed {
						return true
					}
					st, ok := p.TypesInfo.TypeOf(node).Underlying().(*types.Struct)
					if !ok {
						return true
					}
					for i := range st.NumFields() {
						if keyOf(c.ws.Fset, st.Field(i)) == c.key && i < len(node.Elts) {
							seen[c.ws.Fset.Position(node.Pos())] = true
							n++
							err = c.write(p, node.Elts[i])
						}
					}
				}
				return true
			})
			if err != nil {
				return 0, err
			}
		}
	}
	return n, nil
}

// use adjusts the use id of the field: a key in a literal, or x.F.
func (c *fieldChange) use(p *packages.Package, f *ast.File, id *ast.Ident) error {
	path, _ := astutil.PathEnclosingInterval(f, id.Pos(), id.End())
	if kv, ok := path[1].(*ast.KeyValueExpr); ok && kv.Key == id {
		return c.write(p, kv.Value)
	}
	sel, ok := path[1].(*ast.SelectorExpr)
	if !ok || sel.Sel != id {
		return nil
	}
	path = path[1:]
	var e ast.Expr = sel
	for len(path) > 1 {
		if _, paren := path[1].(*ast.ParenExpr); !paren {
			break
		}
		e, path = path[1].(ast.Expr), path[1:]
	}
	where := c.ws.Position(sel.Sel.Pos())
	switch u := path[1].(type) {
	case *ast.AssignStmt:
		for i, l := range u.Lhs {
			if l != e {
				continue
			}
			switch {
			case len(u.Lhs) != len(u.Rhs):
				c.cannot = append(c.cannot, where+": is assigned another call's result")
			case u.Tok == token.ASSIGN:
				return c.write(p, u.Rhs[i])
			case types.IsInterface(c.new):
				c.cannot = append(c.cannot, where+": "+u.Tok.String()+" on an interface")
			default:
				return c.write(p, u.Rhs[i])
			}
			return nil
		}
	case *ast.IncDecStmt:
		if types.IsInterface(c.new) {
			c.cannot = append(c.cannot, where+": "+u.Tok.String()+" on an interface")
		}
		return nil
	case *ast.UnaryExpr:
		if u.Op == token.AND {
			c.cannot = append(c.cannot, where+": takes its address")
			return nil
		}
	}
	// A read.
	return c.mode.read(c, p, path, e, where)
}

// read keeps a use that means the same once the field is an interface
// (a switch, ==, passing it as one, a method of the interface), with the
// constants compared with it written as write would, and notes the rest.
func (m widening) read(c *fieldChange, p *packages.Package, path []ast.Node, e ast.Expr, where string) error {
	if ok, constants := c.keeps(p.TypesInfo, path); ok {
		for _, k := range constants {
			if err := c.write(p, k); err != nil {
				return err
			}
		}
		return nil
	}
	if callsMethodOf(p.TypesInfo, path, c.new) {
		return nil
	}
	c.cannot = append(c.cannot, where+": uses its value as "+types.TypeString(c.old, nil))
	return nil
}

// read converts e back to the old type where its type matters.
func (m converting) read(c *fieldChange, p *packages.Package, path []ast.Node, e ast.Expr, where string) error {
	if typeIrrelevant(p.TypesInfo, path, types.Identical(c.old.Underlying(), c.new.Underlying())) {
		return nil
	}
	fe, err := c.files.of(p, e.Pos())
	if err != nil {
		return err
	}
	fe.edits = append(fe.edits, (&target{Site: site.Site{WS: c.ws}}).conversion(e, fe.write(c.old))...)
	return nil
}

// write handles v, a value written to the field, as its mode does.
func (c *fieldChange) write(p *packages.Package, v ast.Expr) error {
	return c.mode.write(c, p, v)
}

// write notes v unless it is already of the new type, or nil.
func (m narrowing) write(c *fieldChange, p *packages.Package, v ast.Expr) error {
	if fv := c.valueFor(p, v); fv != nil {
		return c.replaceValue(p, v, fv, false)
	}
	c.hold(p, v)
	return nil
}

// write leaves v as it is, unless it is an untyped constant: written to
// an interface it takes its default type, so one whose default is not the
// old type is converted to the old type.
func (m widening) write(c *fieldChange, p *packages.Package, v ast.Expr) error {
	if untypedConstant(p.TypesInfo, v) {
		if dt := defaultType(p.TypesInfo, v); dt == nil || !types.Identical(dt, c.old) {
			return c.convert(p, v, c.old)
		}
	}
	return nil
}

// write converts v to the new type, unless it is an untyped constant.
func (m converting) write(c *fieldChange, p *packages.Package, v ast.Expr) error {
	if untypedConstant(p.TypesInfo, v) {
		return nil
	}
	return c.convert(p, v, c.new)
}

// convert writes v converted to t.
func (c *fieldChange) convert(p *packages.Package, v ast.Expr, t types.Type) error {
	fe, err := c.files.of(p, v.Pos())
	if err != nil {
		return err
	}
	fe.edits = append(fe.edits, (&target{Site: site.Site{WS: c.ws}}).conversion(v, fe.write(t))...)
	return nil
}

// hold notes v, a value written to the narrowed field, unless it is
// already of the new type, or nil.
func (c *fieldChange) hold(p *packages.Package, v ast.Expr) {
	t := p.TypesInfo.TypeOf(v)
	switch {
	case types.AssignableTo(t, c.newIn(p)):
	case types.IsInterface(t):
		c.cannot = append(c.cannot, fmt.Sprintf("%s: writes %s, of type %s, which need not be a %s", c.ws.Position(v.Pos()), types.ExprString(v), types.TypeString(t, types.RelativeTo(c.field.Pkg())), c.name))
	default:
		c.cannot = append(c.cannot, fmt.Sprintf("%s: writes %s, which is not a %s", c.ws.Position(v.Pos()), types.ExprString(v), c.name))
	}
}

// parseValues checks the values given for old constants: each a value of
// newT, in the scope of pkg at pos, that calls nothing.
func parseValues(ws *workspace.Workspace, pkg *packages.Package, pos token.Pos, given map[string]string, newT types.Type, name string) ([]fieldValue, error) {
	keys := slices.Sorted(maps.Keys(given))
	var out []fieldValue
	for _, k := range keys {
		old := oldConstant(k)
		text := strings.TrimSpace(given[k])
		e, err := parser.ParseExpr(text)
		if err != nil {
			return nil, refactor.Refusef("the value for %s, %q, is not a Go expression: %v", old, text, err)
		}
		calls := false
		ast.Inspect(e, func(n ast.Node) bool {
			_, ok := n.(*ast.CallExpr)
			calls = calls || ok
			return !calls
		})
		if calls {
			return nil, refactor.Refusef("the value for %s, %s, calls a function, which each use would then do; give a literal, a constant or a variable", old, text)
		}
		tv, err := types.Eval(ws.Fset, pkg.Types, pos, text)
		if err != nil {
			return nil, refactor.Refusef("the value for %s, %s: %v", old, text, err)
		}
		if !types.AssignableTo(tv.Type, newT) {
			why := ""
			if it, ok := newT.Underlying().(*types.Interface); ok {
				why = notImplements(tv.Type, it, name, pkg.Types)
			}
			return nil, refactor.Refusef("the value for %s, %s, is not a %s%s", old, text, name, why)
		}
		out = append(out, fieldValue{old: old, text: text, expr: e})
	}
	return out, nil
}

// notImplements says why a value of type v is not one of iface, named
// name: ": " and the method v lacks or has with another signature, or "".
func notImplements(v types.Type, iface *types.Interface, name string, pkg *types.Package) string {
	m, wrong := types.MissingMethod(v, iface, true)
	if m == nil {
		return ""
	}
	q := types.RelativeTo(pkg)
	vn := types.TypeString(v, q)
	if !wrong {
		return fmt.Sprintf(": %s has no method %s; move each kind's cases into its type's methods first, and narrow last", vn, m.Name())
	}
	have, _, _ := types.LookupFieldOrMethod(v, true, m.Pkg(), m.Name())
	f, ok := have.(*types.Func)
	switch {
	case !ok:
		return fmt.Sprintf(": %s has %s, which is not a method", vn, m.Name())
	case types.Identical(f.Type(), m.Type()):
		return fmt.Sprintf(": its method %s has a pointer receiver, so only *%s is a %s", m.Name(), vn, name)
	}
	return fmt.Sprintf(": its method %s is %s, where %s has %s", m.Name(), types.TypeString(f.Type(), q), name, types.TypeString(m.Type(), q))
}

// oldConstant is the constant k names: a literal ("fern", 3), or else the
// string k itself (fern).
func oldConstant(k string) constant.Value {
	if bl, ok := mustParse(k).(*ast.BasicLit); ok {
		if v := constant.MakeFromLiteral(bl.Value, bl.Kind, 0); v.Kind() != constant.Unknown {
			return v
		}
	}
	return constant.MakeString(k)
}

// mustParse parses s as an expression, or returns nil.
func mustParse(s string) ast.Expr {
	e, _ := parser.ParseExpr(s)
	return e
}

// valueFor returns the value given for v, a constant of package p, or nil.
func (c *fieldChange) valueFor(p *packages.Package, v ast.Expr) *fieldValue {
	cv := p.TypesInfo.Types[v].Value
	if cv == nil {
		return nil
	}
	for i, fv := range c.values {
		if fv.old.Kind() == cv.Kind() && constant.Compare(fv.old, token.EQL, cv) {
			return &c.values[i]
		}
	}
	return nil
}

// replaceValue writes fv in place of v, a use of its old constant in
// package p, compared with the field when compared (a composite literal
// then in parentheses, which an if's condition needs).
func (c *fieldChange) replaceValue(p *packages.Package, v ast.Expr, fv *fieldValue, compared bool) error {
	fe, err := c.files.of(p, v.Pos())
	if err != nil {
		return err
	}
	text, err := fe.writeValue(fv, c.field.Pkg(), p.Types)
	if err != nil {
		return err
	}
	if _, lit := ast.Unparen(fv.expr).(*ast.CompositeLit); lit && compared {
		text = "(" + text + ")"
	}
	fe.edits = append(fe.edits, edit.TextEdit{Start: c.ws.Fset.Position(v.Pos()).Offset, End: c.ws.Fset.Position(v.End()).Offset, NewText: text})
	c.mapped++
	return nil
}

// writeValue writes fv, of package home, for this file of package p:
// in another package, home's names in it qualified.
func (fe *fileEdits) writeValue(fv *fieldValue, home, p *types.Package) (string, error) {
	if p.Path() == home.Path() {
		return fv.text, nil
	}
	var at []int // offsets in fv.text of names to qualify
	skip := map[*ast.Ident]bool{}
	ast.Inspect(fv.expr, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			skip[n.Sel] = true
		case *ast.KeyValueExpr:
			if id, ok := n.Key.(*ast.Ident); ok {
				skip[id] = true
			}
		case *ast.Ident:
			if !skip[n] && home.Scope().Lookup(n.Name) != nil {
				at = append(at, int(n.Pos())-1)
			}
		}
		return true
	})
	if len(at) == 0 {
		return fv.text, nil
	}
	q, err := fe.adder.Name(home)
	if err != nil {
		return "", err
	}
	s := fv.text
	for i := len(at) - 1; i >= 0; i-- {
		s = s[:at[i]] + q + "." + s[at[i]:]
	}
	return s, nil
}

// read checks path[0], a read of the field, once the field holds
// only values of the new type: a switch on it loses the cases none of
// them matches, and a comparison with such a value, or passing it to a
// generic function (which would be instantiated with the new type), is
// noted.
func (m narrowing) read(c *fieldChange, p *packages.Package, path []ast.Node, _ ast.Expr, where string) error {
	e := path[0].(ast.Expr)
	info := p.TypesInfo
	switch u := path[1].(type) {
	case *ast.SwitchStmt:
		if u.Tag == e {
			return c.dropCases(p, u)
		}
	case *ast.BinaryExpr:
		other := u.X
		if other == e {
			other = u.Y
		}
		if fv := c.valueFor(p, other); fv != nil && (u.Op == token.EQL || u.Op == token.NEQ) {
			return c.replaceValue(p, other, fv, true)
		}
		if (u.Op == token.EQL || u.Op == token.NEQ) && !c.canMatch(p, other) {
			c.cannot = append(c.cannot, fmt.Sprintf("%s: compared with %s, which no %s equals", c.ws.Position(other.Pos()), types.ExprString(other), c.name))
		}
	case *ast.CallExpr:
		if analysis.Generic(info, u.Fun) && slices.Contains(u.Args, e) {
			c.cannot = append(c.cannot, fmt.Sprintf("%s: passed to %s, which would be instantiated with %s", c.ws.Position(e.Pos()), types.ExprString(u.Fun), c.name))
		}
	}
	return nil
}

// dropCases removes from sw, a switch on the narrowed field, the values
// no value of the new type can match, and a clause left with none. A
// value that is not a constant is noted instead (evaluating it might do
// something), as is a clause reached by fallthrough.
func (c *fieldChange) dropCases(p *packages.Package, sw *ast.SwitchStmt) error {
	info := p.TypesInfo
	fe, err := c.files.of(p, sw.Pos())
	if err != nil {
		return err
	}
	tf := c.ws.Fset.File(sw.Pos())
	lineStart := func(pos token.Pos) int { return tf.Offset(tf.LineStart(tf.Line(pos))) }
	clauses := sw.Body.List
	for i, s := range clauses {
		cc := s.(*ast.CaseClause)
		var kept, dead []string
		for _, v := range cc.List {
			where := c.ws.Position(v.Pos())
			switch {
			case c.canMatch(p, v):
				kept = append(kept, types.ExprString(v))
			case info.Types[v].Value == nil:
				c.cannot = append(c.cannot, fmt.Sprintf("%s: compared with %s, which no %s equals", where, types.ExprString(v), c.name))
			default:
				dead = append(dead, types.ExprString(v))
				c.dropped = append(c.dropped, where+" "+types.ExprString(v))
			}
		}
		switch {
		case len(dead) == 0:
		case len(kept) > 0:
			fe.edits = append(fe.edits, edit.TextEdit{Start: tf.Offset(cc.List[0].Pos()), End: tf.Offset(cc.List[len(cc.List)-1].End()), NewText: strings.Join(kept, ", ")})
		case i > 0 && fallsThrough(clauses[i-1].(*ast.CaseClause)):
			c.cannot = append(c.cannot, fmt.Sprintf("%s: case %s is reached by fallthrough from the case before", c.ws.Position(cc.List[0].Pos()), strings.Join(dead, ", ")))
		default:
			end := sw.Body.Rbrace
			if i+1 < len(clauses) {
				end = clauses[i+1].Pos()
			}
			fe.edits = append(fe.edits, edit.TextEdit{Start: lineStart(cc.Pos()), End: lineStart(end)})
		}
	}
	return nil
}

// canMatch reports whether v can equal a value of the new type: v is nil,
// of an interface type, or of a type that implements the new one.
func (c *fieldChange) canMatch(p *packages.Package, v ast.Expr) bool {
	t := p.TypesInfo.TypeOf(v)
	if b, ok := t.(*types.Basic); ok && b.Kind() == types.UntypedNil {
		return true
	}
	t = types.Default(t)
	return types.IsInterface(t) || types.AssignableTo(t, c.newIn(p))
}

// newIn is the new type as package p sees it: a test variant of a
// package type-checks its own copy of the package's types.
func (c *fieldChange) newIn(p *packages.Package) types.Type {
	n, ok := c.new.(*types.Named)
	if !ok || n.Obj().Pkg() == nil {
		return c.new
	}
	seen := map[*types.Package]bool{}
	var find func(*types.Package) types.Type
	find = func(tp *types.Package) types.Type {
		if seen[tp] {
			return nil
		}
		seen[tp] = true
		if tp.Path() == n.Obj().Pkg().Path() {
			if o, ok := tp.Scope().Lookup(n.Obj().Name()).(*types.TypeName); ok {
				return o.Type()
			}
			return nil
		}
		for _, imp := range tp.Imports() {
			if t := find(imp); t != nil {
				return t
			}
		}
		return nil
	}
	if t := find(p.Types); t != nil {
		return t
	}
	return c.new
}

// fallsThrough reports whether cc ends in a fallthrough statement.
func fallsThrough(cc *ast.CaseClause) bool {
	if len(cc.Body) == 0 {
		return false
	}
	b, ok := cc.Body[len(cc.Body)-1].(*ast.BranchStmt)
	return ok && b.Tok == token.FALLTHROUGH
}

// keeps reports whether the read path[0] of the field means the same once
// the field is an interface: it is a switch's tag, compared with ==, or
// passed as an interface. It returns the values compared with it that
// write must convert: untyped constants, which compared with an interface
// take their default type.
func (c *fieldChange) keeps(info *types.Info, path []ast.Node) (bool, []ast.Expr) {
	e := path[0].(ast.Expr)
	var others []ast.Expr
	switch p := path[1].(type) {
	case *ast.SwitchStmt:
		if p.Tag != e {
			return false, nil
		}
		for _, s := range p.Body.List {
			others = append(others, s.(*ast.CaseClause).List...)
		}
	case *ast.BinaryExpr:
		if p.Op != token.EQL && p.Op != token.NEQ {
			return false, nil
		}
		other := p.X
		if other == e {
			other = p.Y
		}
		others = []ast.Expr{other}
	case *ast.CallExpr:
		return passedAsInterface(info, p, e), nil
	default:
		return false, nil
	}
	var constants []ast.Expr
	for _, o := range others {
		switch t := info.TypeOf(o); {
		case untypedConstant(info, o):
			constants = append(constants, o)
		case types.IsInterface(t), types.Identical(t, c.old):
		default:
			return false, nil
		}
	}
	return true, constants
}

// passedAsInterface reports whether e is an argument of call that it
// converts to an interface: a parameter of interface type, or a
// conversion to one.
func passedAsInterface(info *types.Info, call *ast.CallExpr, e ast.Expr) bool {
	if tv := info.Types[call.Fun]; tv.IsType() {
		return types.IsInterface(tv.Type)
	}
	sig, ok := info.TypeOf(call.Fun).(*types.Signature)
	if !ok {
		return false
	}
	for i, a := range call.Args {
		if a != e {
			continue
		}
		n := sig.Params().Len()
		var pt types.Type
		switch {
		case sig.Variadic() && i >= n-1:
			if call.Ellipsis.IsValid() {
				return false
			}
			pt = sig.Params().At(n - 1).Type().(*types.Slice).Elem()
		case i < n:
			pt = sig.Params().At(i).Type()
		default:
			return false
		}
		_, param := pt.(*types.TypeParam)
		return types.IsInterface(pt) && !param
	}
	return false
}

// defaultType is the type an untyped constant e takes in an interface,
// or nil when that is not plain from e.
func defaultType(info *types.Info, e ast.Expr) types.Type {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return defaultType(info, e.X)
	case *ast.BasicLit:
		return map[token.Token]types.Type{token.INT: types.Typ[types.Int], token.FLOAT: types.Typ[types.Float64], token.IMAG: types.Typ[types.Complex128], token.CHAR: types.Universe.Lookup("rune").Type(), token.STRING: types.Typ[types.String]}[e.Kind]
	case *ast.Ident:
		if k, ok := info.Uses[e].(*types.Const); ok {
			return types.Default(k.Type())
		}
	}
	return nil
}

// callsMethodOf reports whether path[0] is the receiver of a method call
// whose method the interface it has.
func callsMethodOf(info *types.Info, path []ast.Node, it types.Type) bool {
	sel, ok := path[1].(*ast.SelectorExpr)
	if !ok || sel.X != path[0] {
		return false
	}
	obj, _, _ := types.LookupFieldOrMethod(it, false, nil, sel.Sel.Name)
	_, isMethod := obj.(*types.Func)
	return isMethod && info.Selections[sel] != nil
}
