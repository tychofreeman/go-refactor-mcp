package gocode

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
)

// ParseDeclarations parses text as top-level declarations (functions,
// methods, types, variables, constants), without a package clause or
// imports: imports are added for the code by AddImports.
func ParseDeclarations(text string) (*Code, []ast.Decl, error) {
	f, err := parser.ParseFile(token.NewFileSet(), "", "package p\n"+text, parser.ParseComments)
	if err != nil {
		return nil, nil, refactor.Refusef("content is not a list of Go declarations: %v", err)
	}
	if len(f.Imports) > 0 {
		return nil, nil, refactor.Refusef("content has imports; leave them out, they are added for the code that needs them")
	}
	if len(f.Decls) == 0 {
		return nil, nil, refactor.Refusef("content has no declarations")
	}
	c := &Code{node: f}
	ast.Inspect(f, func(n ast.Node) bool {
		if b, ok := n.(*ast.BasicLit); ok && strings.HasPrefix(b.Value, "`") && strings.Contains(b.Value, "\n") {
			c.Raw = true
		}
		return true
	})
	return c, f.Decls, nil
}

// DocOf is d's doc comment, or nil.
func DocOf(d ast.Decl) *ast.CommentGroup {
	switch d := d.(type) {
	case *ast.FuncDecl:
		return d.Doc
	case *ast.GenDecl:
		return d.Doc
	}
	return nil
}
