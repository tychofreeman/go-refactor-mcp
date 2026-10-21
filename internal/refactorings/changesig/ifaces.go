package changesig

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
)

// paramChange maps a method's parameter types, written by typeKey (a
// variadic one as "...elem"), to the ones it has after the change.
type paramChange func(params []string) []string

// iface is an interface type some code in the program can see, and where
// it is declared or written (invalid for the predeclared error).
type iface struct {
	typ   types.Type
	under *types.Interface
	pos   token.Pos
}

// holder is a type whose method set includes the target method.
type holder struct {
	typ types.Type
	sig *types.Signature // the target method, as this type has it
}

// checkInterfaces refuses a change to a method's signature that would make
// some type stop implementing an interface it implements now, or start
// implementing one it does not: a type assertion or interface conversion
// could then silently behave differently. It considers every type whose
// method set has the method (including through embedding) and every
// interface with a method of that name that the program can see: those
// written in the workspace, those declared at package level in any
// dependency, and error.
func (t *target) checkInterfaces(change paramChange) error {
	return t.checkSignatures(func(sig *types.Signature) string { return sigKey(change(paramKeys(sig)), sig) })
}

// checkSignatures is checkInterfaces for a change of the method's
// signature that after writes, as sigKey does, from the signature now.
func (t *target) checkSignatures(afterOf func(*types.Signature) string) error {
	recv := t.obj.Signature().Recv()
	if recv == nil {
		return nil
	}
	for _, p := range t.ws.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		ifaces, named := t.visible(p)
		if len(ifaces) == 0 {
			continue
		}
		if base := receiverBase(recv.Type()); base != nil && base.TypeParams().Len() > 0 {
			return refactor.Refusef("the receiver type %s is generic, and the interface %s%s has a method %s; changing methods of generic types that an interface could match is not supported yet",
				base.Obj().Name(), t.typeName(ifaces[0].typ), t.where(ifaces[0]), t.obj.Name())
		}
		for _, h := range t.holders(named) {
			after := afterOf(h.sig)
			for _, i := range ifaces {
				if t.inFamily(i.under) {
					continue // it changes with the target
				}
				now, then := t.implements(h.typ, i.under, ""), t.implements(h.typ, i.under, after)
				switch {
				case now && !then:
					return refactor.Refusef("%s implements %s%s; with the new signature of %s it would not, which could silently change what a type assertion or interface conversion does",
						t.typeName(h.typ), t.typeName(i.typ), t.where(i), t.obj.Name())
				case then && !now:
					return refactor.Refusef("with the new signature of %s, %s would implement %s%s, which it does not now; that could silently change what a type assertion does",
						t.obj.Name(), t.typeName(h.typ), t.typeName(i.typ), t.where(i))
				}
			}
		}
	}
	return nil
}

// inFamily reports whether it's method of the target's name is a member
// of the target's family, and so changes with it.
func (t *target) inFamily(it *types.Interface) bool {
	for i := range it.NumMethods() {
		if m := it.Method(i); m.Id() == t.obj.Id() && t.familyKeys[keyOf(t.ws.Fset, m)] {
			return true
		}
	}
	return false
}

// visible returns, from package variant p's point of view, the interfaces
// that have a method with the target's name (named ones first), and every
// named type.
func (t *target) visible(p *packages.Package) ([]iface, []*types.TypeName) {
	var ifaces []iface
	var named []*types.TypeName
	seen := map[string]int{} // index in ifaces, by typeKey
	addIface := func(typ types.Type, pos token.Pos) {
		it, ok := typ.Underlying().(*types.Interface)
		if !ok || !it.IsMethodSet() || !t.hasMethod(it) {
			return
		}
		k := typeKey(it) // a named interface and its literal are one interface
		i, ok := seen[k]
		switch {
		case !ok:
			seen[k] = len(ifaces)
			ifaces = append(ifaces, iface{typ: typ, under: it, pos: pos})
		case t.inFamily(ifaces[i].under) && !t.inFamily(it):
			// One the change does not reach (fmt.Stringer for a family's
			// own Namer), which must still be implemented.
			ifaces[i] = iface{typ: typ, under: it, pos: pos}
		}
	}
	addName := func(tn *types.TypeName) {
		if tn == nil || tn.IsAlias() {
			return
		}
		named = append(named, tn)
		addIface(tn.Type(), tn.Pos())
	}

	addIface(types.Universe.Lookup("error").Type(), token.NoPos)
	var defs []*types.TypeName
	for _, o := range p.TypesInfo.Defs {
		if tn, ok := o.(*types.TypeName); ok {
			defs = append(defs, tn)
		}
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Pos() < defs[j].Pos() })
	for _, tn := range defs {
		addName(tn)
	}
	for _, dep := range analysis.Dependencies(p) {
		scope := dep.Types.Scope()
		for _, n := range scope.Names() {
			if tn, ok := scope.Lookup(n).(*types.TypeName); ok {
				addName(tn)
			}
		}
	}
	// Then interface types written without a name, as in
	// v.(interface{ Size() int }).
	var exprs []ast.Expr
	for e, tv := range p.TypesInfo.Types {
		if tv.IsType() {
			exprs = append(exprs, e)
		}
	}
	sort.Slice(exprs, func(i, j int) bool { return exprs[i].Pos() < exprs[j].Pos() })
	for _, e := range exprs {
		addIface(p.TypesInfo.Types[e].Type, e.Pos())
	}
	return ifaces, named
}

// holders returns the named types (and pointers to them) whose method set
// includes the target method.
func (t *target) holders(named []*types.TypeName) []holder {
	var hs []holder
	for _, tn := range named {
		n, ok := tn.Type().(*types.Named)
		if !ok || n.TypeParams().Len() > 0 {
			continue
		}
		vs := []types.Type{n}
		switch n.Underlying().(type) {
		case *types.Interface, *types.Pointer:
		default:
			vs = append(vs, types.NewPointer(n))
		}
		for _, v := range vs {
			sel := types.NewMethodSet(v).Lookup(t.obj.Pkg(), t.obj.Name())
			if sel != nil && t.is(sel.Obj()) {
				hs = append(hs, holder{typ: v, sig: sel.Obj().Type().(*types.Signature)})
			}
		}
	}
	return hs
}

// implements reports whether v implements it, using after as the target
// method's signature when it is not "".
func (t *target) implements(v types.Type, it *types.Interface, after string) bool {
	ms := types.NewMethodSet(v)
	for i := 0; i < it.NumMethods(); i++ {
		m := it.Method(i)
		want := sigKey(paramKeys(m.Signature()), m.Signature())
		if after != "" && m.Id() == t.obj.Id() {
			if after != want {
				return false
			}
			continue
		}
		sel := ms.Lookup(m.Pkg(), m.Name())
		if sel == nil {
			return false
		}
		sig := sel.Obj().Type().(*types.Signature)
		if sigKey(paramKeys(sig), sig) != want {
			return false
		}
	}
	return true
}

func (t *target) hasMethod(it *types.Interface) bool {
	for i := 0; i < it.NumMethods(); i++ {
		if it.Method(i).Id() == t.obj.Id() {
			return true
		}
	}
	return false
}

// paramKeys writes a signature's parameter types, comparable across a
// package's variants.
func paramKeys(sig *types.Signature) []string {
	var keys []string
	for i := 0; i < sig.Params().Len(); i++ {
		typ := sig.Params().At(i).Type()
		if sig.Variadic() && i == sig.Params().Len()-1 {
			keys = append(keys, "..."+typeKey(typ.(*types.Slice).Elem()))
			continue
		}
		keys = append(keys, typeKey(typ))
	}
	return keys
}

// sigKey writes a signature with the given parameter types and sig's
// results.
func sigKey(params []string, sig *types.Signature) string {
	var results []string
	for i := 0; i < sig.Results().Len(); i++ {
		results = append(results, typeKey(sig.Results().At(i).Type()))
	}
	return "(" + strings.Join(params, ", ") + ") (" + strings.Join(results, ", ") + ")"
}

func typeKey(typ types.Type) string { return types.TypeString(typ, pathQualifier) }

func pathQualifier(p *types.Package) string { return p.Path() }

// typeName writes a type for a message: by package name, and unqualified
// in the target's own package.
func (t *target) typeName(typ types.Type) string {
	return types.TypeString(typ, func(p *types.Package) string {
		if p.Path() == t.obj.Pkg().Path() {
			return ""
		}
		return p.Name()
	})
}

func (t *target) where(i iface) string {
	if !i.pos.IsValid() || !t.ws.Contains(t.ws.Fset.Position(i.pos).Filename) {
		return ""
	}
	return fmt.Sprintf(" (at %s)", t.ws.Position(i.pos))
}

// receiverBase returns the named type of a receiver, T or *T.
func receiverBase(typ types.Type) *types.Named {
	if p, ok := typ.(*types.Pointer); ok {
		typ = p.Elem()
	}
	n, _ := typ.(*types.Named)
	return n
}
