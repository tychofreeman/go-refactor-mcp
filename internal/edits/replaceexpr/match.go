package replaceexpr

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
)

// sameMeaning returns the expressions of file that mean what expr does,
// in source order: the same tokens, with every package-level name, import,
// field and method referring to the same object, and every local variable
// of the same type (so c.Body matches c.Body in another method of the same
// type, but not d.Body of another type).
func sameMeaning(fset *token.FileSet, info *types.Info, file *ast.File, src []byte, expr ast.Expr) []ast.Expr {
	tf := fset.File(file.Pos())
	want, err := locate.Tokens(src[tf.Offset(expr.Pos()):tf.Offset(expr.End())])
	if err != nil {
		return []ast.Expr{expr}
	}
	names := idents(expr)
	var out []ast.Expr
	ast.Inspect(file, func(n ast.Node) bool {
		e, ok := n.(ast.Expr)
		if !ok || !locate.TokensMatch(tf, src, e, want) {
			return true
		}
		if e != expr && !sameNames(fset, info, names, idents(e)) {
			return true
		}
		out = append(out, e)
		return false // an occurrence inside a match is part of it
	})
	return out
}

// idents lists every name in e, selected ones included, in order.
func idents(e ast.Expr) []*ast.Ident {
	var out []*ast.Ident
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			out = append(out, id)
		}
		return true
	})
	return out
}

// sameNames reports whether two lists of names (of expressions with the
// same tokens) mean the same.
func sameNames(fset *token.FileSet, info *types.Info, a, b []*ast.Ident) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		oa, ob := info.ObjectOf(a[i]), info.ObjectOf(b[i])
		switch {
		case oa == nil || ob == nil:
			if oa != ob {
				return false
			}
		case isLocal(oa) && isLocal(ob):
			if !types.Identical(oa.Type(), ob.Type()) {
				return false
			}
		case analysis.ObjectKey(fset, oa) != analysis.ObjectKey(fset, ob):
			return false
		}
	}
	return true
}

// isLocal reports whether o is a variable declared in a function.
func isLocal(o types.Object) bool {
	v, ok := o.(*types.Var)
	return ok && !v.IsField() && v.Pkg() != nil && v.Parent() != v.Pkg().Scope()
}
