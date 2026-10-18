package m

import (
	"fmt"
	"go/ast"
)

func count(f *ast.File) int {
	n := 0
	ast.Inspect(f, func(node ast.Node) bool {
		if _, ok := node.(*ast.Ident); ok {
			n++
		}
		return true
	})
	fmt.Println(n)
	return n
}
