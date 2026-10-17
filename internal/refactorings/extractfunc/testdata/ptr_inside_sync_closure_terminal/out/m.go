package m

import "go/ast"

func hasIdent(n ast.Node, name string) bool {
	found := false
	ast.Inspect(n, func(c ast.Node) bool {
		return visit(c, name, &found)
	})
	return found
}

func visit(c ast.Node, name string, found *bool) bool {
	if id, ok := c.(*ast.Ident); ok && id.Name == name {
		*found = true
	}
	return !*found
}
