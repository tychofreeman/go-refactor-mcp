package analysis

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

const effectsSrc = `package p

import (
	"errors"
	"fmt"
	"go/ast"
	"sort"
)

var counter int

type counterT struct{ n int }

func pureAdd(a, b int) int { return a + b }

func div(a, b int) int { return a / b }

func readGlobal() int { return counter }

func bump() { counter++ }

func first(xs []int) int { return xs[0] }

func setFirst(xs []int) { xs[0] = 1 }

func build(n int) []int {
	out := make([]int, 0, n)
	for i := range n {
		out = append(out, i)
	}
	return out
}

func fill(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

func mapBuild(keys []string) map[string]bool {
	m := map[string]bool{}
	for _, k := range keys {
		m[k] = true
	}
	return m
}

func callsPure() int { return pureAdd(1, 2) }

func callsBump() { bump() }

func loop(n int) int {
	for n > 1 {
		n /= 2
	}
	return n
}

func recursive(n int) int {
	if n == 0 {
		return 0
	}
	return recursive(n - 1)
}

func iface(s fmt.Stringer) string { return s.String() }

func printIt() { fmt.Println() }

func aliasWrite(n int) []int {
	out := make([]int, n)
	alias := out
	alias[0] = 1
	return out
}

func (c *counterT) get() int { return c.n }

func (c *counterT) inc() { c.n++ }

func (c counterT) incCopy() int {
	c.n++
	return c.n
}

func visit(n ast.Node) int {
	k := 0
	ast.Inspect(n, func(ast.Node) bool {
		k++
		return true
	})
	return k
}

func sortFresh(n int) []int {
	xs := make([]int, n)
	sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
	return xs
}

func sortParam(xs []int) {
	sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
}

func newErr() error { return errors.New("x") }

func goIt() { go pureAdd(1, 2) }

func ptrFresh() int {
	p := &counterT{}
	p.n = 3
	return p.n
}

func callsGet(c *counterT) int { return c.get() }
`

func TestEffectsFunc(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", effectsSrc, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{
		Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{},
		Types: map[ast.Expr]types.TypeAndValue{}, Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	conf := types.Config{Importer: importer.Default()}
	pkg, err := conf.Check("example.com/p", fset, []*ast.File{f}, info)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEffects()
	e.AddPackage([]*ast.File{f}, info)
	// Which of effect, reads and unsafe each function has.
	want := map[string][3]bool{
		"pureAdd":    {false, false, false},
		"div":        {false, false, true},
		"readGlobal": {false, true, false},
		"bump":       {true, true, false},
		"first":      {false, true, true},
		"setFirst":   {true, true, true},
		"build":      {false, false, false},
		"fill":       {false, false, true},
		"mapBuild":   {false, true, false},
		"callsPure":  {false, false, false},
		"callsBump":  {true, true, false},
		"loop":       {false, false, true},
		"recursive":  {true, false, false},
		"iface":      {true, false, false},
		"printIt":    {true, false, false},
		"aliasWrite": {true, true, true},
		"visit":      {false, true, false},
		"sortFresh":  {false, false, true},
		"sortParam":  {true, true, true},
		"newErr":     {false, false, false},
		"goIt":       {true, false, false},
		"ptrFresh":   {false, false, false},
		"callsGet":   {false, true, true},
		"get":        {false, true, true},
		"inc":        {true, true, true},
		"incCopy":    {false, false, false},
	}
	counterT := pkg.Scope().Lookup("counterT").Type()
	for name, w := range want {
		obj := pkg.Scope().Lookup(name)
		if obj == nil {
			ms := types.NewMethodSet(types.NewPointer(counterT))
			for i := 0; i < ms.Len(); i++ {
				if ms.At(i).Obj().Name() == name {
					obj = ms.At(i).Obj()
				}
			}
		}
		fn, _ := obj.(*types.Func)
		if fn == nil {
			t.Fatalf("no function %s", name)
		}
		s := e.Func(fn)
		if s == nil {
			t.Errorf("%s: no summary", name)
			continue
		}
		got := [3]bool{s.Effect != "", s.Reads != "", s.Unsafe != ""}
		if got != w {
			t.Errorf("%s: effect, reads, unsafe = %v, want %v (summary %+v)", name, got, w, *s)
		}
	}
}
