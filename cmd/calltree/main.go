// Command calltree prints a module's call tree without bodies: for each
// package, its types, and each function and method with its signature,
// the first line of its doc comment, and the functions of the module it
// calls (a call through an interface as the interface's method).
//
//	calltree [dir]
package main

import (
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"os"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

func main() {
	dir := "."
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	out, err := tree(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "calltree:", err)
		os.Exit(1)
	}
	fmt.Print(out)
}

func tree(dir string) (string, error) {
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo, Dir: dir}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return "", err
	}
	if packages.PrintErrors(pkgs) > 0 {
		return "", fmt.Errorf("the module in %s does not load", dir)
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].PkgPath < pkgs[j].PkgPath })
	module := map[string]bool{}
	for _, p := range pkgs {
		module[p.PkgPath] = true
	}
	var b strings.Builder
	for _, p := range pkgs {
		fmt.Fprintf(&b, "package %s\n", p.PkgPath)
		for _, f := range p.Syntax {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok {
					b.WriteString(typeDecls(p.Fset, d))
					continue
				}
				b.WriteString(signature(p.Fset, fd) + "\n")
				if fd.Doc != nil {
					fmt.Fprintf(&b, "\t// %s\n", strings.TrimSpace(strings.SplitN(fd.Doc.Text(), "\n", 2)[0]))
				}
				if calls := callees(p, fd, module); len(calls) > 0 {
					fmt.Fprintf(&b, "\tcalls: %s\n", strings.Join(calls, ", "))
				}
			}
		}
	}
	return b.String(), nil
}

// gofmt prints as gofmt does, fields aligned with spaces.
var gofmt = printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}

// signature is fd's declaration without its doc comment and body.
func signature(fset *token.FileSet, fd *ast.FuncDecl) string {
	d := *fd
	d.Doc, d.Body = nil, nil
	var b strings.Builder
	gofmt.Fprint(&b, fset, &d)
	return b.String()
}

// typeDecls is the type declarations of d, each with the first line of
// its doc comment, or "".
func typeDecls(fset *token.FileSet, d ast.Decl) string {
	gd, ok := d.(*ast.GenDecl)
	if !ok || gd.Tok != token.TYPE {
		return ""
	}
	var b strings.Builder
	for _, s := range gd.Specs {
		ts := s.(*ast.TypeSpec)
		doc := ts.Doc
		if doc == nil && len(gd.Specs) == 1 {
			doc = gd.Doc
		}
		spec := *ts
		spec.Doc, spec.Comment = nil, nil
		b.WriteString("type ")
		gofmt.Fprint(&b, fset, &spec)
		b.WriteString("\n")
		if doc != nil {
			fmt.Fprintf(&b, "\t// %s\n", strings.TrimSpace(strings.SplitN(doc.Text(), "\n", 2)[0]))
		}
	}
	return b.String()
}

// callees lists, in the order first called, the functions and methods of
// the module's packages (paths in module) that fd's body calls: Name in
// its own package, pkg.Name in another, Type.Method for a method, and
// Iface.Method for a call through an interface.
func callees(p *packages.Package, fd *ast.FuncDecl, module map[string]bool) []string {
	if fd.Body == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		var id *ast.Ident
		switch f := ast.Unparen(call.Fun).(type) {
		case *ast.Ident:
			id = f
		case *ast.SelectorExpr:
			id = f.Sel
		}
		fn, ok := p.TypesInfo.Uses[id].(*types.Func)
		if id == nil || !ok || fn.Pkg() == nil || !module[fn.Pkg().Path()] {
			return true
		}
		name := fn.Name()
		if recv := fn.Signature().Recv(); recv != nil {
			t := recv.Type()
			if ptr, ok := t.(*types.Pointer); ok {
				t = ptr.Elem()
			}
			if named, ok := t.(*types.Named); ok {
				name = named.Obj().Name() + "." + name
			}
		}
		if fn.Pkg().Path() != p.PkgPath {
			name = fn.Pkg().Name() + "." + name
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
		return true
	})
	return out
}
