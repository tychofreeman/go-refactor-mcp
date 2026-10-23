package analysis

import (
	"fmt"
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

// BareComposites returns the composite literals of e that nothing in e
// brackets (parentheses, a call's arguments, an index, braces): those an
// if, for or switch header would take for the start of its block.
func BareComposites(e ast.Expr) []*ast.CompositeLit {
	var out []*ast.CompositeLit
	var walk func(ast.Expr)
	walk = func(e ast.Expr) {
		switch e := e.(type) {
		case *ast.CompositeLit:
			out = append(out, e)
		case *ast.BinaryExpr:
			walk(e.X)
			walk(e.Y)
		case *ast.UnaryExpr:
			walk(e.X)
		case *ast.StarExpr:
			walk(e.X)
		case *ast.SelectorExpr:
			walk(e.X)
		case *ast.CallExpr:
			walk(e.Fun)
		case *ast.IndexExpr:
			walk(e.X)
		case *ast.IndexListExpr:
			walk(e.X)
		case *ast.SliceExpr:
			walk(e.X)
		case *ast.TypeAssertExpr:
			walk(e.X)
		}
	}
	walk(e)
	return out
}

// InControlHeader reports whether e, written at the node path[0] (path
// innermost first), would put a composite literal unbracketed in an if,
// for or switch header, where Go requires parentheses around it.
func InControlHeader(e ast.Expr, path []ast.Node) bool {
	if len(BareComposites(ast.Unparen(e))) == 0 {
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

// UsedAsVariable reports how the expression at path[0] is used as a
// variable (written to, addressed, or used as a pointer receiver), or "".
// Writes through fields and array elements count, since they modify the
// containing value.
func UsedAsVariable(info *types.Info, path []ast.Node) string {
	cur := path[0]
	for i := 1; i < len(path); i++ {
		switch p := path[i].(type) {
		case *ast.ParenExpr:
		case *ast.SelectorExpr:
			if p.X != cur {
				return ""
			}
			sel := info.Selections[p]
			if sel == nil {
				return ""
			}
			switch sel.Kind() {
			case types.FieldVal:
				if IsPointer(info.TypeOf(p.X)) {
					return "" // writes go through the pointer, which a copy shares
				}
			case types.MethodVal:
				if PtrRecv(sel) && !IsPointer(info.TypeOf(p.X)) && !types.IsInterface(info.TypeOf(p.X)) {
					return fmt.Sprintf("has its method %s called, which takes a pointer to it", sel.Obj().Name())
				}
				return ""
			default:
				return ""
			}
		case *ast.IndexExpr:
			if p.X != cur || !IsArray(info.TypeOf(p.X)) {
				return ""
			}
		case *ast.SliceExpr:
			if p.X == cur && IsArray(info.TypeOf(p.X)) {
				return "is sliced, which refers to it rather than a copy"
			}
			return ""
		case *ast.UnaryExpr:
			if p.Op == token.AND {
				return "has its address taken"
			}
			return ""
		case *ast.AssignStmt:
			for _, l := range p.Lhs {
				if l == cur {
					return "is assigned to"
				}
			}
			return ""
		case *ast.IncDecStmt:
			return "is incremented or decremented"
		case *ast.RangeStmt:
			if p.Key == cur || p.Value == cur {
				return "is assigned to by the range loop"
			}
			return ""
		default:
			return ""
		}
		cur = path[i]
	}
	return ""
}
