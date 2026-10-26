package analysis

import (
	"cmp"
	"go/token"
	"go/types"
	"slices"
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

// Iface is an interface type of the module: its name, "" for a type
// literal, and where it is declared or written.
type Iface struct {
	Name string
	Type *types.Interface
	At   token.Pos
}

// InterfacesWithMethod returns every interface type, among those infos
// record, with a method called name of the package pkgPath, in the
// order they are declared or written.
func InterfacesWithMethod(infos []*types.Info, pkgPath, name string) []Iface {
	has := func(it *types.Interface) bool {
		for i := 0; i < it.NumMethods(); i++ {
			if m := it.Method(i); m.Name() == name && m.Pkg() != nil && m.Pkg().Path() == pkgPath {
				return true
			}
		}
		return false
	}
	seen := map[token.Pos]bool{}
	named := map[*types.Interface]bool{} // a named interface's type literal is not another
	var out []Iface
	for _, info := range infos {
		for id, o := range info.Defs {
			if tn, ok := o.(*types.TypeName); ok {
				if it, ok := tn.Type().Underlying().(*types.Interface); ok && has(it) {
					named[it] = true
					if !seen[id.Pos()] {
						seen[id.Pos()] = true
						out = append(out, Iface{tn.Name(), it, id.Pos()})
					}
				}
			}
		}
		for e, tv := range info.Types {
			if it, ok := tv.Type.(*types.Interface); ok && tv.IsType() && has(it) && !named[it] && !seen[e.Pos()] {
				seen[e.Pos()] = true
				out = append(out, Iface{"", it, e.Pos()})
			}
		}
	}
	slices.SortFunc(out, func(a, b Iface) int { return cmp.Compare(a.At, b.At) })
	return out
}
