package m

import (
	"fmt"
	"go/ast"
)

func idents(f *ast.File) {
	var seen []string
	count := 0
	seen, count = names(f, seen, count)
	fmt.Println(seen, count)
}

func names(f *ast.File, seen []string, count int) ([]string, int) {
	ast.Inspect(f, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			seen = append(seen, id.Name)
			count++
		}
		return true
	})
	return seen, count
}
