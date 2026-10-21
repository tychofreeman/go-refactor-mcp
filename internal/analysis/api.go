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
