package analysis

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

func TestKnownCall(t *testing.T) {
	src := `package p

import (
	"bytes"
	"fmt"
	"go/types"
	"path/filepath"
	"strings"
)

func mine(s string) string { return s }

func f(s string, xs []string, b []byte, o types.Object, pkg *types.Package) {
	_ = strings.ToUpper(s)
	_ = strings.Repeat(s, 2)
	_ = filepath.Join(s, "x")
	_ = filepath.Join(xs...)
	_ = o.Name()
	_ = pkg.Path()
	_ = fmt.Sprint(s)
	_ = bytes.ToUpper(b)
	_ = strings.Join(xs, ",")
	_ = mine(s)
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Types: map[ast.Expr]types.TypeAndValue{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
	conf := types.Config{Importer: importer.Default()}
	if _, err := conf.Check("example.com/p", fset, []*ast.File{f}, info); err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		known  bool
		panics PanicKind
	}{
		"strings.ToUpper(s)":    {true, NoPanic},
		"strings.Repeat(s, 2)":  {true, OtherPanic},
		`filepath.Join(s, "x")`: {true, NoPanic},
		"filepath.Join(xs...)":  {false, 0}, // reads the slice
		"o.Name()":              {true, NilPanic},
		"pkg.Path()":            {true, NilPanic},
		"fmt.Sprint(s)":         {false, 0}, // can call String methods
		"bytes.ToUpper(b)":      {false, 0}, // reads memory others can change
		`strings.Join(xs, ",")`: {false, 0},
		"mine(s)":               {false, 0}, // not on the list
	}
	seen := 0
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		text := types.ExprString(call)
		w, ok := want[text]
		if !ok {
			return true
		}
		seen++
		known, panics := KnownCall(info, call)
		if known != w.known || known && panics != w.panics {
			t.Errorf("%s: known=%v panics=%v, want %v %v", text, known, panics, w.known, w.panics)
		}
		return true
	})
	if seen != len(want) {
		t.Errorf("checked %d calls, want %d", seen, len(want))
	}
}
