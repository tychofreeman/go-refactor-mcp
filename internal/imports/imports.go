// Package imports finds the name a file uses for a package and adds the
// imports a refactoring needs, for refactorings that move code which
// refers to other packages.
package imports

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// Adder knows the imports of one file and collects the ones to add.
type Adder struct {
	ws   *workspace.Workspace
	pkg  *packages.Package
	file *ast.File
	tf   *token.File
	src  []byte

	imported map[string]string // import path -> name in this file ("." for a dot import)
	added    map[string]string // imports to add: path -> name
}

// New returns an Adder for file (with contents src) of pkg.
func New(ws *workspace.Workspace, pkg *packages.Package, file *ast.File, src []byte) *Adder {
	a := &Adder{ws: ws, pkg: pkg, file: file, tf: ws.Fset.File(file.Pos()), src: src,
		imported: map[string]string{}, added: map[string]string{}}
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		switch {
		case imp.Name != nil && imp.Name.Name == "_":
		case imp.Name != nil:
			a.imported[path] = imp.Name.Name
		default:
			if q := analysis.ImportMap(pkg.Types)[path]; q != nil {
				a.imported[path] = q.Name()
			}
		}
	}
	return a
}

// Imported returns the name the file already imports path as ("." for a
// dot import), and whether it does.
func (a *Adder) Imported(path string) (string, bool) {
	n, ok := a.imported[path]
	return n, ok
}

// Name returns the qualifier that refers to package p in this file ("" for
// the file's own package or a dot import), adding an import of p if the
// file has none. The error says why p cannot be imported.
func (a *Adder) Name(p *types.Package) (string, error) {
	if p == a.pkg.Types {
		return "", nil
	}
	if n, ok := a.imported[p.Path()]; ok {
		if n == "." {
			return "", nil
		}
		return n, nil
	}
	if n, ok := a.added[p.Path()]; ok {
		return n, nil
	}
	if reason := a.Unimportable(p); reason != "" {
		return p.Name(), fmt.Errorf("%s", reason)
	}
	name := p.Name()
	for i := 2; !a.FreeName(name); i++ {
		name = fmt.Sprintf("%s%d", p.Name(), i)
	}
	a.added[p.Path()] = name
	return name, nil
}

// Unimportable explains why this file cannot import p, or returns "".
func (a *Adder) Unimportable(p *types.Package) string {
	path := p.Path()
	if p.Name() == "main" {
		return "which is a main package and cannot be imported"
	}
	if i := strings.LastIndex(path, "/internal/"); i >= 0 || strings.HasSuffix(path, "/internal") || strings.HasPrefix(path, "internal/") || path == "internal" {
		parent := ""
		switch {
		case i >= 0:
			parent = path[:i]
		case strings.HasSuffix(path, "/internal"):
			parent = strings.TrimSuffix(path, "/internal")
		}
		if parent != "" && a.pkg.PkgPath != parent && !strings.HasPrefix(a.pkg.PkgPath, parent+"/") {
			return fmt.Sprintf("which is internal to %s and cannot be imported from %s", parent, a.pkg.PkgPath)
		}
	}
	return ""
}

// FreeName reports whether a new import can use name in this file: no
// import, package-level declaration (in any variant of the package) or
// predeclared identifier uses it.
func (a *Adder) FreeName(name string) bool {
	if types.Universe.Lookup(name) != nil {
		return false
	}
	if s := a.pkg.TypesInfo.Scopes[a.file]; s != nil && s.Lookup(name) != nil {
		return false
	}
	for _, n := range a.added {
		if n == name {
			return false
		}
	}
	for _, p := range a.ws.Pkgs {
		if p.PkgPath == a.pkg.PkgPath && p.Types != nil && p.Types.Scope().Lookup(name) != nil {
			return false
		}
	}
	return true
}

// Added returns the imports to add, as "name path" or "path", sorted.
func (a *Adder) Added() []string {
	var out []string
	for _, p := range a.paths() {
		out = append(out, a.spec(p))
	}
	return out
}

// Paths returns the import paths to add, sorted.
func (a *Adder) Paths() []string { return a.paths() }

func (a *Adder) paths() []string {
	var paths []string
	for p := range a.added {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

func (a *Adder) spec(p string) string {
	s := strconv.Quote(p)
	if n := a.added[p]; n != PathBase(p) {
		s = n + " " + s
	}
	return s
}

// Edits adds the imports that Name needed.
func (a *Adder) Edits() []edit.TextEdit {
	if len(a.added) == 0 {
		return nil
	}
	var specs []string
	for _, p := range a.paths() {
		specs = append(specs, a.spec(p))
	}
	off := func(p token.Pos) int { return a.tf.Offset(p) }
	var group, single *ast.GenDecl
	for _, d := range a.file.Decls {
		if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.IMPORT {
			if g.Lparen.IsValid() && group == nil {
				group = g
			} else if !g.Lparen.IsValid() && single == nil {
				single = g
			}
		}
	}
	switch {
	case group != nil:
		at := off(group.Rparen)
		start := off(a.tf.LineStart(a.tf.Line(group.Rparen)))
		text := ""
		for _, sp := range specs {
			text += "\t" + sp + "\n"
		}
		if strings.TrimSpace(string(a.src[start:at])) != "" {
			text = "\n" + text // `"fmt")` on one line
		}
		return []edit.TextEdit{{Start: at, End: at, NewText: text}}
	case single != nil:
		text := "import (\n\t" + string(a.src[off(single.Specs[0].Pos()):off(single.Specs[0].End())]) + "\n"
		for _, sp := range specs {
			text += "\t" + sp + "\n"
		}
		return []edit.TextEdit{{Start: off(single.Pos()), End: off(single.End()), NewText: text + ")"}}
	default:
		at := off(a.file.Name.End())
		text := "\n\nimport " + specs[0]
		if len(specs) > 1 {
			text = "\n\nimport (\n\t" + strings.Join(specs, "\n\t") + "\n)"
		}
		return []edit.TextEdit{{Start: at, End: at, NewText: text}}
	}
}

// PathBase returns the last element of an import path, the name a package
// is usually imported as.
func PathBase(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}
