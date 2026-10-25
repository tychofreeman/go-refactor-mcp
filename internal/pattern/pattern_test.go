package pattern

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/packages"
)

// load type-checks src and returns its package and the value of each
// variable it declares at the top level, by name.
func load(t *testing.T, src string) (*packages.Package, map[string]ast.Expr) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	if _, err := (&types.Config{Importer: importer.Default()}).Check("p", fset, []*ast.File{file}, info); err != nil {
		t.Fatal(err)
	}
	vals := map[string]ast.Expr{}
	for _, d := range file.Decls {
		if g, ok := d.(*ast.GenDecl); ok {
			for _, s := range g.Specs {
				if v, ok := s.(*ast.ValueSpec); ok && len(v.Values) == 1 {
					vals[v.Names[0].Name] = v.Values[0]
				}
			}
		}
	}
	return &packages.Package{TypesInfo: info}, vals
}

func TestMatchWithoutRelaxations(t *testing.T) {
	pkg, v := load(t, `package p

var s = "a"
var pattern = s + "b"
var same = s + "b"
var joined = "ab"
var other = s + "c"
`)
	m := &Matcher{Pkg: pkg, Holes: map[types.Object]int{}}
	info := pkg.TypesInfo
	if !m.Match(v["pattern"], v["same"], info, map[HoleKey]ast.Expr{}) {
		t.Errorf("s + \"b\" does not match itself")
	}
	if m.Match(v["pattern"], v["joined"], info, map[HoleKey]ast.Expr{}) {
		t.Errorf("without relaxations, s + \"b\" matches \"ab\"")
	}
	if m.Match(v["pattern"], v["other"], info, map[HoleKey]ast.Expr{}) {
		t.Errorf("s + \"b\" matches s + \"c\"")
	}
	if m.Miss == nil || m.Miss.Depth != 1 {
		t.Errorf("miss = %+v, want the operands \"b\" and \"c\", one below the top", m.Miss)
	}
}
