// Package gocode holds what the edits share for the Go source they are
// given: parsing it, re-indenting it, and adding (and removing) the imports
// it needs.
package gocode

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/tools/go/packages"

	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// Code is the parsed new code an edit was given.
type Code struct {
	Body *ast.BlockStmt // the statements, as a block (for statements)
	Raw  bool           // a raw string spans lines, so the text must not be re-indented
	node ast.Node       // what was parsed: Body, or the declarations' file
}

// ParseStatements parses text as a list of statements.
func ParseStatements(text string) (*Code, error) {
	src := "package p\nfunc _() {\n" + text + "\n}\n"
	f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.ParseComments)
	if err != nil {
		return nil, refactor.Refusef("content is not a list of Go statements: %v", err)
	}
	body := f.Decls[0].(*ast.FuncDecl).Body
	c := &Code{Body: body, node: body}
	ast.Inspect(c.Body, func(n ast.Node) bool {
		if b, ok := n.(*ast.BasicLit); ok && strings.HasPrefix(b.Value, "`") && strings.Contains(b.Value, "\n") {
			c.Raw = true
		}
		return true
	})
	return c, nil
}

// declared returns the names the content declares anywhere in it, which
// a selector's operand may then be, rather than a package.
func (c *Code) declared() map[string]bool {
	names := map[string]bool{}
	add := func(e ast.Expr) {
		if id, ok := e.(*ast.Ident); ok {
			names[id.Name] = true
		}
	}
	ast.Inspect(c.node, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncDecl:
			add(n.Name)
		case *ast.AssignStmt:
			if n.Tok == token.DEFINE {
				for _, l := range n.Lhs {
					add(l)
				}
			}
		case *ast.ValueSpec:
			for _, id := range n.Names {
				add(id)
			}
		case *ast.TypeSpec:
			add(n.Name)
		case *ast.Field:
			for _, id := range n.Names {
				add(id)
			}
		case *ast.RangeStmt:
			if n.Tok == token.DEFINE {
				add(n.Key)
				if n.Value != nil {
					add(n.Value)
				}
			}
		case *ast.LabeledStmt:
			add(n.Label)
		}
		return true
	})
	return names
}

// packageRefs returns the names the content uses as selector operands
// that it does not declare, which may be packages.
func (c *Code) packageRefs() []string {
	declared := c.declared()
	seen := map[string]bool{}
	var out []string
	ast.Inspect(c.node, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && !declared[id.Name] && !seen[id.Name] {
				seen[id.Name] = true
				out = append(out, id.Name)
			}
		}
		return true
	})
	return out
}

// AddImports imports the packages the content names that are not visible
// at pos, and returns their paths.
func AddImports(ws *workspace.Workspace, pkg *packages.Package, file *ast.File, adder *imports.Adder, pos token.Pos, c *Code, chosen []string) ([]string, error) {
	scope := pkg.Types.Scope().Innermost(pos)
	var added []string
	for _, name := range c.packageRefs() {
		if scope != nil {
			if _, o := scope.LookupParent(name, pos); o != nil {
				continue // a package already imported, or something else in scope
			}
		}
		path, err := resolve(ws, pkg, name, chosen)
		if err != nil {
			return nil, err
		}
		got, err := adder.Name(types.NewPackage(path, name))
		if err != nil {
			return nil, refactor.Refusef("cannot import %s here: %v", path, err)
		}
		if got != name {
			return nil, refactor.Refusef("the content uses %s for %s, but that name is taken in the file", name, path)
		}
		added = append(added, path)
	}
	return added, nil
}

// resolve finds the package the content means by name: one of chosen,
// else what another file of the package imports as name, else a workspace
// package of that name, else a standard library one; it must be unique.
func resolve(ws *workspace.Workspace, pkg *packages.Package, name string, chosen []string) (string, error) {
	for _, p := range chosen {
		if importName(p) == name {
			return p, nil
		}
	}
	try := []func() []string{
		func() []string { // another file of the package
			set := map[string]bool{}
			for _, p := range ws.Pkgs {
				if p.PkgPath != pkg.PkgPath || p.TypesInfo == nil {
					continue
				}
				for _, o := range p.TypesInfo.Defs {
					if pn, ok := o.(*types.PkgName); ok && pn.Name() == name {
						set[pn.Imported().Path()] = true
					}
				}
				for _, o := range p.TypesInfo.Implicits {
					if pn, ok := o.(*types.PkgName); ok && pn.Name() == name {
						set[pn.Imported().Path()] = true
					}
				}
			}
			return keys(set)
		},
		func() []string { // a workspace package
			set := map[string]bool{}
			for _, p := range ws.Pkgs {
				if p.Name == name && p.PkgPath != pkg.PkgPath && !strings.HasSuffix(p.PkgPath, "_test") {
					set[p.PkgPath] = true
				}
			}
			return keys(set)
		},
		func() []string { return stdPackages()[name] },
	}
	for _, f := range try {
		switch paths := f(); len(paths) {
		case 0:
		case 1:
			return paths[0], nil
		default:
			return "", refactor.Refusef("%s could be %s; name the one to import in imports", name, joinOr(paths))
		}
	}
	return "", refactor.Refusef("the content uses %s, which is not in scope there and no package of that name was found; declare it, or name the package to import in imports", name)
}

// importName is the name a package at path is usually imported as.
func importName(path string) string {
	base := imports.PathBase(path)
	if isVersion(base) {
		return imports.PathBase(strings.TrimSuffix(path, "/"+base))
	}
	return base
}

var (
	stdOnce sync.Once
	stdMap  map[string][]string
)

// stdPackages maps package names to the standard library's import paths.
func stdPackages() map[string][]string {
	stdOnce.Do(func() {
		stdMap = map[string][]string{}
		root := filepath.Join(build.Default.GOROOT, "src")
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			name := d.Name()
			rel, _ := filepath.Rel(root, p)
			if rel != "." && (name == "internal" || name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || rel == "cmd") {
				return filepath.SkipDir
			}
			if rel == "." || !hasGoFiles(p) {
				return nil
			}
			path := filepath.ToSlash(rel)
			stdMap[importName(path)] = append(stdMap[importName(path)], path)
			return nil
		})
		for _, ps := range stdMap {
			sort.Strings(ps)
		}
	})
	return stdMap
}

func hasGoFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if n := e.Name(); strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") {
			return true
		}
	}
	return false
}

// RemoveImports removes the file's imports that only the replaced range
// [from, to) used and the content does not, and returns their paths.
func RemoveImports(pkg *packages.Package, file *ast.File, adder *imports.Adder, from, to token.Pos, c *Code) []string {
	refs := map[string]bool{}
	for _, n := range c.packageRefs() {
		refs[n] = true
	}
	var removed []string
	for _, imp := range file.Imports {
		pn := pkgNameOf(pkg.TypesInfo, imp)
		if pn == nil || refs[pn.Name()] {
			continue
		}
		inside, outside := false, false
		for id, o := range pkg.TypesInfo.Uses {
			if o != pn || id.Pos() < file.FileStart || id.Pos() >= file.FileEnd {
				continue
			}
			if from <= id.Pos() && id.Pos() < to {
				inside = true
			} else {
				outside = true
			}
		}
		if inside && !outside {
			adder.Remove(imp)
			removed = append(removed, pn.Imported().Path())
		}
	}
	return removed
}

// ImportNote describes, for a result's notes, the imports an edit added
// or removed: "added import \"fmt\", \"os\"".
func ImportNote(verb string, paths []string) string {
	var quoted []string
	for _, p := range paths {
		quoted = append(quoted, strconv.Quote(p))
	}
	return verb + " import " + strings.Join(quoted, ", ")
}

func pkgNameOf(info *types.Info, imp *ast.ImportSpec) *types.PkgName {
	var o types.Object
	if imp.Name != nil {
		o = info.Defs[imp.Name]
	} else {
		o = info.Implicits[imp]
	}
	pn, _ := o.(*types.PkgName)
	if pn != nil && (pn.Name() == "_" || pn.Name() == ".") {
		return nil
	}
	return pn
}

// Reindent removes the content's common indentation and indents each line
// with indent, unless a raw string spans lines (its text must not change).
func Reindent(text, indent string, raw bool) string {
	lines := strings.Split(strings.Trim(text, "\n"), "\n")
	if raw {
		return indent + strings.TrimLeft(strings.Join(lines, "\n"), " \t")
	}
	common, first := "", true
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		lead := l[:len(l)-len(strings.TrimLeft(l, " \t"))]
		if first {
			common, first = lead, false
		}
		for !strings.HasPrefix(lead, common) {
			common = common[:len(common)-1]
		}
	}
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			lines[i] = ""
			continue
		}
		lines[i] = indent + strings.TrimPrefix(l, common)
	}
	return strings.Join(lines, "\n")
}

// IndentOf returns the whitespace that starts the line at offset start.
func IndentOf(src []byte, start int) string {
	i := start
	for i < len(src) && (src[i] == ' ' || src[i] == '\t') {
		i++
	}
	return string(src[start:i])
}

func keys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func joinOr(xs []string) string {
	if len(xs) <= 1 {
		return strings.Join(xs, "")
	}
	return strings.Join(xs[:len(xs)-1], ", ") + " or " + xs[len(xs)-1]
}

// isVersion reports whether elem is a major version suffix, like v2.
func isVersion(elem string) bool {
	if len(elem) < 2 || elem[0] != 'v' {
		return false
	}
	for _, r := range elem[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
