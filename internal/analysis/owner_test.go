package analysis

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

const ownerSrc = `package p

var g int

type T struct{ n int }

func f(p *T, xs []int) {
	a := 1
	b := 2
	c := 3
	m := map[string]int{}
	_ = &b
	go func() { c++ }()
	_ = a + g + p.n + xs[0] + m["k"]
}
`

func TestOwner(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p.go", ownerSrc, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := newMoveInfo()
	if _, err := (&types.Config{}).Check("example.com/p", fset, []*ast.File{file}, info); err != nil {
		t.Fatal(err)
	}
	fd := file.Decls[len(file.Decls)-1].(*ast.FuncDecl)
	f := AnalyzeFunc(info, fd)
	want := map[string]Owner{"a": ThisCode, "g": Anyone, "p.n": Anyone, "xs[0]": Anyone, `m["k"]`: ThisFunc, "b": ThisFunc, "c": ThisFunc}
	last := fd.Body.List[len(fd.Body.List)-1].(*ast.AssignStmt).Rhs[0]
	seen := 0
	ast.Inspect(last, func(n ast.Node) bool {
		x, ok := n.(ast.Expr)
		if !ok {
			return true
		}
		if w, ok := want[types.ExprString(x)]; ok {
			seen++
			if got := f.Owner(x); got != w {
				t.Errorf("%s: owner %v, want %v", types.ExprString(x), got, w)
			}
			return false
		}
		return true
	})
	for _, name := range []string{"b", "c"} {
		seen++
		var found *types.Var
		for id, o := range info.Defs {
			if id.Name == name && o != nil {
				found, _ = o.(*types.Var)
			}
		}
		if got := f.Owns(found); got != want[name] {
			t.Errorf("%s: owns %v, want %v", name, got, want[name])
		}
	}
	if seen != len(want) {
		t.Errorf("checked %d, want %d", seen, len(want))
	}
}
