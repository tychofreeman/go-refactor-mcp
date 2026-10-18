package analysis

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestDescribe(t *testing.T) {
	src := `package p

func f(xs []int, m map[string]int, c chan int) int {
	k := 0
	k = 1
	k++
	if k > 0 {
		return k + 1
	}
	for _, x := range xs {
		_ = -x
	}
	for k < 3 {
		break
	}
	switch k {
	case 1:
	}
	defer f(nil, nil, nil)
	c <- k
	return len(xs[1:]) * m["a"]
}
`
	f, err := parser.ParseFile(token.NewFileSet(), "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"an if statement": false, "a short variable declaration": false, "an assignment": false,
		"an increment": false, "a return statement": false, "a + operation": false,
		"a range loop": false, "a for loop": false, "a unary - operation": false,
		"a break statement": false, "a switch statement": false, "a case clause": false,
		"a defer statement": false, "a send statement": false, "a call": false,
		"a slice expression": false, "an index expression": false, "a * operation": false,
		"a name": false, "a literal": false, "a function declaration": false,
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		d := Describe(n)
		if d == "" {
			t.Errorf("no words for %T", n)
		}
		if _, ok := want[d]; ok {
			want[d] = true
		}
		return true
	})
	for w, seen := range want {
		if !seen {
			t.Errorf("no node was described as %q", w)
		}
	}
}
