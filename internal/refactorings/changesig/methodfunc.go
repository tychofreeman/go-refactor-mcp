package changesig

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"unicode"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// MethodToFunction is the "method_to_function" refactoring.
type MethodToFunction struct{}

// NewMethodToFunction returns the method_to_function refactoring.
func NewMethodToFunction() *MethodToFunction { return &MethodToFunction{} }

func (*MethodToFunction) Name() string { return "method_to_function" }

func (*MethodToFunction) Description() string {
	return `Turn a method into a function whose first parameter is the receiver, and change every call: func (c *Cart) total() int becomes func total(c *Cart) int, cart.total() becomes total(cart), and v.total() on a Cart variable total(&v). Use it before moving a method to another package, or when a method uses little of its receiver: then introduce_parameter and remove_parameter to pass only what it uses.
Identify the method by file and name (Type.Method), or by line at its declaration or any call; new_name gives the function another name.
Refuses, saying why: a method that some interface in the workspace has, a method used as a value, a generic receiver, a name already taken, an exported method through which the type may implement an interface checked at run time (String, Error, MarshalJSON).`
}

func (*MethodToFunction) InputSchema() json.RawMessage {
	return refactor.Schema[m2fArgs]()
}

type m2fArgs struct {
	File    string `json:"file" desc:"Path of a file containing the method's declaration or a call, relative to the workspace root."`
	Line    int    `json:"line,omitempty" desc:"1-based line of that occurrence. Omit it to find name as a symbol of the package of file."`
	Name    string `json:"name" desc:"The method's name; without line, Type.Method."`
	Column  int    `json:"column,omitempty" desc:"Optional 1-based column, only needed to disambiguate."`
	NewName string `json:"new_name,omitempty" desc:"Optional name for the function (default: the method's name)."`
}

// Plan checks preconditions and computes the edits.
func (*MethodToFunction) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a m2fArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	t, err := findTarget(ws, locate.IdentQuery{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name})
	if err != nil {
		return nil, err
	}
	recv := t.obj.Signature().Recv()
	if recv == nil {
		return nil, refactor.Refusef("%s is already a function, not a method", t.obj.Name())
	}
	named := recvNamed(recv.Type())
	switch {
	case t.obj.Exported() && named != nil && analysis.MethodInterface(ws.Pkgs, named, t.obj.Name(), false) != "":
		return nil, refactor.Refusef("%s may be how %s implements %s, which code checks for at run time; losing the method would change behavior with no compile error", t.obj.Name(), named.Obj().Name(), analysis.MethodInterface(ws.Pkgs, named, t.obj.Name(), false))
	case named != nil && named.TypeParams().Len() > 0:
		return nil, refactor.Refusef("the receiver type %s has type parameters, which the function would have to declare; this is not supported yet", named.Obj().Name())
	}
	name := a.NewName
	if name == "" {
		name = t.obj.Name()
	}
	if !token.IsIdentifier(name) {
		return nil, refactor.Refusef("new_name %q is not a valid Go identifier", name)
	}
	if err := t.checkFuncName(name); err != nil {
		return nil, err
	}
	if err := t.checkNoInterface(t.obj.Name()); err != nil {
		return nil, err
	}
	recvParam, err := t.recvParam(named)
	if err != nil {
		return nil, err
	}

	path := ws.Fset.Position(t.decl.Pos()).Filename
	fes := []edit.FileEdit{{Path: path, Edits: t.funcDeclEdits(name, recvParam)}}
	srcs := map[string][]byte{path: t.Src}
	for _, c := range t.calls {
		where := ws.Position(c.expr.Pos())
		if err := checkNoSpread(c, where); err != nil {
			return nil, err
		}
		if err := t.checkNameAtCall(c, name); err != nil {
			return nil, err
		}
		cpath := ws.Fset.Position(c.expr.Pos()).Filename
		if srcs[cpath] == nil {
			if srcs[cpath], err = os.ReadFile(cpath); err != nil {
				return nil, err
			}
		}
		es, err := t.funcCallEdits(c, name, srcs[cpath])
		if err != nil {
			return nil, err
		}
		fes = append(fes, edit.FileEdit{Path: cpath, Edits: es})
	}
	return t.newPlan(fmt.Sprintf("turned method %s into function %s", t.obj.Name(), name), fes), nil
}

// recvNamed returns the named type of a receiver type T or *T, or nil.
func recvNamed(t types.Type) *types.Named {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, _ := t.(*types.Named)
	return n
}

// checkFuncName refuses a function name the package (any variant of it)
// already declares, that a file of it imports a package as, or that is
// predeclared.
func (t *target) checkFuncName(name string) error {
	if types.Universe.Lookup(name) != nil {
		return refactor.Refusef("%s is predeclared; a function of that name would hide it; pass new_name", name)
	}
	for _, p := range t.WS.Pkgs {
		if p.Types == nil || p.PkgPath != t.Pkg.PkgPath {
			continue
		}
		if o := p.Types.Scope().Lookup(name); o != nil {
			return refactor.Refusef("the package already declares %s (at %s); pass new_name", name, t.WS.Position(o.Pos()))
		}
		for _, f := range p.Syntax {
			if s := p.TypesInfo.Scopes[f]; s != nil && s.Lookup(name) != nil {
				return refactor.Refusef("%s imports a package as %s; pass new_name", t.WS.Rel(t.WS.Fset.Position(f.Pos()).Filename), name)
			}
		}
	}
	return nil
}

// checkNoInterface refuses if any interface in the workspace has a method
// called name in the target's package: some type might implement it
// through a method of that name (directly or through embedding), and would
// stop or start to.
func (t *target) checkNoInterface(name string) error {
	iface, at, found := analysis.InterfaceWithMethod(t.WS.TypesInfos(), t.obj.Pkg().Path(), name)
	switch {
	case !found:
		return nil
	case iface != nil:
		return refactor.Refusef("the interface %s (at %s) has a method %s, which a type could implement through it; that is not supported", iface.Name(), t.WS.Position(at), name)
	}
	return refactor.Refusef("the interface at %s has a method %s, which a type could implement through it; that is not supported", t.WS.Position(at), name)
}

// recvParam returns the receiver written as a parameter, naming it if the
// method does not.
func (t *target) recvParam(named *types.Named) (string, error) {
	field := t.decl.Recv.List[0]
	typ := t.Text(field.Type)
	if len(field.Names) > 0 {
		return field.Names[0].Name + " " + typ, nil
	}
	var candidates []string
	if named != nil {
		r := []rune(named.Obj().Name())
		candidates = append(candidates, string(unicode.ToLower(r[0])))
	}
	candidates = append(candidates, "recv")
	for _, c := range candidates {
		if token.IsIdentifier(c) && t.checkNewName(c) == nil && types.Universe.Lookup(c) == nil {
			return c + " " + typ, nil
		}
	}
	return "", refactor.Refusef("could not find a name for the receiver of %s; name it first", t.obj.Name())
}

// funcDeclEdits drop the receiver, rename the method if asked, and make
// the receiver the first parameter.
func (t *target) funcDeclEdits(name, recvParam string) []edit.TextEdit {
	es := []edit.TextEdit{{Start: t.Off(t.decl.Recv.Pos()), End: t.Off(t.decl.Name.Pos())}}
	if name != t.obj.Name() {
		es = append(es, edit.TextEdit{Start: t.Off(t.decl.Name.Pos()), End: t.Off(t.decl.Name.End()), NewText: name})
	}
	at := t.Off(t.decl.Type.Params.Opening) + 1
	text := recvParam
	if len(t.decl.Type.Params.List) > 0 {
		text += ", "
	}
	return append(es, edit.TextEdit{Start: at, End: at, NewText: text})
}

// checkNameAtCall refuses a call where name would not refer to the new
// function: a local declaration hides it.
func (t *target) checkNameAtCall(c call, name string) error {
	scope := c.pkg.Types.Scope().Innermost(c.expr.Pos())
	if scope == nil {
		return nil
	}
	if _, o := scope.LookupParent(name, c.expr.Pos()); o != nil && o.Parent() != o.Pkg().Scope() {
		return refactor.Refusef("at the call at %s, %s is the %s declared at %s; pass new_name, or rename it first",
			t.WS.Position(c.expr.Pos()), name, kind(o), t.WS.Position(o.Pos()))
	}
	return nil
}

// funcCallEdits turn the call x.m(args) into name(x', args), where x' is
// the receiver the method gets: through the embedded fields the selector
// goes through, its address taken or dereferenced as the call did.
func (t *target) funcCallEdits(c call, name string, csrc []byte) ([]edit.TextEdit, error) {
	se := ast.Unparen(c.expr.Fun).(*ast.SelectorExpr)
	info := c.pkg.TypesInfo
	sel := info.Selections[se]
	for _, other := range t.calls {
		if other.expr != c.expr && se.X.Pos() <= other.expr.Pos() && other.expr.End() <= se.X.End() {
			return nil, refactor.Refusef("the receiver of the call at %s contains another call of %s, at %s; assign it to a variable first",
				t.WS.Position(c.expr.Pos()), t.obj.Name(), t.WS.Position(other.expr.Pos()))
		}
	}
	x := string(csrc[t.Off(se.X.Pos()):t.Off(se.X.End())])
	if !simpleOperand(se.X) {
		x = "(" + x + ")"
	}
	typ := info.TypeOf(se.X)
	path := sel.Index()
	for _, i := range path[:len(path)-1] {
		st, _ := deref(typ).Underlying().(*types.Struct)
		if st == nil {
			return nil, fmt.Errorf("internal error: no struct on the path of %s", types.ExprString(se))
		}
		f := st.Field(i)
		x += "." + f.Name()
		typ = f.Type()
	}
	recvPtr, isPtr := analysis.IsPointer(t.obj.Signature().Recv().Type()), analysis.IsPointer(typ)
	switch {
	case recvPtr && !isPtr:
		x = "&" + x
	case !recvPtr && isPtr:
		x = "*" + x
	}
	at := t.Off(c.expr.Lparen) + 1
	if len(c.expr.Args) > 0 {
		x += ", "
	}
	return []edit.TextEdit{
		{Start: t.Off(c.expr.Fun.Pos()), End: t.Off(c.expr.Fun.End()), NewText: name},
		{Start: at, End: at, NewText: x},
	}, nil
}

func deref(t types.Type) types.Type {
	if p, ok := t.Underlying().(*types.Pointer); ok {
		return p.Elem()
	}
	return t
}
