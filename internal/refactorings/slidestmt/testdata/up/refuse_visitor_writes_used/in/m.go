package m

import "go/ast"

func F(n ast.Node) int {
	count := 0
	ast.Inspect(n, func(ast.Node) bool {
		count++
		return true
	})
	c := count
	return c
}
