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
	return `Turn a method into a function whose first parameter is the receiver, and change every call x.m(a) into m(x, a) (m(&x, a) where the call took x's address, m(*p, a) where it dereferenced p, m(&o.inner, a) for a method promoted from an embedded field).
Use it before moving a method to another package (move_declaration does not move methods there), or when a method uses so little of its receiver that it is really a helper: follow with introduce_parameter and remove_parameter to pass only what it uses.
Identify the method by file, line and name, at its declaration or any call. The function keeps the method's name unless new_name is given; an unnamed receiver gets a name.
Only unexported methods: an exported one could satisfy an interface outside this module (fmt looks for String, errors for Error), and losing it would not be a compile error. Refuses if any interface in the workspace has a method of that name, if the method is used other than by calling it (a method value or method expression), if its receiver type has type parameters, if the package already declares the name, if the name means something else at a call, or for a call whose receiver contains another call of the method.`
}

func (*MethodToFunction) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":     {"type": "string",  "description": "Path of a file containing the method's declaration or a call, relative to the workspace root."},
    "line":     {"type": "integer", "description": "1-based line of that occurrence."},
    "name":     {"type": "string",  "description": "The method's name."},
    "column":   {"type": "integer", "description": "Optional 1-based column, only needed to disambiguate."},
    "new_name": {"type": "string",  "description": "Optional name for the function (default: the method's name)."}
  },
  "required": ["file", "line", "name"],
  "additionalProperties": false
}`)
}

type m2fArgs struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Column  int    `json:"column,omitempty"`
	Name    string `json:"name"`
	NewName string `json:"new_name,omitempty"`
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
	case t.obj.Exported():
		return nil, refactor.Refusef("%s is exported, so an interface outside this module could need it (fmt looks for String, errors for Error), and losing the method would not be a compile error; only unexported methods can become functions", t.obj.Name())
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
	srcs := map[string][]byte{path: t.src}
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
	for _, p := range t.ws.Pkgs {
		if p.Types == nil || p.PkgPath != t.pkg.PkgPath {
			continue
		}
		if o := p.Types.Scope().Lookup(name); o != nil {
			return refactor.Refusef("the package already declares %s (at %s); pass new_name", name, t.ws.Position(o.Pos()))
		}
		for _, f := range p.Syntax {
			if s := p.TypesInfo.Scopes[f]; s != nil && s.Lookup(name) != nil {
				return refactor.Refusef("%s imports a package as %s; pass new_name", t.ws.Rel(t.ws.Fset.Position(f.Pos()).Filename), name)
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
	iface, at, found := analysis.InterfaceWithMethod(t.ws.TypesInfos(), t.obj.Pkg().Path(), name)
	switch {
	case !found:
		return nil
	case iface != nil:
		return refactor.Refusef("the interface %s (at %s) has a method %s, which a type could implement through it; that is not supported", iface.Name(), t.ws.Position(at), name)
	}
	return refactor.Refusef("the interface at %s has a method %s, which a type could implement through it; that is not supported", t.ws.Position(at), name)
}

// recvParam returns the receiver written as a parameter, naming it if the
// method does not.
func (t *target) recvParam(named *types.Named) (string, error) {
	field := t.decl.Recv.List[0]
	typ := t.text(field.Type)
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
	es := []edit.TextEdit{{Start: t.off(t.decl.Recv.Pos()), End: t.off(t.decl.Name.Pos())}}
	if name != t.obj.Name() {
		es = append(es, edit.TextEdit{Start: t.off(t.decl.Name.Pos()), End: t.off(t.decl.Name.End()), NewText: name})
	}
	at := t.off(t.decl.Type.Params.Opening) + 1
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
			t.ws.Position(c.expr.Pos()), name, kind(o), t.ws.Position(o.Pos()))
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
				t.ws.Position(c.expr.Pos()), t.obj.Name(), t.ws.Position(other.expr.Pos()))
		}
	}
	x := string(csrc[t.off(se.X.Pos()):t.off(se.X.End())])
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
	at := t.off(c.expr.Lparen) + 1
	if len(c.expr.Args) > 0 {
		x += ", "
	}
	return []edit.TextEdit{
		{Start: t.off(c.expr.Fun.Pos()), End: t.off(c.expr.Fun.End()), NewText: name},
		{Start: at, End: at, NewText: x},
	}, nil
}

func deref(t types.Type) types.Type {
	if p, ok := t.Underlying().(*types.Pointer); ok {
		return p.Elem()
	}
	return t
}
