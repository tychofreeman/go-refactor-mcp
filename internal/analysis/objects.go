package analysis

import (
	"fmt"
	"go/token"
	"go/types"
)

// ObjectKey identifies an object by its declaration position, which a
// package's variants share.
func ObjectKey(fset *token.FileSet, o types.Object) string {
	p := fset.Position(o.Pos())
	return fmt.Sprintf("%s:%d:%s", p.Filename, p.Offset, o.Name())
}

// KindOf names the kind of object o is, for a message.
func KindOf(o types.Object) string {
	switch o := o.(type) {
	case *types.Var:
		if o.Pkg() != nil && o.Parent() == o.Pkg().Scope() {
			return "package-level variable"
		}
		return "variable"
	case *types.Const:
		return "constant"
	case *types.TypeName:
		return "type"
	case *types.Func:
		return "function"
	case *types.PkgName:
		return "import"
	case *types.Builtin:
		return "builtin"
	case *types.Nil:
		return "predeclared nil"
	case *types.Label:
		return "label"
	}
	return "symbol"
}
