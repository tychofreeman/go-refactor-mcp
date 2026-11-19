package analysis

import "go/types"

// UsesTypeParam reports whether t mentions one of the type parameters in
// set, or any type parameter when set is nil; seen holds the types
// already visited.
func UsesTypeParam(t types.Type, set map[*types.TypeName]bool, seen map[types.Type]bool) bool {
	if t == nil || seen[t] {
		return false
	}
	seen[t] = true
	any := func(ts ...types.Type) bool {
		for _, e := range ts {
			if UsesTypeParam(e, set, seen) {
				return true
			}
		}
		return false
	}
	switch t := t.(type) {
	case *types.TypeParam:
		return set == nil || set[t.Obj()]
	case *types.Named:
		for i := 0; i < t.TypeArgs().Len(); i++ {
			if any(t.TypeArgs().At(i)) {
				return true
			}
		}
		// A local type's underlying type can mention them too.
		if o := t.Obj(); o.Pkg() != nil && o.Parent() != o.Pkg().Scope() {
			return any(t.Underlying())
		}
	case *types.Alias:
		return any(types.Unalias(t))
	case *types.Pointer:
		return any(t.Elem())
	case *types.Slice:
		return any(t.Elem())
	case *types.Array:
		return any(t.Elem())
	case *types.Chan:
		return any(t.Elem())
	case *types.Map:
		return any(t.Key(), t.Elem())
	case *types.Signature:
		return any(t.Params(), t.Results())
	case *types.Tuple:
		for i := 0; i < t.Len(); i++ {
			if any(t.At(i).Type()) {
				return true
			}
		}
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if any(t.Field(i).Type()) {
				return true
			}
		}
	case *types.Interface:
		for i := 0; i < t.NumMethods(); i++ {
			if any(t.Method(i).Type()) {
				return true
			}
		}
		for i := 0; i < t.NumEmbeddeds(); i++ {
			if any(t.EmbeddedType(i)) {
				return true
			}
		}
	}
	return false
}

// HasTypeParam reports whether t is or mentions a type parameter.
func HasTypeParam(t types.Type) bool {
	return UsesTypeParam(t, nil, map[types.Type]bool{})
}
