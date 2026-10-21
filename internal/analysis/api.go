package analysis

import (
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

// PublicAPI reports whether obj is part of what other modules can use:
// exported, in a package they can import (not main, and not under an
// internal directory of its module). pkgs give the module's path.
func PublicAPI(obj types.Object, pkgs []*packages.Package) bool {
	if !obj.Exported() || obj.Pkg() == nil || obj.Pkg().Name() == "main" {
		return false
	}
	path := obj.Pkg().Path()
	for _, p := range pkgs {
		if p.PkgPath == path && p.Module != nil {
			path = strings.TrimPrefix(path, p.Module.Path)
			break
		}
	}
	return !slices.Contains(strings.Split(path, "/"), "internal")
}

// DynamicMethods are method names the standard library and common
// packages look for at run time, by the interface that has them: a type
// gaining or losing one changes behavior with no compile error.
var DynamicMethods = map[string]string{
	"String": "fmt.Stringer", "GoString": "fmt.GoStringer", "Format": "fmt.Formatter", "Error": "error",
	"Unwrap": "errors.Unwrap", "Is": "errors.Is", "As": "errors.As",
	"MarshalJSON": "json.Marshaler", "UnmarshalJSON": "json.Unmarshaler",
	"MarshalText": "encoding.TextMarshaler", "UnmarshalText": "encoding.TextUnmarshaler",
	"MarshalBinary": "encoding.BinaryMarshaler", "UnmarshalBinary": "encoding.BinaryUnmarshaler",
	"MarshalXML": "xml.Marshaler", "UnmarshalXML": "xml.Unmarshaler",
	"MarshalYAML": "yaml.Marshaler", "UnmarshalYAML": "yaml.Unmarshaler",
	"Scan": "sql.Scanner", "Value": "driver.Valuer",
}

// MethodInterface names an interface through which recv gaining an
// exported method called name (adding), or losing it, could change
// behavior with no compile error: one in DynamicMethods, or a
// package-level interface of pkgs or what they import that recv or *recv
// would start implementing (every other method of it is in the method
// set) or would stop implementing. It returns "" when there is none.
func MethodInterface(pkgs []*packages.Package, recv *types.Named, name string, adding bool) string {
	if iface, ok := DynamicMethods[name]; ok {
		return iface
	}
	seen := map[string]bool{}
	for _, p := range append(append([]*packages.Package{}, pkgs...), Dependencies(pkgs...)...) {
		if p.Types == nil || seen[p.PkgPath] {
			continue
		}
		seen[p.PkgPath] = true
		for _, tn := range PackageInterfaces(p.Types) {
			it := tn.Type().Underlying().(*types.Interface)
			for _, T := range []types.Type{recv, types.NewPointer(recv)} {
				if hasMethod(it, name) && (adding && implementsWith(T, it, name) || !adding && types.Implements(T, it)) {
					return p.Types.Name() + "." + tn.Name()
				}
			}
		}
	}
	return ""
}

func hasMethod(it *types.Interface, name string) bool {
	for i := 0; i < it.NumMethods(); i++ {
		if it.Method(i).Name() == name {
			return true
		}
	}
	return false
}

// implementsWith reports whether T would implement it with a method
// called name, whatever that method's signature: every other method of it
// is in T's method set.
func implementsWith(T types.Type, it *types.Interface, name string) bool {
	ms := types.NewMethodSet(T)
	for i := 0; i < it.NumMethods(); i++ {
		m := it.Method(i)
		if m.Name() == name {
			continue
		}
		sel := ms.Lookup(m.Pkg(), m.Name())
		if sel == nil || !types.Identical(sel.Obj().Type(), m.Type()) {
			return false
		}
	}
	return true
}
