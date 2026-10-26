// Package changesig implements refactorings that change a function's
// signature and update every call to match: add_parameter,
// remove_parameter, move_parameter, introduce_parameter,
// change_parameter_type, function_to_method and method_to_function.
//
// They share two requirements. Every reference to the function must be a
// call: a function value (`f := fn`, passing fn as an argument) has the
// function's type, which the change would alter. And a method must not
// stop or start implementing any interface (see checkInterfaces), which
// could change what a type assertion does without a compile error.
package changesig

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/site"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// target is the function whose signature changes.
type target struct {
	site.Site
	obj   *types.Func
	key   string
	decl  *ast.FuncDecl // the declaration in pkg
	ftype *ast.FuncType // the signature: decl's, or for an interface method (decl nil) the one in the interface
	// params flattens the parameter list: one entry per name.
	params []param
	calls  []call
	fixes  []edit.FileEdit // further edits the change needs, beyond the signature and the calls
	// members is, for the target the model named, every method that
	// changes with it: an interface method of the module, the methods of
	// the module's types that implement it, and so on (see familyOf). Nil
	// for a lone function or method.
	members []*target
	// familyKeys are the members' keys, in each member.
	familyKeys map[string]bool
}

type param struct {
	field *ast.Field
	index int // of the name within field.Names
	name  string
}

// call is one call of the target, found in pkg.
type call struct {
	pkg  *packages.Package
	expr *ast.CallExpr
}

// findTarget resolves the function at the given position and checks that
// its signature can change: a function or unexported method declared in
// the workspace, with a body, only ever called. (Each refactoring then
// checks interfaces against the new signature.)
func findTarget(ws *workspace.Workspace, q locate.IdentQuery) (*target, error) {
	fn, err := locateFunc(ws, q)
	if err != nil {
		return nil, err
	}
	if recv := fn.Signature().Recv(); recv != nil && types.IsInterface(recv.Type()) {
		return nil, refactor.Refusef("%s is an interface method; this refactoring cannot change interface methods (add_parameter, remove_parameter, move_parameter and change_parameter_type change them with every implementation)", fn.Name())
	}
	return newTarget(ws, fn)
}

// findFamily is findTarget for refactorings that change an interface
// method with every implementation: the target named, with members set
// when other methods change with it.
func findFamily(ws *workspace.Workspace, q locate.IdentQuery) (*target, error) {
	fn, err := locateFunc(ws, q)
	if err != nil {
		return nil, err
	}
	fam := familyOf(ws, fn)
	recv := fn.Signature().Recv()
	if len(fam) == 1 && (recv == nil || !types.IsInterface(recv.Type())) {
		return newTarget(ws, fn)
	}
	keys := map[string]bool{}
	for _, f := range fam {
		keys[keyOf(ws.Fset, f)] = true
	}
	var primary *target
	var members []*target
	for _, f := range fam {
		m, err := newTarget(ws, f)
		if err != nil {
			return nil, err
		}
		m.familyKeys = keys
		members = append(members, m)
		if m.key == keyOf(ws.Fset, fn) {
			primary = m
		}
	}
	primary.members = members
	return primary, nil
}

// locateFunc resolves the function or method at q, declared in the
// workspace, whose signature may change.
func locateFunc(ws *workspace.Workspace, q locate.IdentQuery) (*types.Func, error) {
	pkg, id, err := locate.Ident(ws, q)
	if err != nil {
		return nil, err
	}
	fn, ok := pkg.TypesInfo.ObjectOf(id).(*types.Func)
	if !ok {
		return nil, refactor.Refusef("%q at %s is not a function or method", id.Name, ws.Position(id.Pos()))
	}
	fn = fn.Origin()
	if fn.Pkg() == nil || !ws.Contains(ws.Fset.Position(fn.Pos()).Filename) {
		return nil, refactor.Refusef("%s is declared outside the workspace; only functions declared in this module can change", fn.Name())
	}
	if fn.Parent() == fn.Pkg().Scope() && (fn.Name() == "init" || (fn.Name() == "main" && fn.Pkg().Name() == "main")) {
		return nil, refactor.Refusef("the signature of %s is fixed by the language", fn.Name())
	}
	return fn, nil
}

// newTarget finds fn's declaration (for an interface method, its line in
// the interface), its parameters and every call of it.
func newTarget(ws *workspace.Workspace, fn *types.Func) (*target, error) {
	t := &target{Site: site.Site{WS: ws}, obj: fn, key: keyOf(ws.Fset, fn)}
	iface := false
	if recv := fn.Signature().Recv(); recv != nil && types.IsInterface(recv.Type()) {
		iface = true
	}
	var declFile *ast.File
	for _, p := range ws.Pkgs {
		if t.ftype != nil {
			break
		}
		for _, f := range p.Syntax {
			ast.Inspect(f, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.FuncDecl:
					if !iface && keyOf(ws.Fset, p.TypesInfo.Defs[n.Name]) == t.key {
						t.Pkg, t.decl, t.ftype, declFile = p, n, n.Type, f
					}
					return false
				case *ast.InterfaceType:
					for _, m := range n.Methods.List {
						ft, ok := m.Type.(*ast.FuncType)
						if iface && ok && len(m.Names) == 1 && keyOf(ws.Fset, p.TypesInfo.Defs[m.Names[0]]) == t.key {
							t.Pkg, t.ftype, declFile = p, ft, f
						}
					}
				}
				return t.ftype == nil
			})
		}
	}
	if t.ftype == nil || !iface && t.decl.Body == nil {
		return nil, refactor.Refusef("%s has no body in Go source (it may be implemented in assembly)", fn.Name())
	}
	var err error
	if t.Src, err = os.ReadFile(ws.Fset.Position(declFile.Pos()).Filename); err != nil {
		return nil, err
	}
	t.File, t.TF, t.Info = declFile, ws.Fset.File(declFile.Pos()), t.Pkg.TypesInfo
	for _, f := range t.ftype.Params.List {
		if len(f.Names) == 0 {
			if !iface {
				return nil, refactor.Refusef("the parameters of %s have no names; name them first", fn.Name())
			}
			t.params = append(t.params, param{field: f})
		}
		for i, n := range f.Names {
			t.params = append(t.params, param{field: f, index: i, name: n.Name})
		}
	}
	if err := t.collectCalls(); err != nil {
		return nil, err
	}
	return t, nil
}

// group is the methods that change with t: its members, or t alone.
func (t *target) group() []*target {
	if t.members != nil {
		return t.members
	}
	return []*target{t}
}

// named is a member of t's group whose parameters have names, to find a
// parameter by name in: t itself if it has them.
func (t *target) named() *target {
	for _, m := range append([]*target{t}, t.group()...) {
		if len(m.params) == 0 || m.params[0].name != "" {
			return m
		}
	}
	return t
}

// declPos is where t is declared: its func declaration, or its line in
// an interface.
func (t *target) declPos() token.Pos {
	if t.decl != nil {
		return t.decl.Pos()
	}
	return t.ftype.Pos()
}

func (t *target) declFile() string { return t.WS.Fset.Position(t.declPos()).Filename }

// groupName names the methods that change: "Area", or for a family
// "Shape.Area, Square.Area and Rect.Area".
func (t *target) groupName() string {
	if t.members == nil {
		return t.obj.Name()
	}
	var names []string
	for _, m := range t.members {
		names = append(names, t.typeName(receiverBase(m.obj.Signature().Recv().Type()))+"."+m.obj.Name())
	}
	if len(names) == 1 {
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// familyOf returns fn with every method that must change with it: the
// interface methods of the module's interfaces and the methods of the
// module's types that implement them, joined wherever a type implements
// an interface through the method.
func familyOf(ws *workspace.Workspace, fn *types.Func) []*types.Func {
	if fn.Signature().Recv() == nil {
		return []*types.Func{fn}
	}
	parent := map[string]string{}
	var find func(k string) string
	find = func(k string) string {
		if p, ok := parent[k]; ok && p != k {
			parent[k] = find(p)
			return parent[k]
		}
		return k
	}
	objs := map[string]*types.Func{}
	inModule := func(f *types.Func) bool { return ws.Contains(ws.Fset.Position(f.Pos()).Filename) }
	for _, p := range ws.Pkgs {
		if p.Types == nil {
			continue
		}
		scopes := []*types.Scope{p.Types.Scope()}
		for _, dep := range analysis.Dependencies(p) {
			if len(dep.GoFiles) > 0 && ws.Contains(dep.GoFiles[0]) {
				scopes = append(scopes, dep.Types.Scope())
			}
		}
		type ifaceMethod struct {
			m  *types.Func
			it *types.Interface
		}
		type method struct {
			v types.Type
			m *types.Func
		}
		var ims []ifaceMethod
		var ms []method
		for _, s := range scopes {
			for _, name := range s.Names() {
				tn, ok := s.Lookup(name).(*types.TypeName)
				if !ok || tn.IsAlias() {
					continue
				}
				n, ok := tn.Type().(*types.Named)
				if !ok || n.TypeParams().Len() > 0 {
					continue
				}
				if it, ok := n.Underlying().(*types.Interface); ok {
					for i := range it.NumMethods() {
						if m := it.Method(i); m.Id() == fn.Id() && inModule(m) {
							ims = append(ims, ifaceMethod{m, it})
						}
					}
					continue
				}
				for _, v := range []types.Type{n, types.NewPointer(n)} {
					if sel := types.NewMethodSet(v).Lookup(fn.Pkg(), fn.Name()); sel != nil {
						if m, ok := sel.Obj().(*types.Func); ok && inModule(m) {
							ms = append(ms, method{v, m.Origin()})
						}
					}
				}
			}
		}
		for _, im := range ims {
			k := keyOf(ws.Fset, im.m)
			objs[k] = im.m
			for _, m := range ms {
				if types.Implements(m.v, im.it) {
					mk := keyOf(ws.Fset, m.m)
					objs[mk] = m.m
					parent[find(mk)] = find(k)
				}
			}
		}
	}
	key := keyOf(ws.Fset, fn)
	var fam []*types.Func
	for k, o := range objs {
		if find(k) == find(key) {
			fam = append(fam, o)
		}
	}
	if len(fam) == 0 {
		return []*types.Func{fn}
	}
	sort.Slice(fam, func(i, j int) bool {
		a, b := ws.Fset.Position(fam[i].Pos()), ws.Fset.Position(fam[j].Pos())
		if a.Filename != b.Filename {
			return a.Filename < b.Filename
		}
		return a.Offset < b.Offset
	})
	return fam
}

// collectCalls finds every call of the target in every package variant,
// and refuses any other reference.
func (t *target) collectCalls() error {
	seen := map[string]bool{}
	for _, p := range t.WS.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		info := p.TypesInfo
		callFuns := map[*ast.Ident]*ast.CallExpr{}
		for _, f := range p.Syntax {
			var err error
			ast.Inspect(f, func(n ast.Node) bool {
				ce, ok := n.(*ast.CallExpr)
				if !ok || err != nil {
					return err == nil
				}
				fun := ast.Unparen(ce.Fun)
				switch x := fun.(type) {
				case *ast.IndexExpr:
					fun = x.X
				case *ast.IndexListExpr:
					fun = x.X
				}
				var id *ast.Ident
				switch x := fun.(type) {
				case *ast.Ident:
					id = x
				case *ast.SelectorExpr:
					id = x.Sel
					if sel := info.Selections[x]; sel != nil && sel.Kind() == types.MethodExpr && t.is(info.Uses[id]) {
						err = refactor.Refusef("%s is called as a method expression at %s, which is not supported yet", t.obj.Name(), t.WS.Position(id.Pos()))
					}
				}
				if id != nil {
					callFuns[id] = ce
				}
				return true
			})
			if err != nil {
				return err
			}
		}
		for id, o := range info.Uses {
			if !t.is(o) {
				continue
			}
			ce, isCall := callFuns[id]
			if !isCall {
				return refactor.Refusef("%s is used as a value at %s, not called; its type would change, so this is not supported",
					t.obj.Name(), t.WS.Position(id.Pos()))
			}
			pos := t.WS.Fset.Position(ce.Pos())
			// By range: in x.m().m() both calls start at the same offset.
			k := fmt.Sprintf("%s:%d-%d", pos.Filename, pos.Offset, t.Off(ce.End()))
			if seen[k] {
				continue // the same call, seen through another package variant
			}
			seen[k] = true
			t.calls = append(t.calls, call{pkg: p, expr: ce})
		}
	}
	sort.Slice(t.calls, func(i, j int) bool {
		a, b := t.WS.Fset.Position(t.calls[i].expr.Pos()), t.WS.Fset.Position(t.calls[j].expr.Pos())
		if a.Filename != b.Filename {
			return a.Filename < b.Filename
		}
		return a.Offset < b.Offset
	})
	return nil
}

// checkNoSpread refuses a call like f(g()), whose arguments are another
// call's results.
func checkNoSpread(c call, where string) error {
	if len(c.expr.Args) == 1 {
		if _, multi := c.pkg.TypesInfo.Types[c.expr.Args[0]].Type.(*types.Tuple); multi {
			return refactor.Refusef("the call at %s passes the results of another call as its arguments, so its arguments cannot change; assign them to variables first", where)
		}
	}
	return nil
}

// newPlan merges the edits into a plan whose summary is what, followed by
// how many calls and files changed.
func (t *target) newPlan(what string, fes []edit.FileEdit) *refactor.Plan {
	files := map[string]bool{}
	for _, fe := range fes {
		files[fe.Path] = true
	}
	calls := 0
	for _, m := range t.group() {
		calls += len(m.calls)
	}
	plan := &refactor.Plan{
		Summary: fmt.Sprintf("%s (%d call%s updated, %d file%s)",
			what, calls, textutil.Plural(calls), len(files), textutil.Plural(len(files))),
		Edits: edit.Merge(fes),
		// Every call changes the same way: the summary says how, the
		// result lists where.
		Locations: true,
	}
	if analysis.PublicAPI(t.obj, t.WS.Pkgs) {
		plan.Notes = append(plan.Notes, refactor.APINote(t.obj.Name(), "changing its signature"))
		if t.obj.Signature().Recv() != nil {
			plan.Notes = append(plan.Notes, "interfaces declared inside functions outside this module (such as the ones errors.Is and errors.As look for) cannot be checked")
		}
	}
	return plan
}

func (t *target) is(o types.Object) bool {
	return o != nil && keyOf(t.WS.Fset, o) == t.key
}

// keyOf identifies an object by its declaration position, which is what a
// package's variants (and a generic function's instantiations) share.
func keyOf(fset *token.FileSet, obj types.Object) string {
	if obj == nil {
		return ""
	}
	if f, ok := obj.(*types.Func); ok {
		obj = f.Origin()
	}
	p := fset.Position(obj.Pos())
	return fmt.Sprintf("%s:%d:%d:%s", p.Filename, p.Line, p.Column, obj.Name())
}

// unsafeValue returns what in e, an expression of fa, evaluated once more
// or once less where nothing uses its value, would change behavior (a
// side effect, or a panic), and why, or nil.
func unsafeValue(fa *analysis.Func, e ast.Expr, eff *analysis.Effects) (ast.Node, string) {
	m := analysis.Move{Effects: eff, Func: fa, Expr: e, Times: analysis.Unused}
	if o := m.Check(); o != nil {
		return o.In, o.Does
	}
	return nil, ""
}
