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

	"golang.org/x/tools/go/packages"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
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
	effs   *analysis.Effects // what calls do, built on first use by effects
	fixes  []edit.FileEdit   // further edits the change needs, beyond the signature and the calls
}

// effects summarizes what calls do: the module's own functions from their
// bodies, and known ones of the standard library (fmt.Sprintf of values
// without methods, for one), so that a call without side effects is not
// refused as one.
func (t *target) effects() *analysis.Effects {
	if t.effs == nil {
		t.effs = analysis.NewEffects()
		for _, p := range t.ws.Pkgs {
			t.effs.AddPackage(p.Syntax, p.TypesInfo)
		}
	}
	return t.effs
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
	if err := t.collectCalls(); err != nil {
		return nil, err
	}
	return t, nil
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
			// By range: in x.m().m() both calls start at the same offset.
			k := fmt.Sprintf("%s:%d-%d", pos.Filename, pos.Offset, t.off(ce.End()))
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
	plan := &refactor.Plan{
		Summary: fmt.Sprintf("%s (%d call%s updated, %d file%s)",
			what, len(t.calls), textutil.Plural(len(t.calls)), len(files), textutil.Plural(len(files))),
		Edits: edit.Merge(fes),
		// Every call changes the same way: the summary says how, the
		// result lists where.
		Locations: true,
	}
	if t.obj.Exported() {
		plan.Notes = append(plan.Notes, fmt.Sprintf("%s is exported: calls in other modules are not updated", t.obj.Name()))
		if t.obj.Signature().Recv() != nil {
			plan.Notes = append(plan.Notes, "interfaces declared inside functions outside this module (such as the ones errors.Is and errors.As look for) cannot be checked")
		}
	}
	return plan
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

// unsafeValue returns the first part of e whose evaluation could have a
// side effect or panic, and what it could do.
func unsafeValue(info *types.Info, e ast.Expr, eff *analysis.Effects) (ast.Node, string) {
	if tv := info.Types[e]; tv.Value != nil {
		return nil, "" // a constant
	}
	var bad ast.Node
	var why string
	ast.Inspect(e, syncWalk(info, func(n ast.Node) bool {
		if bad != nil {
			return false
		}
		if l := assignsOutside(info, n, e); l != nil {
			bad, why = n, "has side effects: it assigns "+types.ExprString(l)
			return false
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
		case *ast.CallExpr:
			if known, panics := analysis.KnownCall(info, n); known {
				if panics != analysis.NoPanic {
					bad, why = n, "could panic: it calls "+types.ExprString(n.Fun)
				}
			} else if eff != nil {
				switch s := eff.Call(info, n); {
				case s.Effect != "":
					bad, why = n, "has side effects"
				case s.Unsafe != "":
					bad, why = n, "could panic: it "+s.Unsafe
				}
			} else if analysis.HasEffect(info, n) {
				bad, why = n, "has side effects"
			}
		default:
			if analysis.HasEffect(info, n) {
				bad, why = n, "has side effects"
			}
		}
		return bad == nil
	}))
	return bad, why
}

// syncWalk adapts an ast.Inspect visitor so that it walks into the
// function literals a call runs before it returns (analysis.SyncLits,
// such as the one strings.FieldsFunc calls), whose bodies are part of
// what the call does, and skips every other literal, since defining one
// runs nothing.
func syncWalk(info *types.Info, visit func(ast.Node) bool) func(ast.Node) bool {
	sync := map[*ast.FuncLit]bool{}
	return func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			if !sync[n] {
				return false
			}
		case *ast.CallExpr:
			for _, lit := range analysis.SyncLits(info, n) {
				sync[lit] = true
			}
		}
		return visit(n)
	}
}

// assignsOutside returns the first thing the assignment or ++/-- n writes
// that is not a variable declared inside e, or nil. Within an expression,
// only a function literal that runs during it can assign.
func assignsOutside(info *types.Info, n, e ast.Node) ast.Expr {
	var lhs []ast.Expr
	switch n := n.(type) {
	case *ast.AssignStmt:
		lhs = n.Lhs
	case *ast.IncDecStmt:
		lhs = []ast.Expr{n.X}
	}
	for _, l := range lhs {
		if id, ok := ast.Unparen(l).(*ast.Ident); ok {
			if o := info.ObjectOf(id); id.Name == "_" || o != nil && e.Pos() <= o.Pos() && o.Pos() < e.End() {
				continue
			}
		}
		return l
	}
	return nil
}
