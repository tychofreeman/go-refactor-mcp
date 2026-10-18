package m

import "go/ast"

func F(n ast.Node, xs []int) int {
	count := 0
	ast.Inspect(n, func(ast.Node) bool {
		count++
		return true
	})
	total := len(xs)
	return count + total
}
