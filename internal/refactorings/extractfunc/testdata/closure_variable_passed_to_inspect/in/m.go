package m

import "go/ast"

func count(root ast.Node) int {
	n := 0
	var visit func(ast.Node) bool
	visit = func(x ast.Node) bool {
		n++
		return true
	}
	ast.Inspect(root, visit)
	return n
}
