package analysis

import (
	"go/ast"
	"go/types"
)

// during is a function of the module and one of its parameters.
type during struct {
	fn *types.Func
	i  int
}

// RunsOnlyDuring reports whether fn, a function of the module, calls the
// function its parameter i holds only before it returns: the body calls
// it (not with go), passes it to a function that does the same, or uses
// it in a function literal that runs only during the call; it never
// stores, returns or sends it, nor passes it anywhere else. A function
// not known, a method of an interface, a variadic parameter and one the
// body does not name do not qualify. While fn is being judged, a call back
// to it (recursion) counts as qualifying.
func (e *Effects) RunsOnlyDuring(fn *types.Func, i int) bool {
	src, ok := e.decls[fn]
	sig := fn.Signature()
	if !ok || i >= sig.Params().Len() || sig.Variadic() && i == sig.Params().Len()-1 {
		return false
	}
	if _, isFunc := sig.Params().At(i).Type().Underlying().(*types.Signature); !isFunc {
		return false
	}
	k := during{fn, i}
	if v, ok := e.during[k]; ok {
		return v
	}
	e.during[k] = true // assumed while judged
	v := false
	if p := paramObj(src, i); p != nil {
		v = onlyCalledDuring(src.info, src.decl.Body, p, e)
	}
	e.during[k] = v
	return v
}

// paramObj returns the object of the parameter i of src's function, or
// nil when it has no name.
func paramObj(src funcSource, i int) types.Object {
	n := 0
	for _, f := range src.decl.Type.Params.List {
		if len(f.Names) == 0 {
			if n == i {
				return nil
			}
			n++
			continue
		}
		for _, id := range f.Names {
			if n == i {
				return src.info.Defs[id]
			}
			n++
		}
	}
	return nil
}

// onlyCalledDuring reports whether every use in body of v, a variable
// holding a function, runs it only while body runs: a call (not with go),
// an argument to a function that calls it only before returning, the
// assignment of a function literal to it, or a use inside a function
// literal that itself runs only while body runs. While v is being judged,
// a use that leads back to it (a recursive visit function) qualifies.
func onlyCalledDuring(info *types.Info, body *ast.BlockStmt, v types.Object, e *Effects) bool {
	if e.judging[v] {
		return true
	}
	e.judging[v] = true
	defer delete(e.judging, v)
	ok := true
	var stack []ast.Node
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		stack = append(stack, n)
		id, isID := n.(*ast.Ident)
		if !ok || !isID || info.Uses[id] != v {
			return ok
		}
		if !usedDuring(info, body, stack, e) {
			ok = false
		}
		return ok
	})
	return ok
}

// usedDuring reports whether the use of a function at the top of stack,
// the nodes from body down to it, runs the function only while body runs.
func usedDuring(info *types.Info, body *ast.BlockStmt, stack []ast.Node, e *Effects) bool {
	use := stack[len(stack)-1].(ast.Expr)
	parent := parentOf(stack, len(stack)-1)
	switch p := parent.(type) {
	case *ast.AssignStmt:
		for _, l := range p.Lhs {
			if l == use {
				_, lit := p.Rhs[0].(*ast.FuncLit)
				return lit && len(p.Lhs) == 1 && len(p.Rhs) == 1
			}
		}
		return false
	case *ast.CallExpr:
		if ast.Unparen(p.Fun) == use {
			if _, isGo := parentOf(stack, indexOf(stack, p)).(*ast.GoStmt); isGo {
				return false
			}
		} else if !CallsBeforeReturning(info, p, use, e) {
			return false
		}
	default:
		return false
	}
	// A use inside a function literal runs when the literal does.
	for j := len(stack) - 2; j >= 0; j-- {
		lit, ok := stack[j].(*ast.FuncLit)
		if !ok {
			continue
		}
		if !litDuring(info, body, stack[:j+1], lit, e) {
			return false
		}
	}
	return true
}

// litDuring reports whether lit, at the top of stack, runs only while
// body runs: called at once (not with go), passed to a function that
// calls it only before returning, or assigned to a local variable whose
// every use runs it only while body runs.
func litDuring(info *types.Info, body *ast.BlockStmt, stack []ast.Node, lit *ast.FuncLit, e *Effects) bool {
	switch p := parentOf(stack, len(stack)-1).(type) {
	case *ast.CallExpr:
		if ast.Unparen(p.Fun) == lit {
			_, isGo := parentOf(stack, indexOf(stack, p)).(*ast.GoStmt)
			return !isGo
		}
		return CallsBeforeReturning(info, p, lit, e)
	case *ast.AssignStmt:
		if len(p.Lhs) != 1 || len(p.Rhs) != 1 {
			return false
		}
		id, ok := p.Lhs[0].(*ast.Ident)
		if !ok {
			return false
		}
		v := info.ObjectOf(id)
		if _, local := v.(*types.Var); !local || v.Parent() == nil || v.Parent() == v.Pkg().Scope() {
			return false
		}
		return onlyCalledDuring(info, body, v, e)
	}
	return false
}

// parentOf returns the node above stack[i], looking through parentheses,
// or nil.
func parentOf(stack []ast.Node, i int) ast.Node {
	for j := i - 1; j >= 0; j-- {
		if _, paren := stack[j].(*ast.ParenExpr); !paren {
			return stack[j]
		}
	}
	return nil
}

// indexOf returns where n is in stack, or -1.
func indexOf(stack []ast.Node, n ast.Node) int {
	for i, s := range stack {
		if s == n {
			return i
		}
	}
	return -1
}
