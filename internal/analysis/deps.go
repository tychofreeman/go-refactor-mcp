package analysis

import (
	"go/types"
	"sort"

	"golang.org/x/tools/go/packages"
)

// Dependencies returns every package the roots import, directly or not,
// that has type information, once per import path and sorted by it. Code
// in them can reach the roots' values through their interfaces without
// naming the roots' types (io.Copy calls WriteTo).
func Dependencies(roots ...*packages.Package) []*packages.Package {
	seen := map[string]bool{}
	var deps []*packages.Package
	var walk func(*packages.Package)
	walk = func(p *packages.Package) {
		for _, d := range p.Imports {
			if !seen[d.PkgPath] && d.Types != nil {
				seen[d.PkgPath] = true
				deps = append(deps, d)
				walk(d)
			}
		}
	}
	for _, r := range roots {
		walk(r)
	}
	sort.Slice(deps, func(i, j int) bool { return deps[i].PkgPath < deps[j].PkgPath })
	return deps
}

// PackageInterfaces returns the interface types p declares at package
// level, in name order, aliases left out.
func PackageInterfaces(p *types.Package) []*types.TypeName {
	var out []*types.TypeName
	scope := p.Scope()
	for _, n := range scope.Names() {
		if tn, ok := scope.Lookup(n).(*types.TypeName); ok && !tn.IsAlias() && types.IsInterface(tn.Type()) {
			out = append(out, tn)
		}
	}
	return out
}
