package analysis

import (
	"go/ast"
	"go/types"
)

// swapsOnly holds the functions that take a slice as an interface and
// whose reflection only measures it and swaps its elements, never
// looking at what they hold.
var swapsOnly = map[string]bool{
	"sort.Slice":         true,
	"sort.SliceStable":   true,
	"sort.SliceIsSorted": true,
	"reflect.Swapper":    true,
}

// PassedAsInterface returns the first argument in files that is passed to
// an interface parameter, or converted to an interface type, and whose
// type holds a type match accepts, or nil. Reflection there (encoding/json,
// text/template, fmt's %+v) sees what the value holds: its field names
// among others. Arguments of sort.Slice and the like, which only swap
// elements, and assignments to interface variables and returns are not
// looked at.
func PassedAsInterface(info *types.Info, files []*ast.File, match func(types.Type) bool) ast.Expr {
	var found ast.Expr
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || found != nil {
				return found == nil
			}
			if fn := staticCallee(info, call); fn != nil && swapsOnly[fn.FullName()] {
				return true
			}
			tv := info.Types[call.Fun]
			var param func(i int) types.Type
			switch {
			case tv.IsType():
				param = func(int) types.Type { return tv.Type }
			case tv.IsBuiltin():
				return true
			default:
				sig, ok := tv.Type.Underlying().(*types.Signature)
				if !ok {
					return true
				}
				param = func(i int) types.Type {
					ps := sig.Params()
					if sig.Variadic() && i >= ps.Len()-1 {
						last := ps.At(ps.Len() - 1).Type()
						if call.Ellipsis.IsValid() {
							return last
						}
						return last.(*types.Slice).Elem()
					}
					if i < ps.Len() {
						return ps.At(i).Type()
					}
					return nil
				}
			}
			for i, arg := range call.Args {
				if pt := param(i); pt != nil && types.IsInterface(pt) && Holds(info.TypeOf(arg), match) {
					found = arg
					return false
				}
			}
			return true
		})
		if found != nil {
			break
		}
	}
	return found
}

// Holds reports whether a value of type t holds one of a type match
// accepts: t itself, or what it points to, its elements, keys and fields,
// as reflection would walk them. Interfaces, functions and channels are not
// looked into.
func Holds(t types.Type, match func(types.Type) bool) bool {
	seen := map[types.Type]bool{}
	var holds func(t types.Type) bool
	holds = func(t types.Type) bool {
		if t == nil || seen[t] {
			return false
		}
		seen[t] = true
		if match(t) {
			return true
		}
		switch u := t.(type) {
		case *types.Named:
			return holds(u.Underlying())
		case *types.Alias:
			return holds(types.Unalias(u))
		case *types.Pointer:
			return holds(u.Elem())
		case *types.Slice:
			return holds(u.Elem())
		case *types.Array:
			return holds(u.Elem())
		case *types.Map:
			return holds(u.Key()) || holds(u.Elem())
		case *types.Struct:
			for i := 0; i < u.NumFields(); i++ {
				if holds(u.Field(i).Type()) {
					return true
				}
			}
		}
		return false
	}
	return holds(t)
}
