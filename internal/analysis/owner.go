package analysis

import (
	"go/ast"
	"go/types"
)

// Owner says who can reach a piece of memory: read it, or change it.
type Owner int

const (
	Anyone   Owner = iota // code anywhere: a package variable, what a parameter points to
	ThisFunc              // this function's own code and the pointers and closures it made: a local whose address is taken or that a closure may run later, or memory a fresh local points to
	ThisCode              // the code at hand alone: a local nothing else reaches
)

// Owns says who can reach the variable v: a local nothing else reaches
// is ThisCode; one whose address is taken, or that a closure not known
// to run only during its statement captures, is ThisFunc; anything else
// is Anyone.
func (f *Func) Owns(v *types.Var) Owner {
	switch {
	case v == nil || !f.IsLocal(v):
		return Anyone
	case f.Addressed[v] || f.CapturedAsync[v]:
		return ThisFunc
	}
	return ThisCode
}

// Owner says who can reach the memory the assignable expression x names:
// a local variable or part of one (Owns), or, through a pointer, slice
// or map, what it points to (Pointee).
func (f *Func) Owner(x ast.Expr) Owner {
	if v := f.RootVar(x); v != nil {
		return f.Owns(v)
	}
	switch x := ast.Unparen(x).(type) {
	case *ast.IndexExpr:
		return f.Pointee(x.X)
	case *ast.SelectorExpr:
		if IsPointer(f.Info.TypeOf(x.X)) {
			return f.Pointee(x.X)
		}
	case *ast.StarExpr:
		return f.Pointee(x.X)
	}
	return Anyone
}

// Pointee says who can reach the memory that x, a pointer, slice or map,
// points to: ThisFunc when x is a local that only ever holds memory the
// function made itself (see freshLocals), else Anyone.
func (f *Func) Pointee(x ast.Expr) Owner {
	id, ok := ast.Unparen(x).(*ast.Ident)
	if !ok {
		return Anyone
	}
	v, ok := f.Info.Uses[id].(*types.Var)
	if !ok || !f.fresh()[v] {
		return Anyone
	}
	return ThisFunc
}

// fresh returns the function's fresh locals, computed on first use.
func (f *Func) fresh() map[*types.Var]bool {
	if f.freshVars == nil {
		f.freshVars = map[*types.Var]bool{}
		switch n := f.Node.(type) {
		case *ast.FuncDecl:
			if n.Body != nil {
				f.freshVars = freshLocals(f, n.Body)
			}
		case *ast.FuncLit:
			f.freshVars = freshLocals(f, n.Body)
		}
	}
	return f.freshVars
}
