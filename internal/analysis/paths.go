package analysis

import (
	"go/ast"
	"go/types"
	"strings"
)

// PathOf returns the part of a local variable that the assigned
// expression e is: its fields (.f.g), or "" for all of it.
func PathOf(info *types.Info, e ast.Expr) string {
	switch e := ast.Unparen(e).(type) {
	case *ast.SelectorExpr:
		if IsFieldOfValue(info, e) {
			return PathOf(info, e.X) + "." + e.Sel.Name
		}
	case *ast.IndexExpr:
		return PathOf(info, e.X)
	}
	return ""
}

// IsFieldOfValue reports whether sel selects a field of a struct value,
// not through a pointer.
func IsFieldOfValue(info *types.Info, sel *ast.SelectorExpr) bool {
	s := info.Selections[sel]
	return s != nil && s.Kind() == types.FieldVal && !s.Indirect() && !IsPointer(info.TypeOf(sel.X))
}

// PathAbove returns the part of its variable that id is used for: the
// fields selected from it (.f.g), or "" for the whole variable.
func PathAbove(info *types.Info, id *ast.Ident, parents map[ast.Node]ast.Node) string {
	path := ""
	var n ast.Node = id
	for {
		p := parents[n]
		if paren, ok := p.(*ast.ParenExpr); ok {
			n = paren
			continue
		}
		sel, ok := p.(*ast.SelectorExpr)
		if !ok || sel.X != n || !IsFieldOfValue(info, sel) {
			return path
		}
		path += "." + sel.Sel.Name
		n = sel
	}
}

// ParentsIn maps each node inside root to its parent.
func ParentsIn(root ast.Node) map[ast.Node]ast.Node {
	parents := map[ast.Node]ast.Node{}
	var stack []ast.Node
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if len(stack) > 0 {
			parents[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})
	return parents
}

// Overlaps reports whether two parts of a variable share memory: one is
// the other or contains it.
func Overlaps(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+".") || strings.HasPrefix(b, a+".")
}
