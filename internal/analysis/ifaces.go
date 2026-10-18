package analysis

import (
	"go/token"
	"go/types"
)

// InterfaceWithMethod finds an interface type, among those infos record,
// with a method called name of the package pkgPath: a named interface
// (iface is its type name) or an interface type literal (iface is nil).
// at is where it is declared or written.
func InterfaceWithMethod(infos []*types.Info, pkgPath, name string) (iface *types.TypeName, at token.Pos, found bool) {
	has := func(it *types.Interface) bool {
		for i := 0; i < it.NumMethods(); i++ {
			if m := it.Method(i); m.Name() == name && m.Pkg() != nil && m.Pkg().Path() == pkgPath {
				return true
			}
		}
		return false
	}
	for _, info := range infos {
		for id, o := range info.Defs {
			if tn, ok := o.(*types.TypeName); ok {
				if it, ok := tn.Type().Underlying().(*types.Interface); ok && has(it) {
					return tn, id.Pos(), true
				}
			}
		}
		for e, tv := range info.Types {
			if it, ok := tv.Type.(*types.Interface); ok && tv.IsType() && has(it) {
				return nil, e.Pos(), true
			}
		}
	}
	return nil, token.NoPos, false
}
