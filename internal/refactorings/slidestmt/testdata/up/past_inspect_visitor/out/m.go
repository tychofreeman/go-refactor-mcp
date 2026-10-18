package m

import "go/ast"

func F(n ast.Node, xs []int) int {
	count := 0
	total := len(xs)
	ast.Inspect(n, func(ast.Node) bool {
		count++
		return true
	})
	return count + total
}
