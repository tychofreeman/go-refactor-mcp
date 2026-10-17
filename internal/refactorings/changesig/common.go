// Package changesig implements refactorings that change a function's
// signature and update every call to match: add_parameter, so far.
//
// They share one requirement: every reference to the function must be a
// call. A function value (`f := fn`, passing fn as an argument) has the
// function's type, which the change would alter, and a method that
// implements an interface would stop implementing it, possibly silently.
package changesig

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"sort"

	"golang.org/x/tools/go/packages"

	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// target is the function whose signature changes.
type target struct {
	ws   *workspace.Workspace
	obj  *types.Func
	key  string
	pkg  *packages.Package // a variant of the declaring package
	decl *ast.FuncDecl     // the declaration in pkg
	src  []byte            // the declaring file's contents
	// params flattens the parameter list: one entry per name.
	params []param
	calls  []call
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
// the workspace, with a body, only ever called.
func findTarget(ws *workspace.Workspace, q locate.IdentQuery) (*target, error) {
	pkg, id, err := locate.Ident(ws, q)
	if err != nil {
		return nil, err
	}
	fn, ok := pkg.TypesInfo.ObjectOf(id).(*types.Func)
	if !ok {
		return nil, refactor.Refusef("%q at %s is not a function or method", id.Name, ws.Position(id.Pos()))
	}
	fn = fn.Origin()
	declFile := ws.Fset.Position(fn.Pos()).Filename
	if fn.Pkg() == nil || !ws.Contains(declFile) {
		return nil, refactor.Refusef("%s is declared outside the workspace; only functions declared in this module can change", fn.Name())
	}
	if recv := fn.Signature().Recv(); recv != nil {
		if types.IsInterface(recv.Type()) {
			return nil, refactor.Refusef("%s is an interface method; changing interface methods (and every implementation) is not supported yet", fn.Name())
		}
		if fn.Exported() {
			return nil, refactor.Refusef("%s is an exported method; exported methods are not supported yet: its type could stop implementing an interface, possibly in another package, without a compile error", fn.Name())
		}
	}
	if fn.Parent() == fn.Pkg().Scope() && (fn.Name() == "init" || (fn.Name() == "main" && fn.Pkg().Name() == "main")) {
		return nil, refactor.Refusef("the signature of %s is fixed by the language", fn.Name())
	}

	t := &target{ws: ws, obj: fn, key: keyOf(ws.Fset, fn)}
	for _, p := range ws.Pkgs {
		if t.decl != nil {
			break
		}
		for _, f := range p.Syntax {
			for _, d := range f.Decls {
				if fd, ok := d.(*ast.FuncDecl); ok && keyOf(ws.Fset, p.TypesInfo.Defs[fd.Name]) == t.key {
					t.pkg, t.decl = p, fd
				}
			}
		}
	}
	if t.decl == nil || t.decl.Body == nil {
		return nil, refactor.Refusef("%s has no body in Go source (it may be implemented in assembly)", fn.Name())
	}
	if t.src, err = os.ReadFile(declFile); err != nil {
		return nil, err
	}
	for _, f := range t.decl.Type.Params.List {
		if len(f.Names) == 0 {
			return nil, refactor.Refusef("the parameters of %s have no names; name them first", fn.Name())
		}
		for i, n := range f.Names {
			t.params = append(t.params, param{field: f, index: i, name: n.Name})
		}
	}
	if fn.Signature().Recv() != nil {
		if err := t.checkNoInterface(); err != nil {
			return nil, err
		}
	}
	if err := t.collectCalls(); err != nil {
		return nil, err
	}
	return t, nil
}

// checkNoInterface refuses an unexported method whose name some interface
// in its package also has: the type might implement it. (Only interfaces
// in the same package can have unexported methods.)
func (t *target) checkNoInterface() error {
	for _, p := range t.ws.Pkgs {
		if p.Types == nil || p.Types.Path() != t.obj.Pkg().Path() {
			continue
		}
		for _, tv := range p.TypesInfo.Types {
			it, ok := tv.Type.Underlying().(*types.Interface)
			if !ok {
				continue
			}
			for i := 0; i < it.NumMethods(); i++ {
				if m := it.Method(i); m.Name() == t.obj.Name() {
					return refactor.Refusef("an interface in package %s has a method %s (at %s), which the type of %s may implement; changing such methods is not supported yet",
						p.Types.Name(), m.Name(), t.ws.Position(m.Pos()), t.obj.FullName())
				}
			}
		}
	}
	return nil
}

// collectCalls finds every call of the target in every package variant,
// and refuses any other reference.
func (t *target) collectCalls() error {
	seen := map[string]bool{}
	for _, p := range t.ws.Pkgs {
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
						err = refactor.Refusef("%s is called as a method expression at %s, which is not supported yet", t.obj.Name(), t.ws.Position(id.Pos()))
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
					t.obj.Name(), t.ws.Position(id.Pos()))
			}
			pos := t.ws.Fset.Position(ce.Pos())
			k := fmt.Sprintf("%s:%d", pos.Filename, pos.Offset)
			if seen[k] {
				continue // the same call, seen through another package variant
			}
			seen[k] = true
			t.calls = append(t.calls, call{pkg: p, expr: ce})
		}
	}
	sort.Slice(t.calls, func(i, j int) bool {
		a, b := t.ws.Fset.Position(t.calls[i].expr.Pos()), t.ws.Fset.Position(t.calls[j].expr.Pos())
		if a.Filename != b.Filename {
			return a.Filename < b.Filename
		}
		return a.Offset < b.Offset
	})
	return nil
}

func (t *target) is(o types.Object) bool {
	return o != nil && keyOf(t.ws.Fset, o) == t.key
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

func (t *target) off(p token.Pos) int { return t.ws.Fset.Position(p).Offset }

func (t *target) text(n ast.Node) string { return string(t.src[t.off(n.Pos()):t.off(n.End())]) }

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
