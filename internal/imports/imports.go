// Package imports finds the name a file uses for a package and adds the
// imports a refactoring needs, for refactorings that move code which
// refers to other packages.
package imports

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// Adder knows the imports of one file and collects the ones to add and to
// remove.
type Adder struct {
	ws   *workspace.Workspace
	pkg  *packages.Package
	file *ast.File
	tf   *token.File
	src  []byte

	imported map[string]string // import path -> name in this file ("." for a dot import)
	added    map[string]string // imports to add: path -> name
	removed  map[*ast.ImportSpec]bool
}

// New returns an Adder for file (with contents src) of pkg.
func New(ws *workspace.Workspace, pkg *packages.Package, file *ast.File, src []byte) *Adder {
	a := &Adder{ws: ws, pkg: pkg, file: file, tf: ws.Fset.File(file.Pos()), src: src,
		imported: map[string]string{}, added: map[string]string{}, removed: map[*ast.ImportSpec]bool{}}
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

// KeepsUnused reports whether an edit is to leave the imports it makes
// unused, for the end of a batch to remove.
func (a *Adder) KeepsUnused() bool { return a.ws.KeepsUnusedImports() }

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

// Remove drops one of the file's imports: Edits deletes its line, or the
// whole import declaration when none of its imports remain.
func (a *Adder) Remove(spec *ast.ImportSpec) { a.removed[spec] = true }

// Edits adds the imports that Name needed and deletes the removed ones.
func (a *Adder) Edits() []edit.TextEdit {
	var edits []edit.TextEdit
	var group, single *ast.GenDecl // where added imports go
	for _, d := range a.file.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.IMPORT {
			continue
		}
		var gone []ast.Spec
		for _, s := range g.Specs {
			if a.removed[s.(*ast.ImportSpec)] {
				gone = append(gone, s)
			}
		}
		if len(gone) > 0 && len(gone) == len(g.Specs) {
			edits = append(edits, a.deleteLines(g.Pos(), g.End()))
			continue
		}
		for _, s := range gone {
			edits = append(edits, a.deleteLines(s.Pos(), s.End()))
		}
		if g.Lparen.IsValid() && group == nil {
			group = g
		} else if !g.Lparen.IsValid() && single == nil {
			single = g
		}
	}
	if len(a.added) == 0 {
		return edits
	}
	return append(edits, a.addEdits(group, single)...)
}

// addEdits inserts the added imports: into group, the file's first
// parenthesized import declaration, or else into single, made into a
// parenthesized one, or else into a new declaration. As goimports does,
// standard library imports are kept apart from the others: each goes,
// sorted, into a run of imports of its kind (runs are separated by blank
// lines), or into a new run, before the others for the standard library
// and after them otherwise.
func (a *Adder) addEdits(group, single *ast.GenDecl) []edit.TextEdit {
	off := func(p token.Pos) int { return a.tf.Offset(p) }
	if group == nil {
		var std, other []string
		add := func(path, text string) {
			if IsStd(path) {
				std = append(std, text)
			} else {
				other = append(other, text)
			}
		}
		if single != nil {
			sp := single.Specs[0].(*ast.ImportSpec)
			add(importPath(sp), string(a.src[off(sp.Pos()):off(sp.End())]))
		}
		for _, p := range a.paths() {
			add(p, a.spec(p))
		}
		sort.Slice(std, func(i, j int) bool { return specPath(std[i]) < specPath(std[j]) })
		sort.Slice(other, func(i, j int) bool { return specPath(other[i]) < specPath(other[j]) })
		var text string
		switch {
		case len(std)+len(other) == 1:
			text = "import " + strings.Join(append(std, other...), "")
		default:
			var runs []string
			for _, run := range [][]string{std, other} {
				if len(run) > 0 {
					runs = append(runs, "\t"+strings.Join(run, "\n\t")+"\n")
				}
			}
			text = "import (\n" + strings.Join(runs, "\n") + ")"
		}
		if single != nil {
			return []edit.TextEdit{{Start: off(single.Pos()), End: off(single.End()), NewText: text}}
		}
		at := off(a.file.Name.End())
		return []edit.TextEdit{{Start: at, End: at, NewText: "\n\n" + text}}
	}

	runs := a.runs(group)
	var edits []edit.TextEdit
	for _, std := range []bool{true, false} {
		var paths []string
		for _, p := range a.paths() {
			if IsStd(p) == std {
				paths = append(paths, p)
			}
		}
		if len(paths) == 0 {
			continue
		}
		run := -1
		for i, r := range runs {
			kind := slices.ContainsFunc(r, func(sp *ast.ImportSpec) bool { return IsStd(importPath(sp)) == std })
			if std && kind && !slices.ContainsFunc(r, func(sp *ast.ImportSpec) bool { return !IsStd(importPath(sp)) }) && run < 0 {
				run = i // the first run of only standard library imports
			} else if !std && kind {
				run = i // the last run with others
			}
		}
		if run < 0 {
			var text string
			for _, p := range paths {
				text += "\t" + a.spec(p) + "\n"
			}
			switch {
			case len(runs) == 0:
				edits = append(edits, a.beforeRparen(group, text))
			case std:
				at := off(a.tf.LineStart(a.tf.Line(runs[0][0].Pos())))
				edits = append(edits, edit.TextEdit{Start: at, End: at, NewText: text + "\n"})
			default:
				edits = append(edits, a.beforeRparen(group, "\n"+text))
			}
			continue
		}
		r := runs[run]
		for _, p := range paths {
			i := slices.IndexFunc(r, func(sp *ast.ImportSpec) bool { return importPath(sp) > p })
			if i < 0 {
				last := r[len(r)-1]
				if a.tf.Line(last.End()) == a.tf.Line(group.Rparen) {
					edits = append(edits, a.beforeRparen(group, "\t"+a.spec(p)+"\n"))
					continue
				}
				at := a.nextLine(last.End())
				edits = append(edits, edit.TextEdit{Start: at, End: at, NewText: "\t" + a.spec(p) + "\n"})
				continue
			}
			at := off(a.tf.LineStart(a.tf.Line(r[i].Pos())))
			edits = append(edits, edit.TextEdit{Start: at, End: at, NewText: "\t" + a.spec(p) + "\n"})
		}
	}
	return edits
}

// runs returns the imports of group that stay, in runs separated by blank
// lines (or by removed imports' lines).
func (a *Adder) runs(group *ast.GenDecl) [][]*ast.ImportSpec {
	var runs [][]*ast.ImportSpec
	prevLine := -1
	for _, s := range group.Specs {
		sp := s.(*ast.ImportSpec)
		if a.removed[sp] {
			continue
		}
		line := a.tf.Line(sp.Pos())
		if sp.Doc != nil {
			line = a.tf.Line(sp.Doc.Pos())
		}
		if len(runs) == 0 || line > prevLine+1 && a.blankBetween(prevLine, line) {
			runs = append(runs, nil)
		}
		runs[len(runs)-1] = append(runs[len(runs)-1], sp)
		prevLine = a.tf.Line(sp.End())
	}
	return runs
}

// blankBetween reports whether a blank line lies strictly between the
// lines from and to.
func (a *Adder) blankBetween(from, to int) bool {
	for l := from + 1; l < to; l++ {
		start := a.tf.Offset(a.tf.LineStart(l))
		end := len(a.src)
		if l+1 <= a.tf.LineCount() {
			end = a.tf.Offset(a.tf.LineStart(l + 1))
		}
		if strings.TrimSpace(string(a.src[start:end])) == "" {
			return true
		}
	}
	return false
}

// beforeRparen inserts text, whole lines, before the closing parenthesis
// of group.
func (a *Adder) beforeRparen(group *ast.GenDecl, text string) edit.TextEdit {
	at := a.tf.Offset(group.Rparen)
	start := a.tf.Offset(a.tf.LineStart(a.tf.Line(group.Rparen)))
	if strings.TrimSpace(string(a.src[start:at])) != "" {
		text = "\n" + text // `"fmt")` on one line
	} else {
		at = start
	}
	return edit.TextEdit{Start: at, End: at, NewText: text}
}

// nextLine returns the offset of the line after the one holding p.
func (a *Adder) nextLine(p token.Pos) int {
	o := a.tf.Offset(p)
	if i := strings.IndexByte(string(a.src[o:]), '\n'); i >= 0 {
		return o + i + 1
	}
	return len(a.src)
}

// IsStd reports whether path is (by the goimports rule) in the standard
// library: its first element has no dot.
func IsStd(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

func importPath(sp *ast.ImportSpec) string { return strings.Trim(sp.Path.Value, "`\"") }

// specPath returns the path of an import spec written as "name path" or
// "path".
func specPath(spec string) string {
	f := strings.Fields(spec)
	return strings.Trim(f[len(f)-1], "`\"")
}

// deleteLines deletes the whole lines from the one holding from to the one
// holding to.
func (a *Adder) deleteLines(from, to token.Pos) edit.TextEdit {
	start := a.tf.Offset(a.tf.LineStart(a.tf.Line(from)))
	end := len(a.src)
	if i := strings.IndexByte(string(a.src[a.tf.Offset(to):]), '\n'); i >= 0 {
		end = a.tf.Offset(to) + i + 1
	}
	return edit.TextEdit{Start: start, End: end}
}

// PathBase returns the last element of an import path, the name a package
// is usually imported as.
func PathBase(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}
