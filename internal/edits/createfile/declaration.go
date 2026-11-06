package createfile

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// fromDeclaration returns the Go file at path made from a type's header:
// the package clause, the type (with an empty body for a struct or an
// interface given without one) and, for each interface of implements, an
// assertion that the type's pointer implements it and a stub for each of
// its methods.
func fromDeclaration(ws *workspace.Workspace, path, declaration string, implements []string) (string, error) {
	name, typeSrc, err := typeHeader(declaration)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(path)
	pkgName, err := dirPackage(dir)
	if err != nil {
		return "", err
	}
	own := dirTypes(ws, dir)
	if pkgName == "" {
		pkgName = filepath.Base(dir)
		if own != nil {
			pkgName = own.Name()
		}
	}
	imports := map[string]bool{}
	qualify := func(p *types.Package) string {
		if p == own {
			return ""
		}
		imports[p.Path()] = true
		return p.Name()
	}
	recv := receiverName(name)
	var asserts []string
	stubs := map[string]string{}
	for _, iname := range implements {
		iface, err := lookupInterface(ws, own, iname)
		if err != nil {
			return "", err
		}
		asserts = append(asserts, fmt.Sprintf("_ %s = (*%s)(nil)", types.TypeString(iface, qualify), name))
		u := iface.Underlying().(*types.Interface)
		for i := range u.NumMethods() {
			m := u.Method(i)
			if _, ok := stubs[m.Name()]; ok {
				continue
			}
			var sig bytes.Buffer
			types.WriteSignature(&sig, m.Type().(*types.Signature), qualify)
			stubs[m.Name()] = fmt.Sprintf("func (%s *%s) %s%s {\n\tpanic(\"not implemented\")\n}\n", recv, name, m.Name(), sig.String())
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", pkgName)
	if len(imports) > 0 {
		b.WriteString("import (\n")
		for _, p := range slices.Sorted(maps.Keys(imports)) {
			fmt.Fprintf(&b, "\t%q\n", p)
		}
		b.WriteString(")\n\n")
	}
	b.WriteString(typeSrc + "\n")
	if len(asserts) > 0 {
		b.WriteString("\nvar (\n")
		for _, a := range asserts {
			b.WriteString("\t" + a + "\n")
		}
		b.WriteString(")\n")
	}
	for _, m := range slices.Sorted(maps.Keys(stubs)) {
		b.WriteString("\n" + stubs[m])
	}
	out, err := format.Source([]byte(b.String()))
	if err != nil {
		return "", fmt.Errorf("the file made from declaration does not format: %v", err)
	}
	return string(out), nil
}

// typeHeader parses declaration as one type declaration and returns the
// type's name and its source, a struct or interface without a body given
// an empty one.
func typeHeader(declaration string) (string, string, error) {
	src := strings.TrimSpace(declaration)
	if strings.HasSuffix(src, "struct") || strings.HasSuffix(src, "interface") {
		src += "{}"
	}
	f, err := parser.ParseFile(token.NewFileSet(), "", "package p\n\n"+src, 0)
	if err == nil && len(f.Decls) == 1 {
		if g, ok := f.Decls[0].(*ast.GenDecl); ok && g.Tok == token.TYPE && len(g.Specs) == 1 {
			return g.Specs[0].(*ast.TypeSpec).Name.Name, src, nil
		}
	}
	return "", "", refactor.Refusef("declaration is a type's header, such as \"type Cart struct\" or \"type Celsius float64\"; to write other code, give content")
}

// dirTypes returns the types of the package of the Go files in dir, or
// nil if the workspace has none there.
func dirTypes(ws *workspace.Workspace, dir string) *types.Package {
	for _, p := range ws.Pkgs {
		for _, f := range p.GoFiles {
			if filepath.Dir(f) == dir && !strings.HasSuffix(p.Name, "_test") && p.Types != nil {
				return p.Types
			}
		}
	}
	return nil
}

// lookupInterface finds the interface type name names: Name in own, or
// pkg.Name with pkg a package's import path or the name of one the
// workspace loads.
func lookupInterface(ws *workspace.Workspace, own *types.Package, name string) (*types.Named, error) {
	var obj types.Object
	if i := strings.LastIndex(name, "."); i < 0 {
		if own != nil {
			obj = own.Scope().Lookup(name)
		}
	} else if p := findPackage(ws, name[:i]); p != nil {
		obj = p.Scope().Lookup(name[i+1:])
	}
	if tn, ok := obj.(*types.TypeName); ok {
		if n, ok := tn.Type().(*types.Named); ok && types.IsInterface(n) {
			return n, nil
		}
	}
	return nil, refactor.Refusef("implements names %s, which is not an interface of this package or of a package it can import (give Name, or pkg.Name with pkg an import path such as io)", name)
}

// findPackage returns the package whose import path or name is path,
// among the workspace's packages and what they import, else from the
// standard importer.
func findPackage(ws *workspace.Workspace, path string) *types.Package {
	var byName *types.Package
	seen := map[*types.Package]bool{}
	var visit func(p *types.Package) *types.Package
	visit = func(p *types.Package) *types.Package {
		if p == nil || seen[p] {
			return nil
		}
		seen[p] = true
		if p.Path() == path {
			return p
		}
		if p.Name() == path && byName == nil {
			byName = p
		}
		for _, q := range p.Imports() {
			if found := visit(q); found != nil {
				return found
			}
		}
		return nil
	}
	for _, p := range ws.Pkgs {
		if found := visit(p.Types); found != nil {
			return found
		}
	}
	if byName != nil {
		return byName
	}
	if p, err := importer.Default().Import(path); err == nil {
		return p
	}
	return nil
}

// receiverName is the stubs' receiver: the type's first letter, lower case.
func receiverName(typeName string) string {
	for _, r := range typeName {
		return string(unicode.ToLower(r))
	}
	return "x"
}
