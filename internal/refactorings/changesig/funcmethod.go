package changesig

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// FunctionToMethod is the "function_to_method" refactoring.
type FunctionToMethod struct{}

// NewFunctionToMethod returns the function_to_method refactoring.
func NewFunctionToMethod() *FunctionToMethod { return &FunctionToMethod{} }

func (*FunctionToMethod) Name() string { return "function_to_method" }

func (*FunctionToMethod) Description() string {
	return `Turn a function into a method of its first parameter's type, and change every call: func total(c *Cart) int becomes func (c *Cart) total() int, total(cart) becomes cart.total(), and total(&v) becomes v.total(). The reverse of method_to_function. The first parameter becomes the receiver (move_parameter first to choose another); its type must be T or *T for a type T of the function's package.
Identify the function by file and name, or by line at its declaration or any call; new_name gives the method another name.
Refuses, saying why: a name that some interface in the workspace has, or that T or a type embedding it already has as a field or method; a function used as a value, or generic; an exported name that could make T implement an interface checked at run time (String, Error, MarshalJSON).`
}

func (*FunctionToMethod) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":     {"type": "string",  "description": "Path of a file containing the function's declaration or a call, relative to the workspace root."},
    "line":     {"type": "integer", "description": "1-based line of that occurrence. Omit it to find name as a symbol of the package of file."},
    "name":     {"type": "string",  "description": "The function's name; without line, it is found among the package-level names."},
    "column":   {"type": "integer", "description": "Optional 1-based column, only needed to disambiguate."},
    "receiver": {"type": "string",  "description": "Optional: the parameter that becomes the receiver; it must be the first (the default)."},
    "new_name": {"type": "string",  "description": "Optional name for the method (default: the function's name)."}
  },
  "required": ["file", "name"],
  "additionalProperties": false
}`)
}

type f2mArgs struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Column   int    `json:"column,omitempty"`
	Name     string `json:"name"`
	Receiver string `json:"receiver,omitempty"`
	NewName  string `json:"new_name,omitempty"`
}

// Plan checks preconditions and computes the edits.
func (*FunctionToMethod) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a f2mArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	t, err := findTarget(ws, locate.IdentQuery{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name})
	if err != nil {
		return nil, err
	}
	sig := t.obj.Signature()
	switch {
	case sig.Recv() != nil:
		return nil, refactor.Refusef("%s is already a method", t.obj.Name())
	case sig.TypeParams().Len() > 0:
		return nil, refactor.Refusef("%s has type parameters; a method cannot declare its own", t.obj.Name())
	case len(t.params) == 0:
		return nil, refactor.Refusef("%s has no parameters; the first one becomes the receiver", t.obj.Name())
	case a.Receiver != "" && a.Receiver != t.params[0].name:
		return nil, refactor.Refusef("%s is not the first parameter of %s; move it there first with move_parameter", a.Receiver, t.obj.Name())
	case sig.Variadic() && len(t.params) == 1:
		return nil, refactor.Refusef("the only parameter of %s is variadic, so it cannot be the receiver", t.obj.Name())
	}
	name := a.NewName
	if name == "" {
		name = t.obj.Name()
	}
	switch {
	case !token.IsIdentifier(name):
		return nil, refactor.Refusef("new_name %q is not a valid Go identifier", name)
	}
	recvVar := sig.Params().At(0)
	named := recvNamed(recvVar.Type())
	if named == nil || named.Obj().Pkg() != t.obj.Pkg() {
		return nil, refactor.Refusef("the type of %s, %s, is not a type declared in package %s, so it cannot have methods here", recvVar.Name(), t.typeName(recvVar.Type()), t.obj.Pkg().Name())
	}
	switch named.Underlying().(type) {
	case *types.Pointer, *types.Interface:
		return nil, refactor.Refusef("%s is a pointer or interface type, which cannot have methods", named.Obj().Name())
	}
	if named.TypeParams().Len() > 0 {
		return nil, refactor.Refusef("the type %s has type parameters; this is not supported yet", named.Obj().Name())
	}
	if err := t.checkNoInterface(name); err != nil {
		return nil, err
	}
	if token.IsExported(name) {
		if iface := analysis.MethodInterface(ws.Pkgs, named, name, true); iface != "" {
			return nil, refactor.Refusef("a method %s could make %s implement %s, which code checks for at run time, and change behavior with no compile error; pass another new_name", name, named.Obj().Name(), iface)
		}
	}
	if err := t.checkNoSelector(named, name); err != nil {
		return nil, err
	}

	path := ws.Fset.Position(t.decl.Pos()).Filename
	fes := []edit.FileEdit{{Path: path, Edits: t.methodDeclEdits(name)}}
	srcs := map[string][]byte{path: t.src}
	for _, c := range t.calls {
		where := ws.Position(c.expr.Pos())
		if c.pkg.PkgPath != t.pkg.PkgPath && !token.IsExported(name) {
			return nil, refactor.Refusef("the call at %s is in another package, which cannot call the unexported method %s", where, name)
		}
		if err := checkNoSpread(c, where); err != nil {
			return nil, err
		}
		cpath := ws.Fset.Position(c.expr.Pos()).Filename
		if srcs[cpath] == nil {
			if srcs[cpath], err = os.ReadFile(cpath); err != nil {
				return nil, err
			}
		}
		es, err := t.methodCallEdits(c, name, srcs[cpath])
		if err != nil {
			return nil, err
		}
		fes = append(fes, edit.FileEdit{Path: cpath, Edits: es})
	}
	return t.newPlan(fmt.Sprintf("turned function %s into method %s of %s", t.obj.Name(), name, t.typeName(recvVar.Type())), fes), nil
}

// checkNoSelector refuses if T, *T or a type that embeds T already has a
// field or method called name: the new method would clash with it, or,
// promoted, change which one x.name means.
func (t *target) checkNoSelector(named *types.Named, name string) error {
	pkg := named.Obj().Pkg()
	if obj, idx, _ := types.LookupFieldOrMethod(types.NewPointer(named), true, pkg, name); obj != nil || idx != nil {
		return refactor.Refusef("%s already has a field or method %s", named.Obj().Name(), name)
	}
	for _, p := range t.ws.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, o := range p.TypesInfo.Defs {
			tn, ok := o.(*types.TypeName)
			if !ok || tn.IsAlias() || typeKey(tn.Type()) == typeKey(named) || !embeds(tn.Type(), named, map[types.Type]bool{}) {
				continue
			}
			if obj, idx, _ := types.LookupFieldOrMethod(types.NewPointer(tn.Type()), true, pkg, name); obj != nil || idx != nil {
				return refactor.Refusef("%s embeds %s and already has a field or method %s, which the new method could hide or make ambiguous", tn.Name(), named.Obj().Name(), name)
			}
		}
	}
	return nil
}

// embeds reports whether the struct underlying t embeds named, directly or
// through other embedded fields.
func embeds(t types.Type, named *types.Named, seen map[types.Type]bool) bool {
	if seen[t] {
		return false
	}
	seen[t] = true
	st, ok := deref(t).Underlying().(*types.Struct)
	if !ok {
		return false
	}
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		if !f.Embedded() {
			continue
		}
		if ft := deref(f.Type()); typeKey(ft) == typeKey(named) || embeds(ft, named, seen) {
			return true
		}
	}
	return false
}

// methodDeclEdits move the first parameter to a receiver, and rename the
// function if asked.
func (t *target) methodDeclEdits(name string) []edit.TextEdit {
	p := t.params[0]
	f := p.field
	recv := "(" + p.name + " " + t.text(f.Type) + ") "
	es := []edit.TextEdit{{Start: t.off(t.decl.Name.Pos()), End: t.off(t.decl.Name.End()), NewText: recv + name}}
	list := t.decl.Type.Params.List
	switch {
	case len(f.Names) > 1: // a, b *T: drop "a, "
		es = append(es, edit.TextEdit{Start: t.off(f.Names[0].Pos()), End: t.off(f.Names[1].Pos())})
	case len(list) > 1:
		es = append(es, edit.TextEdit{Start: t.off(f.Pos()), End: t.off(list[1].Pos())})
	default:
		es = append(es, edit.TextEdit{Start: t.off(f.Pos()), End: t.off(f.End())})
	}
	return es
}

// methodCallEdits turn the call m(x, args) into x'.name(args), where x' is
// x written as a method's operand: &v becomes v and *p becomes p, since the
// call takes the address or dereferences itself, and a value of another
// type (nil, say) is converted to the receiver type.
func (t *target) methodCallEdits(c call, name string, csrc []byte) ([]edit.TextEdit, error) {
	info := c.pkg.TypesInfo
	arg := c.expr.Args[0]
	for _, other := range t.calls {
		if other.expr != c.expr && arg.Pos() <= other.expr.Pos() && other.expr.End() <= arg.End() {
			return nil, refactor.Refusef("the receiver argument of the call at %s contains another call of %s, at %s; assign it to a variable first",
				t.ws.Position(c.expr.Pos()), t.obj.Name(), t.ws.Position(other.expr.Pos()))
		}
	}
	src := func(n ast.Node) string { return string(csrc[t.off(n.Pos()):t.off(n.End())]) }
	recvType := t.obj.Signature().Params().At(0).Type()
	var x string
	operand := ast.Unparen(arg)
	switch e := operand.(type) {
	case *ast.UnaryExpr:
		if _, lit := ast.Unparen(e.X).(*ast.CompositeLit); e.Op == token.AND && !lit {
			operand = e.X // &v: the call takes v's address itself
		}
	case *ast.StarExpr:
		if !analysis.IsPointer(recvType) {
			operand = e.X // *p: the call dereferences p itself
		}
	}
	x = src(operand)
	if typeKey(info.TypeOf(arg)) != typeKey(recvType) { // by name: a test variant has its own types
		x = "(" + t.text(t.params[0].field.Type) + ")(" + src(arg) + ")"
	} else if !simpleOperand(operand) || isCompositeLit(operand) {
		x = "(" + x + ")"
	}
	es := []edit.TextEdit{{Start: t.off(c.expr.Fun.Pos()), End: t.off(c.expr.Fun.End()), NewText: x + "." + name}}
	if len(c.expr.Args) > 1 {
		es = append(es, edit.TextEdit{Start: t.off(arg.Pos()), End: t.off(c.expr.Args[1].Pos())})
	} else {
		es = append(es, edit.TextEdit{Start: t.off(arg.Pos()), End: t.off(c.expr.Rparen)})
	}
	return es, nil
}

// isCompositeLit reports whether e is a composite literal, which as a
// selector's operand needs parentheses in some statements (if T{} {).
func isCompositeLit(e ast.Expr) bool {
	_, ok := e.(*ast.CompositeLit)
	return ok
}
