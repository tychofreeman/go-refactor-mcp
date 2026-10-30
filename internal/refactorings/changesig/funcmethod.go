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
	return `Turn a function into a method of its first parameter's type, and change every call: func total(c *Cart) int becomes func (c *Cart) total() int, total(cart) becomes cart.total(), and total(&v) becomes v.total(). The reverse of method_to_function. The first parameter becomes the receiver, or the one receiver names (its argument moves first at each call); its type must be T or *T for a type T of the function's package.
Identify the function by file and name, or by line at its declaration or any call; new_name gives the method another name.
Refuses, saying why: a name that makes T implement an interface while a value of T becomes an interface value (String, Error, or a module's interface), or that T or a type embedding it already has as a field or method; a function used as a value, or generic; an unexported name called from another package.`
}

func (*FunctionToMethod) InputSchema() json.RawMessage {
	return refactor.Schema[f2mArgs]()
}

type f2mArgs struct {
	File     string `json:"file" desc:"Path of a file containing the function's declaration or a call, relative to the workspace root."`
	Line     int    `json:"line,omitempty" desc:"1-based line of that occurrence. Omit it to find name as a symbol of the package of file."`
	Name     string `json:"name" desc:"The function's name; without line, it is found among the package-level names."`
	Column   int    `json:"column,omitempty" desc:"Optional 1-based column, only needed to disambiguate."`
	Receiver string `json:"receiver,omitempty" desc:"Optional: the parameter that becomes the receiver (default: the first)."`
	NewName  string `json:"new_name,omitempty" desc:"Optional name for the method (default: the function's name)."`
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
	}
	r := 0 // the receiver's index among the parameters
	if a.Receiver != "" {
		if r, err = t.paramIndex(a.Receiver); err != nil {
			return nil, err
		}
	}
	if sig.Variadic() && r == len(t.params)-1 {
		return nil, refactor.Refusef("%s is variadic, so it cannot be the receiver", t.params[r].name)
	}
	name := a.NewName
	if name == "" {
		name = t.obj.Name()
	}
	switch {
	case !token.IsIdentifier(name):
		return nil, refactor.Refusef("new_name %q is not a valid Go identifier", name)
	}
	recvVar := sig.Params().At(r)
	named := recvNamed(recvVar.Type())
	if named == nil || named.Obj().Pkg() != t.obj.Pkg() {
		err := refactor.Refusef("the type of %s, %s, is not a type declared in package %s, so it cannot have methods here", recvVar.Name(), t.typeName(recvVar.Type()), t.obj.Pkg().Name())
		if a.Receiver == "" {
			return nil, t.receiverHint(err, name, r)
		}
		return nil, err
	}
	switch named.Underlying().(type) {
	case *types.Pointer, *types.Interface:
		return nil, refactor.Refusef("%s is a pointer or interface type, which cannot have methods", named.Obj().Name())
	}
	if named.TypeParams().Len() > 0 {
		return nil, refactor.Refusef("the type %s has type parameters; this is not supported yet", named.Obj().Name())
	}
	implements, err := t.checkImplements(named, name, r, recvVar.Type() != types.Type(named))
	if err != nil {
		return nil, err
	}
	if token.IsExported(name) {
		// Only through a value that becomes an interface value can a check
		// at run time see the method.
		if iface := analysis.MethodInterface(ws.Pkgs, named, name, true); iface != "" {
			if at := t.becomesInterface(named); at != "" {
				return nil, refactor.Refusef("a method %s could make %s implement %s, which code checks for at run time, and change behavior with no compile error (%s becomes an interface value); pass another new_name", name, named.Obj().Name(), iface, at)
			}
		}
	}
	if err := t.checkNoSelector(named, name); err != nil {
		if a.Receiver == "" {
			return nil, t.receiverHint(err, name, r)
		}
		return nil, err
	}

	path := ws.Fset.Position(t.decl.Pos()).Filename
	fes := []edit.FileEdit{{Path: path, Edits: t.methodDeclEdits(name, r)}}
	srcs := map[string][]byte{path: t.Src}
	for _, c := range t.calls {
		where := ws.Position(c.expr.Pos())
		if c.pkg.PkgPath != t.Pkg.PkgPath && !token.IsExported(name) {
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
		es, err := t.methodCallEdits(c, name, srcs[cpath], r)
		if err != nil {
			return nil, err
		}
		fes = append(fes, edit.FileEdit{Path: cpath, Edits: es})
	}
	plan := t.newPlan(fmt.Sprintf("turned function %s into method %s of %s", t.obj.Name(), name, t.typeName(recvVar.Type())), fes)
	plan.Notes = append(plan.Notes, implements...)
	return plan, nil
}

// checkNoSelector refuses if T, *T or a type that embeds T already has a
// field or method called name: the new method would clash with it, or,
// promoted, change which one x.name means.
func (t *target) checkNoSelector(named *types.Named, name string) error {
	pkg := named.Obj().Pkg()
	if obj, idx, _ := types.LookupFieldOrMethod(types.NewPointer(named), true, pkg, name); obj != nil || idx != nil {
		return refactor.Refusef("%s already has a field or method %s", named.Obj().Name(), name)
	}
	for _, p := range t.WS.Pkgs {
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

// receiverHint adds to err, the refusal of a name the receiver's type
// already has, another parameter whose type could take the method.
func (t *target) receiverHint(err error, name string, r int) error {
	sig := t.obj.Signature()
	for i, p := range t.params {
		n := recvNamed(sig.Params().At(i).Type())
		if i == r || n == nil || n.Obj().Pkg() != t.obj.Pkg() || types.IsInterface(n) {
			continue
		}
		if _, isPtr := n.Underlying().(*types.Pointer); isPtr {
			continue
		}
		if obj, _, _ := types.LookupFieldOrMethod(types.NewPointer(n), true, n.Obj().Pkg(), name); obj == nil {
			return refactor.Refusef("%s; give receiver %q to make it a method of %s", err.Error(), p.name, n.Obj().Name())
		}
	}
	return err
}

// checkImplements refuses a method name that an interface of the module
// has, if with the method (parameter r its receiver) named (through *named only, with ptr) would
// implement the interface while one of its values becomes an interface
// value, where a type assertion or reflection could see the difference.
// It returns a note for each interface it starts to implement unseen. A
// type that embeds named would get the method too, so then any such
// interface is refused.
func (t *target) checkImplements(named *types.Named, name string, r int, ptr bool) ([]string, error) {
	ifaces := analysis.InterfacesWithMethod(t.WS.TypesInfos(), t.obj.Pkg().Path(), name)
	if len(ifaces) == 0 {
		return nil, nil
	}
	for _, p := range t.WS.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, o := range p.TypesInfo.Defs {
			if tn, ok := o.(*types.TypeName); ok && !tn.IsAlias() && typeKey(tn.Type()) != typeKey(named) && embeds(tn.Type(), named, map[types.Type]bool{}) {
				return nil, t.checkNoInterface(name)
			}
		}
	}
	sig := t.obj.Signature()
	var rest []*types.Var
	for i := 0; i < sig.Params().Len(); i++ {
		if i != r {
			rest = append(rest, sig.Params().At(i))
		}
	}
	method := types.NewSignatureType(nil, nil, nil, types.NewTuple(rest...), sig.Results(), sig.Variadic())
	noQualifier := func(*types.Package) string { return "" }
	var notes []string
	for _, it := range ifaces {
		iname := it.Name
		if iname == "" {
			iname = "at " + t.WS.Position(it.At)
		}
		for _, T := range []types.Type{named, types.NewPointer(named)} {
			_, isPtr := T.(*types.Pointer)
			if !implementsWith(T, it.Type, name, method, isPtr || !ptr) {
				continue
			}
			if at := t.becomesInterface(named); at != "" {
				return nil, refactor.Refusef("with a method %s, type %s would implement interface %s, which it does not now; that could change what a type assertion or interface conversion does: %s becomes an interface value", name, types.TypeString(T, noQualifier), iname, at)
			}
			notes = append(notes, fmt.Sprintf("type %s now implements interface %s; no value of it becomes an interface value, so no type assertion or reflection sees the difference", types.TypeString(T, noQualifier), iname))
			break // *T implements what T does: one note for both
		}
	}
	return notes, nil
}

// implementsWith reports whether T implements I once it has a method
// name of signature method (if has; a method on *T only is not in T's
// method set).
func implementsWith(T types.Type, I *types.Interface, name string, method *types.Signature, has bool) bool {
	ms := types.NewMethodSet(T)
	for i := 0; i < I.NumMethods(); i++ {
		im := I.Method(i)
		if im.Name() == name {
			if !has || !types.Identical(method, im.Type()) {
				return false
			}
			continue
		}
		sel := ms.Lookup(im.Pkg(), im.Name())
		if sel == nil || !types.Identical(sel.Obj().Type(), im.Type()) {
			return false
		}
	}
	return true
}

// becomesInterface returns where a value that holds named may become an
// interface value in the workspace, or "".
func (t *target) becomesInterface(named *types.Named) string {
	match := func(T types.Type) bool { return types.Identical(T, named) }
	for _, p := range t.WS.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		if e := analysis.BecomesInterface(p.TypesInfo, p.Syntax, match); e != nil {
			return fmt.Sprintf("%s at %s", types.ExprString(e), t.WS.Position(e.Pos()))
		}
	}
	return ""
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

// methodDeclEdits move parameter r to a receiver, and rename the function
// if asked.
func (t *target) methodDeclEdits(name string, r int) []edit.TextEdit {
	p := t.params[r]
	recv := "(" + p.name + " " + t.Text(p.field.Type) + ") "
	cut := t.paramCut(p)
	return []edit.TextEdit{
		{Start: t.Off(t.decl.Name.Pos()), End: t.Off(t.decl.Name.End()), NewText: recv + name},
		{Start: cut.start, End: cut.end},
	}
}

// methodCallEdits turn the call m(x, args) into x'.name(args), where x,
// argument r, is written as a method's operand: &v becomes v and *p
// becomes p, since the call takes the address or dereferences itself, and
// a value of another type (nil, say) is converted to the receiver type.
func (t *target) methodCallEdits(c call, name string, csrc []byte, r int) ([]edit.TextEdit, error) {
	info := c.pkg.TypesInfo
	arg := c.expr.Args[r]
	if r > 0 {
		if err := t.checkOrder(c, r, 0); err != nil {
			return nil, err
		}
	}
	for _, other := range t.calls {
		if other.expr != c.expr && arg.Pos() <= other.expr.Pos() && other.expr.End() <= arg.End() {
			return nil, refactor.Refusef("the receiver argument of the call at %s contains another call of %s, at %s; assign it to a variable first",
				t.WS.Position(c.expr.Pos()), t.obj.Name(), t.WS.Position(other.expr.Pos()))
		}
	}
	src := func(n ast.Node) string { return string(csrc[t.Off(n.Pos()):t.Off(n.End())]) }
	recvType := t.obj.Signature().Params().At(r).Type()
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
		x = "(" + t.Text(t.params[r].field.Type) + ")(" + src(arg) + ")"
	} else if !simpleOperand(operand) || isCompositeLit(operand) {
		x = "(" + x + ")"
	}
	cut := t.argCut(c.expr, r, false)
	return []edit.TextEdit{
		{Start: t.Off(c.expr.Fun.Pos()), End: t.Off(c.expr.Fun.End()), NewText: x + "." + name},
		{Start: cut.start, End: cut.end},
	}, nil
}

// isCompositeLit reports whether e is a composite literal, which as a
// selector's operand needs parentheses in some statements (if T{} {).
func isCompositeLit(e ast.Expr) bool {
	_, ok := e.(*ast.CompositeLit)
	return ok
}
