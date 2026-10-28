package workspace

import (
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// metaMode asks go list for the packages, their files and imports, and the
// metadata of their dependencies, without types: so that it compiles
// nothing.
const metaMode = packages.NeedName |
	packages.NeedFiles |
	packages.NeedCompiledGoFiles |
	packages.NeedImports |
	packages.NeedDeps |
	packages.NeedTypesSizes |
	packages.NeedModule

// trimpath is the build flag of every load. Without it, go's build cache
// keys a package by its directory, so a module copied to a new path (each
// A/B run) compiles again: minutes for Hugo, 2 s with it.
var trimpath = []string{"-trimpath"}

// loadPackages loads the packages of patterns in the module at root, with
// their tests, leaving out test mains: their metadata from go list, the
// types of the standard library's and other modules' packages from export
// data, and the module's own packages parsed and type-checked here, as
// Metadata.Load does.
//
// packages.Load with types has go list compile every package it lists to
// export data, the module's own too, though they are then type-checked
// from source. With a large dependency (the TypeScript compiler), each
// package depending on a changed one took seconds to compile, and a
// reload minutes.
func loadPackages(fset *token.FileSet, root string, patterns []string) ([]*packages.Package, error) {
	cfg := &packages.Config{Mode: metaMode, Dir: root, Fset: fset, Tests: true, BuildFlags: trimpath}
	all, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, err
	}
	roots := withoutTestMains(all)
	inModule := func(p *packages.Package) bool {
		files := p.GoFiles
		if len(files) == 0 {
			files = p.CompiledGoFiles
		}
		return len(files) > 0 && strings.HasPrefix(files[0], root+string(filepath.Separator))
	}
	// The module's packages besides the roots are test variants of their
	// dependencies (see sourcePackages); every other package imported is
	// the standard library's or another module's.
	seen := map[*packages.Package]bool{}
	for _, p := range roots {
		seen[p] = true
	}
	var extra []*packages.Package
	others := map[string][]*packages.Package{}
	var visit func(p *packages.Package)
	visit = func(p *packages.Package) {
		for _, imp := range p.Imports {
			if seen[imp] {
				continue
			}
			seen[imp] = true
			if inModule(imp) {
				extra = append(extra, imp)
			} else {
				others[imp.PkgPath] = append(others[imp.PkgPath], imp)
			}
			visit(imp)
		}
	}
	for _, p := range roots {
		visit(p)
	}
	sort.Slice(extra, func(i, j int) bool { return extra[i].ID < extra[j].ID })
	if err := exportTypes(fset, root, others, imported(roots, others)); err != nil {
		return nil, err
	}
	if err := check(fset, append(roots, extra...), len(roots)); err != nil {
		return nil, err
	}
	return roots, nil
}

// imported returns the paths of others that the module's packages import
// themselves (in roots or in the module's other packages), sorted.
func imported(roots []*packages.Package, others map[string][]*packages.Package) []string {
	direct := map[string]bool{}
	done := map[*packages.Package]bool{}
	var visit func(p *packages.Package)
	visit = func(p *packages.Package) {
		if done[p] {
			return
		}
		done[p] = true
		for path, imp := range p.Imports {
			if others[imp.PkgPath] != nil {
				direct[path] = true
			} else {
				visit(imp)
			}
		}
	}
	for _, p := range roots {
		visit(p)
	}
	var out []string
	for path := range direct {
		if path != "unsafe" && path != "C" {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

// exportTypes sets the types of the packages others, the standard
// library's and other modules', by path, from their export data: loading
// the paths the module imports, with their dependencies, which go list
// compiles where the build cache does not have them.
func exportTypes(fset *token.FileSet, root string, others map[string][]*packages.Package, paths []string) error {
	for _, p := range others["unsafe"] {
		p.Types = types.Unsafe
	}
	if len(paths) == 0 {
		return nil
	}
	// NeedExportFile, or with NeedDeps go/packages type-checks every
	// dependency from source.
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedImports | packages.NeedDeps | packages.NeedTypes | packages.NeedExportFile, Dir: root, Fset: fset, BuildFlags: trimpath}
	loaded, err := packages.Load(cfg, paths...)
	if err != nil {
		return err
	}
	packages.Visit(loaded, nil, func(l *packages.Package) {
		for _, p := range others[l.PkgPath] {
			p.Types = l.Types
		}
	})
	return nil
}
