package m

import (
	"fmt"
	"go/ast"
)

func idents(f *ast.File) {
	var seen []string
	count := 0
	ast.Inspect(f, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			seen = append(seen, id.Name)
			count++
		}
		return true
	})
	fmt.Println(seen, count)
}
