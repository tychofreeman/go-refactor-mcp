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
	"strings"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// ChangeParameterType is the "change_parameter_type" refactoring.
type ChangeParameterType struct{}

// NewChangeParameterType returns the change_parameter_type refactoring.
func NewChangeParameterType() *ChangeParameterType { return &ChangeParameterType{} }

func (*ChangeParameterType) Name() string { return "change_parameter_type" }

func (*ChangeParameterType) Description() string {
	return `Change the type of a function's or method's parameter, where every value keeps its meaning, and update the body and every call in the workspace to match.
Use it to make a function accept more (an interface instead of a concrete type, so tests can pass a fake) or a more precise type (a named type such as UserID instead of string), before changing what it does.
Identify the function by file, line and name, at its declaration or any call, and give the parameter's name and its new type (as Go source; packages it names are imported).
Two kinds of change keep behavior:
- To an interface the old type implements (*bytes.Buffer to io.Writer): calls are unchanged, and the body must compile with the new type. Refused if the body compares the parameter with nil and the old type is not an interface (a nil pointer in an interface is not nil).
- Between types with the same underlying type (string and UserID), or to a number type that holds every value of the old one (int32 to int64): typed arguments are converted at calls (UserID(name)), and the body converts the parameter back (string(id)) wherever its type could matter: everywhere but indexing, len, cap, range, and comparisons with a constant. Refused if the body assigns the parameter or takes its address.
To write a parameter's type another way that is the same type (an alias declared for it, type renderArg = string), give that: only the declarations change, a variadic parameter's too (give renderArg or ...renderArg). A variadic parameter's type cannot otherwise change. Also refuses generic types, a call that passes another call's results, and a method that would stop or start implementing an interface declared outside the module. An interface method of the module changes with every method of the module's types that implements it, and such a method with the interface: name any of them.`
}

func (*ChangeParameterType) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":      {"type": "string",  "description": "Path of a file containing the function's declaration or a call, relative to the workspace root."},
    "line":      {"type": "integer", "description": "1-based line of that occurrence. Omit it to find name as a symbol of the package of file."},
    "name":      {"type": "string",  "description": "The function's name: with line, just the method name for a method; without line, the package-level name or Type.Method."},
    "column":    {"type": "integer", "description": "Optional 1-based column, only needed to disambiguate."},
    "parameter": {"type": "string",  "description": "Name of the parameter whose type changes."},
    "type":      {"type": "string",  "description": "The new type, as Go source, e.g. io.Writer or UserID."}
  },
  "required": ["file", "name", "parameter", "type"],
  "additionalProperties": false
}`)
}

// ChecksByCompiling: that the body compiles with the new type is checked
// by compiling it.
func (*ChangeParameterType) ChecksByCompiling() {}

type typeArgs struct {
	File      string `json:"file"`
	Line      int    `json:"line"`
	Column    int    `json:"column,omitempty"`
	Name      string `json:"name"`
	Parameter string `json:"parameter"`
	Type      string `json:"type"`
}

// Plan checks preconditions and computes the edits.
func (*ChangeParameterType) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a typeArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	t, err := findFamily(ws, locate.IdentQuery{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name})
	if err != nil {
		return nil, err
	}
	at, err := t.named().paramIndex(a.Parameter)
	if err != nil {
		return nil, err
	}
	sig := t.obj.Signature()
	variadic := sig.Variadic() && at == len(t.params)-1
	spelled, dots := strings.CutPrefix(strings.TrimSpace(a.Type), "...")
	if dots && !variadic {
		return nil, refactor.Refusef("%s is not variadic, so its type cannot be %s", a.Parameter, a.Type)
	}
	old := sig.Params().At(at).Type()
	if variadic {
		old = old.(*types.Slice).Elem() // the type of each argument, as written after "..."
	}
	newT, err := t.parseType(spelled)
	if err != nil {
		return nil, err
	}
	if containsTypeParam(old) || containsTypeParam(newT) {
		return nil, refactor.Refusef("changing a parameter to or from a type with type parameters is not supported")
	}
	if types.Identical(old, newT) {
		return t.respell(a.Parameter, at, old, newT, variadic)
	}
	if variadic {
		return nil, refactor.Refusef("%s is variadic; its type can only be written another way that is the same type (an alias of %s), not changed", a.Parameter, t.typeName(old))
	}
	widen, err := t.checkConversion(old, newT)
	if err != nil {
		return nil, err
	}
	files := newFileEdits(ws)
	for _, m := range t.group() {
		if err := m.checkInterfaces(func(ps []string) []string { ps[at] = typeKey(newT); return ps }); err != nil {
			return nil, err
		}
		decl, err := files.of(m.pkg, m.declPos())
		if err != nil {
			return nil, err
		}
		p := m.params[at]
		decl.edits = append(decl.edits, m.fieldEdit(p, decl.write(newT)))
		body, err := m.bodyEdits(p, old, newT, widen, decl)
		if err != nil {
			return nil, err
		}
		decl.edits = append(decl.edits, body...)
		if !widen {
			for _, c := range m.calls {
				if err := m.convertArg(files, c, at, newT); err != nil {
					return nil, err
				}
			}
		}
	}
	return t.newPlan(fmt.Sprintf("changed parameter %q of %s from %s to %s", a.Parameter, t.groupName(), t.typeName(old), t.typeName(newT)), files.all()), nil
}

// respell writes the parameter name, at index at, as newT, a type
// identical to its own type old but written another way (an alias), in
// the declaration of every member of the family. Nothing else changes:
// the calls and bodies already use the same type.
func (t *target) respell(name string, at int, old, newT types.Type, variadic bool) (*refactor.Plan, error) {
	files := newFileEdits(t.ws)
	changed := false
	for _, m := range t.group() {
		decl, err := files.of(m.pkg, m.declPos())
		if err != nil {
			return nil, err
		}
		p := m.params[at]
		typ := decl.write(newT)
		if variadic {
			typ = "..." + typ
		}
		if typ == m.text(p.field.Type) {
			continue
		}
		decl.edits = append(decl.edits, m.fieldEdit(p, typ))
		changed = true
	}
	if !changed {
		return nil, refactor.Refusef("%s is already of type %s", name, t.typeName(old))
	}
	return t.newPlan(fmt.Sprintf("wrote parameter %q of %s as %s, the same type as %s", name, t.groupName(), t.typeName(newT), t.typeName(old)), files.all()), nil
}

// checkConversion checks that every value of type old means the same as
// one of newT, and reports whether newT is an interface old is widened
// to (otherwise values are converted).
func (t *target) checkConversion(old, newT types.Type) (bool, error) {
	widen := types.IsInterface(newT) && types.AssignableTo(old, newT)
	if !widen && !types.Identical(old.Underlying(), newT.Underlying()) {
		if isNumber(old) && isNumber(newT) {
			if !widens(old, newT) {
				return false, refactor.Refusef("%s cannot hold every value of %s, so converting the arguments could change them", t.typeName(newT), t.typeName(old))
			}
		} else {
			return false, refactor.Refusef("a value of type %s cannot become one of type %s without changing what it is: change_parameter_type handles an interface the old type implements, a type with the same underlying type, or a number type that holds every value of the old one", t.typeName(old), t.typeName(newT))
		}
	}
	return widen, nil
}

// parseType evaluates the type expression src in the declaring file. A
// package it names that the file does not import is looked up among the
// packages loaded.
func (t *target) parseType(src string) (types.Type, error) {
	if typ, err := t.evalType(src); err == nil {
		return typ, nil
	}
	expr, err := parser.ParseExpr(src)
	if err != nil {
		return nil, refactor.Refusef("the type %q is not Go: %v", src, err)
	}
	// A package with the declaring package's names, the declaring file's
	// imports, and the packages named in src that the file lacks.
	scope := types.NewPackage(t.pkg.PkgPath, t.pkg.Name)
	for _, n := range t.pkg.Types.Scope().Names() {
		scope.Scope().Insert(t.pkg.Types.Scope().Lookup(n))
	}
	for _, f := range t.pkg.Syntax {
		if t.ws.Fset.File(f.Pos()) != t.ws.Fset.File(t.declPos()) {
			continue
		}
		for _, spec := range f.Imports {
			if pn := t.pkg.TypesInfo.PkgNameOf(spec); pn != nil {
				scope.Scope().Insert(pn)
			}
		}
	}
	loaded := loadedPackages(t.ws)
	var missing error
	ast.Inspect(expr, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		x, ok := sel.X.(*ast.Ident)
		if !ok || scope.Scope().Lookup(x.Name) != nil {
			return true
		}
		switch found := loaded[x.Name]; len(found) {
		case 1:
			scope.Scope().Insert(types.NewPkgName(token.NoPos, scope, x.Name, found[0]))
		case 0:
			missing = refactor.Refusef("no package %s is loaded; import it in %s first", x.Name, t.ws.Rel(t.declFile()))
		default:
			missing = refactor.Refusef("several packages are named %s; import the one you mean in %s first", x.Name, t.ws.Rel(t.declFile()))
		}
		return false
	})
	if missing != nil {
		return nil, missing
	}
	tv, err := types.Eval(t.ws.Fset, scope, token.NoPos, src)
	if err != nil || !tv.IsType() {
		reason := "it is not a type"
		if err != nil {
			reason = err.Error()
		}
		return nil, refactor.Refusef("the type %q is not valid here: %s", src, reason)
	}
	return tv.Type, nil
}

// loadedPackages maps package names to the packages of that name the
// workspace's packages import, directly or not.
func loadedPackages(ws *workspace.Workspace) map[string][]*types.Package {
	out := map[string][]*types.Package{}
	seen := map[string]bool{}
	packages.Visit(ws.Pkgs, func(p *packages.Package) bool {
		if p.Types != nil && !seen[p.PkgPath] && !strings.HasSuffix(p.ID, ".test") {
			seen[p.PkgPath] = true
			out[p.Types.Name()] = append(out[p.Types.Name()], p.Types)
		}
		return true
	}, nil)
	return out
}

// fieldEdit gives the parameter p the type typ, splitting its group if
// it shares one (a, b string to a string, b Name).
func (t *target) fieldEdit(p param, typ string) edit.TextEdit {
	f := p.field
	if len(f.Names) <= 1 {
		return edit.TextEdit{Start: t.off(f.Type.Pos()), End: t.off(f.Type.End()), NewText: typ}
	}
	old := t.text(f.Type)
	names := func(ids []*ast.Ident) string {
		var ns []string
		for _, id := range ids {
			ns = append(ns, id.Name)
		}
		return strings.Join(ns, ", ")
	}
	var parts []string
	if p.index > 0 {
		parts = append(parts, names(f.Names[:p.index])+" "+old)
	}
	parts = append(parts, p.name+" "+typ)
	if p.index < len(f.Names)-1 {
		parts = append(parts, names(f.Names[p.index+1:])+" "+old)
	}
	return edit.TextEdit{Start: t.off(f.Pos()), End: t.off(f.End()), NewText: strings.Join(parts, ", ")}
}

// bodyEdits checks the body's uses of p, and when its type is converted,
// converts it back where its type could matter.
func (t *target) bodyEdits(p param, old, newT types.Type, widen bool, decl *fileEdits) ([]edit.TextEdit, error) {
	if t.decl == nil {
		return nil, nil // an interface method has no body
	}
	info := t.pkg.TypesInfo
	v := info.Defs[p.field.Names[p.index]]
	var file *ast.File
	for _, f := range t.pkg.Syntax {
		if t.ws.Fset.File(f.Pos()) == t.ws.Fset.File(t.decl.Pos()) {
			file = f
		}
	}
	var edits []edit.TextEdit
	var err error
	ast.Inspect(t.decl.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || err != nil || info.Uses[id] != v {
			return err == nil
		}
		path, _ := astutil.PathEnclosingInterval(file, id.Pos(), id.End())
		line := t.ws.Fset.Position(id.Pos()).Line
		if widen {
			if b, ok := path[1].(*ast.BinaryExpr); ok && !types.IsInterface(old) && (info.Types[b.X].IsNil() || info.Types[b.Y].IsNil()) {
				err = refactor.Refusef("the body compares %s with nil on line %d; held in the interface %s, a nil %s is not nil, so that would change", p.name, line, t.typeName(newT), t.typeName(old))
			}
			return false
		}
		err = t.checkTypeKept(p, newT, path, line)
		if err == nil && !typeIrrelevant(info, path, types.Identical(old.Underlying(), newT.Underlying())) {
			edits = append(edits, t.conversion(id, decl.write(old))...)
		}
		return false
	})
	return edits, err
}

// checkTypeKept refuses a use, path[0], of the parameter p that needs it
// to keep its type: an assignment to it, or its address.
func (t *target) checkTypeKept(p param, newT types.Type, path []ast.Node, line int) error {
	switch u := path[1].(type) {
	case *ast.AssignStmt:
		if slices.Contains(u.Lhs, path[0].(ast.Expr)) {
			return refactor.Refusef("the body assigns %s on line %d, which would then need a value of type %s", p.name, line, t.typeName(newT))
		}
	case *ast.IncDecStmt:
		return refactor.Refusef("the body assigns %s on line %d, which would then need a value of type %s", p.name, line, t.typeName(newT))
	case *ast.UnaryExpr:
		if u.Op == token.AND {
			return refactor.Refusef("the body takes the address of %s on line %d, whose type would change", p.name, line)
		}
	}
	return nil
}

// conversion converts the expression n to the type written typ.
func (t *target) conversion(n ast.Node, typ string) []edit.TextEdit {
	if strings.HasPrefix(typ, "*") || strings.HasPrefix(typ, "<-") || strings.HasPrefix(typ, "func") {
		typ = "(" + typ + ")"
	}
	return []edit.TextEdit{{Start: t.off(n.Pos()), End: t.off(n.Pos()), NewText: typ + "("},
		{Start: t.off(n.End()), End: t.off(n.End()), NewText: ")"}}
}

// typeIrrelevant reports whether the use path[0] of a parameter works the
// same with either type: an index, an argument of len or cap, what a range
// ranges over, a comparison with a constant, or (with the same underlying
// type) indexed itself.
func typeIrrelevant(info *types.Info, path []ast.Node, sameUnderlying bool) bool {
	e := path[0].(ast.Expr)
	switch p := path[1].(type) {
	case *ast.IndexExpr:
		return p.Index == e || (p.X == e && sameUnderlying)
	case *ast.SliceExpr:
		return p.Low == e || p.High == e || p.Max == e
	case *ast.RangeStmt:
		return p.X == e
	case *ast.CallExpr:
		if id, ok := ast.Unparen(p.Fun).(*ast.Ident); ok {
			if b, ok := info.Uses[id].(*types.Builtin); ok && (b.Name() == "len" || b.Name() == "cap") {
				return true
			}
		}
	case *ast.BinaryExpr:
		other := p.X
		if other == e {
			other = p.Y
		}
		switch p.Op {
		case token.EQL, token.NEQ, token.LSS, token.GTR, token.LEQ, token.GEQ:
			return info.Types[other].Value != nil
		}
	}
	return false
}

// convertArg converts the argument at index at of call c to typ, unless
// it is an untyped constant, which takes the new type as it did the old.
func (t *target) convertArg(files *fileSet, c call, at int, typ types.Type) error {
	where := t.ws.Position(c.expr.Pos())
	if err := checkNoSpread(c, where); err != nil {
		return err
	}
	arg := c.expr.Args[at]
	if untypedConstant(c.pkg.TypesInfo, arg) {
		return nil
	}
	fe, err := files.of(c.pkg, c.expr.Pos())
	if err != nil {
		return err
	}
	fe.edits = append(fe.edits, t.conversion(arg, fe.write(typ))...)
	return nil
}

// untypedConstant reports whether e is an untyped constant expression:
// literals and untyped constants, combined by operators. (The type checker
// records such an argument with the type it was converted to.)
func untypedConstant(info *types.Info, e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.BasicLit:
		return true
	case *ast.ParenExpr:
		return untypedConstant(info, e.X)
	case *ast.UnaryExpr:
		return untypedConstant(info, e.X)
	case *ast.BinaryExpr:
		return untypedConstant(info, e.X) && untypedConstant(info, e.Y)
	case *ast.Ident:
		c, ok := info.Uses[e].(*types.Const)
		if !ok {
			return false
		}
		b, ok := c.Type().(*types.Basic)
		return ok && b.Info()&types.IsUntyped != 0
	}
	return false
}

// isNumber reports whether t is an integer or floating-point type.
func isNumber(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&(types.IsInteger|types.IsFloat) != 0
}

// widens reports whether every value of the number type from is one of
// to: a wider integer of the same signedness, an unsigned one in a wider
// signed one, or float32 in float64. int, uint and uintptr count as 64
// bits wide as a source and 32 as a target, to hold on every platform.
func widens(from, to types.Type) bool {
	f, t := from.Underlying().(*types.Basic), to.Underlying().(*types.Basic)
	if f.Info()&types.IsFloat != 0 || t.Info()&types.IsFloat != 0 {
		return f.Kind() == t.Kind() || f.Kind() == types.Float32 && t.Kind() == types.Float64
	}
	size := func(b *types.Basic, platform int64) int64 {
		switch b.Kind() {
		case types.Int, types.Uint, types.Uintptr:
			return platform
		}
		return types.SizesFor("gc", "amd64").Sizeof(b)
	}
	fs, ts := size(f, 8), size(t, 4)
	fu, tu := f.Info()&types.IsUnsigned != 0, t.Info()&types.IsUnsigned != 0
	switch {
	case fu == tu:
		return ts >= fs
	case fu:
		return ts > fs
	}
	return false
}

// fileEdits are the edits to one file, with the imports they need.
type fileEdits struct {
	path  string
	adder *imports.Adder
	edits []edit.TextEdit
}

// write writes typ for this file, importing the packages it names.
func (fe *fileEdits) write(typ types.Type) string {
	return types.TypeString(typ, func(p *types.Package) string {
		name, err := fe.adder.Name(p)
		if err != nil {
			return p.Name()
		}
		return name
	})
}

// fileSet collects edits by file.
type fileSet struct {
	ws    *workspace.Workspace
	by    map[string]*fileEdits
	order []string
}

func newFileEdits(ws *workspace.Workspace) *fileSet {
	return &fileSet{ws: ws, by: map[string]*fileEdits{}}
}

// of returns the edits of the file of pkg that contains pos.
func (s *fileSet) of(pkg *packages.Package, pos token.Pos) (*fileEdits, error) {
	path := s.ws.Fset.Position(pos).Filename
	if fe := s.by[path]; fe != nil {
		return fe, nil
	}
	var file *ast.File
	for _, f := range pkg.Syntax {
		if s.ws.Fset.File(f.Pos()) == s.ws.Fset.File(pos) {
			file = f
		}
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fe := &fileEdits{path: path, adder: imports.New(s.ws, pkg, file, src)}
	s.by[path] = fe
	s.order = append(s.order, path)
	return fe, nil
}

// all is every file's edits, imports included.
func (s *fileSet) all() []edit.FileEdit {
	var out []edit.FileEdit
	for _, p := range s.order {
		fe := s.by[p]
		out = append(out, edit.FileEdit{Path: p, Edits: append(fe.edits, fe.adder.Edits()...)})
	}
	return out
}
