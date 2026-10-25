package pattern

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"testing"

	"golang.org/x/tools/go/packages"
)

// load type-checks src and returns its package and the value of each
// variable it declares at the top level, by name.
func load(t *testing.T, src string) (*packages.Package, map[string]ast.Expr) {
	file, info := check(t, src)
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

// check parses and type-checks src as package p.
func check(t *testing.T, src string) (*ast.File, *types.Info) {
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
	return file, info
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
	if !m.At(pkg.TypesInfo).Match(v["pattern"], v["same"]) {
		t.Errorf("s + \"b\" does not match itself")
	}
	if m.At(pkg.TypesInfo).Match(v["pattern"], v["joined"]) {
		t.Errorf("without relaxations, s + \"b\" matches \"ab\"")
	}
	a := m.At(pkg.TypesInfo)
	if a.Match(v["pattern"], v["other"]) {
		t.Errorf("s + \"b\" matches s + \"c\"")
	}
	if a.Miss == nil || a.Miss.Depth != 1 {
		t.Errorf("miss = %+v, want the operands \"b\" and \"c\", one below the top", a.Miss)
	}
}

func TestAttemptBindsHoles(t *testing.T) {
	pkg, v := load(t, `package p

var s = "a"
var u = "b"
var pattern = s + "!"
var code = u + "!"
`)
	s := pkg.TypesInfo.Uses[v["pattern"].(*ast.BinaryExpr).X.(*ast.Ident)]
	m := &Matcher{Pkg: pkg, Holes: map[types.Object]int{s: 1}}
	a := m.At(pkg.TypesInfo)
	if !a.Match(v["pattern"], v["code"]) {
		t.Fatal("s + \"!\" with s a hole does not match u + \"!\"")
	}
	if got := types.ExprString(a.Bound[HoleKey{Obj: s}]); got != "u" {
		t.Errorf("s is bound to %q, want u", got)
	}
	if b := m.At(pkg.TypesInfo).Bound; len(b) != 0 {
		t.Errorf("a new attempt starts with %v bound", b)
	}
}

func TestMatchStructural(t *testing.T) {
	file, info := check(t, `package p

func a(n int) string {
	switch {
	case n > 1:
		return "many"
	}
	return "one"
}

func b(k int) string {
	switch {
	case k > 2:
		return "lots"
	}
	return "one"
}

func c(k int) string {
	switch {
	case k >= 2:
		return "lots"
	}
	return "one"
}
`)
	fns := map[string]*ast.FuncDecl{}
	for _, d := range file.Decls {
		if f, ok := d.(*ast.FuncDecl); ok {
			fns[f.Name.Name] = f
		}
	}
	param := func(fn string) types.Object { return info.Defs[fns[fn].Type.Params.List[0].Names[0]] }
	compare := func(other string) (*Attempt, map[string]string, bool) {
		lits := map[string]string{}
		m := &Matcher{
			Pkg:        &packages.Package{TypesInfo: info},
			Structural: true,
			Literal: func(p, x *ast.BasicLit) bool {
				lits[p.Value] = x.Value
				return true
			},
		}
		a := m.At(info)
		a.Locals = map[types.Object]types.Object{param("a"): param(other)}
		return a, lits, a.MatchStmt(fns["a"].Body, fns[other].Body)
	}
	_, lits, ok := compare("b")
	if !ok {
		t.Fatal("a and b, the same but for constants and their parameter's name, do not match")
	}
	if want := map[string]string{"1": "2", `"many"`: `"lots"`, `"one"`: `"one"`}; !maps.Equal(lits, want) {
		t.Errorf("constants = %v, want %v", lits, want)
	}
	m, _, ok := compare("c")
	if ok {
		t.Fatal("a and c, whose conditions differ in their operator, match")
	}
	if m.Miss == nil || types.ExprString(m.Miss.P.(ast.Expr)) != "n > 1" {
		t.Errorf("miss = %+v, want at n > 1", m.Miss)
	}
}
