package m

import "go/ast"

func hasIdent(n ast.Node, name string) bool {
	found := false
	ast.Inspect(n, func(c ast.Node) bool {
		if id, ok := c.(*ast.Ident); ok && id.Name == name {
			found = true
		}
		return !found
	})
	return found
}
