package analysis

import (
	"go/ast"
	"go/token"
	"go/types"
)

// OwnType is the type e has on its own, before any context converts it:
// untyped constants and comparisons stay untyped (go/types records the
// converted type for them). e must be in pkg, positioned in fset.
func OwnType(fset *token.FileSet, pkg *types.Package, info *types.Info, e ast.Expr) types.Type {
	tv := info.Types[e]
	b, basic := tv.Type.Underlying().(*types.Basic)
	if !basic || b.Info()&types.IsUntyped != 0 {
		return tv.Type // only basic types can come from untyped expressions
	}
	hasFuncLit := false
	ast.Inspect(e, func(n ast.Node) bool {
		_, ok := n.(*ast.FuncLit)
		hasFuncLit = hasFuncLit || ok
		return !hasFuncLit
	})
	if hasFuncLit {
		return tv.Type
	}
	u := ast.Unparen(e)
	own := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
	if err := types.CheckExpr(fset, pkg, u.Pos(), u, own); err != nil {
		return tv.Type
	}
	return own.Types[u].Type
}

// NeedsParens reports whether e, written in place of use (whose parent is
// parent), must be parenthesized to keep its grouping.
func NeedsParens(e ast.Expr, parent ast.Node, use ast.Expr) bool {
	switch e.(type) {
	case *ast.Ident, *ast.BasicLit, *ast.CompositeLit, *ast.CallExpr, *ast.SelectorExpr, *ast.IndexExpr,
		*ast.IndexListExpr, *ast.SliceExpr, *ast.TypeAssertExpr, *ast.ParenExpr, *ast.FuncLit:
		return false // primary expressions bind tightest
	}
	switch p := parent.(type) {
	case *ast.BinaryExpr:
		b, ok := e.(*ast.BinaryExpr)
		if !ok {
			return false // unary operators bind tighter than any binary one
		}
		pp, ip := p.Op.Precedence(), b.Op.Precedence()
		return ip < pp || (ip == pp && p.Y == use)
	case *ast.UnaryExpr, *ast.StarExpr:
		return true
	case *ast.SelectorExpr:
		return p.X == use
	case *ast.IndexExpr:
		return p.X == use
	case *ast.IndexListExpr:
		return p.X == use
	case *ast.SliceExpr:
		return p.X == use
	case *ast.TypeAssertExpr:
		return p.X == use
	case *ast.CallExpr:
		return p.Fun == use
	}
	return false
}

// InControlHeader reports whether e, a composite literal written at the
// node path[0] (path innermost first), would land unbracketed in an if, for
// or switch header, where Go requires parentheses around it.
func InControlHeader(e ast.Expr, path []ast.Node) bool {
	if _, ok := ast.Unparen(e).(*ast.CompositeLit); !ok {
		return false
	}
	for i := 1; i < len(path); i++ {
		switch path[i].(type) {
		case *ast.ParenExpr, *ast.CallExpr, *ast.IndexExpr, *ast.CompositeLit, *ast.FuncLit, *ast.BlockStmt:
			return false // bracketed: no ambiguity
		case *ast.IfStmt, *ast.ForStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.RangeStmt:
			return true
		}
	}
	return false
}
