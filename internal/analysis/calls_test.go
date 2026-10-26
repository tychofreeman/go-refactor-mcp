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
	"encoding/json"
	"errors"
	"fmt"
	"go/types"
	"path/filepath"
	"reflect"
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
	_ = reflect.TypeOf(s)
	_ = o.Type()
	_ = errors.New(s)
	_ = json.Valid(b)
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
		"reflect.TypeOf(s)":     {true, NoPanic},
		"o.Type()":              {true, NilPanic}, // fixed once type checking is done
		"errors.New(s)":         {false, 0},       // a new error each call
		"json.Valid(b)":         {false, 0},       // reads the slice
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

func TestReadOnlyCall(t *testing.T) {
	src := `package p

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
)

func f(s string, xs []string, b []byte) {
	_ = json.Valid(b)
	_ = bytes.Equal(b, b)
	_ = strings.Join(xs, ",")
	_ = os.Getenv(s)
	_ = strings.Repeat(s, 2)
	_ = errors.New(s)
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Types: map[ast.Expr]types.TypeAndValue{}}
	conf := types.Config{Importer: importer.Default()}
	if _, err := conf.Check("example.com/p", fset, []*ast.File{f}, info); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"json.Valid(b)":         true,
		"bytes.Equal(b, b)":     true,
		`strings.Join(xs, ",")`: false, // can panic when the result is too long
		"os.Getenv(s)":          true,  // reads the environment, which others can set, but changes nothing
		"errors.New(s)":         true,  // a new error each call, but changes nothing
		"strings.Repeat(s, 2)":  false, // can panic
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
		if got := ReadOnlyCall(info, call); got != w {
			t.Errorf("%s: read only = %v, want %v", text, got, w)
		}
		return true
	})
	if seen != len(want) {
		t.Errorf("checked %d calls, want %d", seen, len(want))
	}
}

func TestMayPanic(t *testing.T) {
	src := `package p

const k = "abc"

func f[N int | float64](a, b int, u uint, x, y float64, s string, arr [4]int, xs []int, p *[4]int, v any, n, m N) {
	_ = a / b
	_ = a % b
	_ = a / 2
	_ = x / y
	_ = n / m
	_ = a << b
	_ = a << u
	_ = a << 3
	_ = s[0]
	_ = k[1]
	_ = arr[a]
	_ = arr[1]
	_ = xs[0]
	_ = p[1]
	_ = s[1:]
	_ = s[:]
	_ = xs[:]
	_ = v.(int)
	_ = [2]int(xs)
	_ = (*[2]int)(xs)
	_ = []byte(s)
	_ = -a + len(s)
	_ = 1 + a/b
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Types: map[ast.Expr]types.TypeAndValue{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
	if _, err := (&types.Config{}).Check("example.com/p", fset, []*ast.File{f}, info); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{ // expression -> the part that can panic, "" for none
		"a / b": "a / b", "a % b": "a % b", "a / 2": "", "x / y": "", "n / m": "n / m",
		"a << b": "a << b", "a << u": "", "a << 3": "",
		"s[0]": "s[0]", "k[1]": "", "arr[a]": "arr[a]", "arr[1]": "", "xs[0]": "xs[0]", "p[1]": "p[1]",
		"s[1:]": "s[1:]", "s[:]": "", "xs[:]": "",
		"v.(int)": "v.(int)", "[2]int(xs)": "[2]int(xs)", "(*[2]int)(xs)": "(*[2]int)(xs)", "[]byte(s)": "",
		"-a + len(s)": "", "1 + a / b": "a / b",
	}
	seen := 0
	ast.Inspect(f, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		e := as.Rhs[0]
		w, ok := want[types.ExprString(e)]
		if !ok {
			t.Errorf("no expectation for %s", types.ExprString(e))
			return true
		}
		seen++
		got := ""
		if p := MayPanic(info, e); p != nil {
			got = types.ExprString(p.(ast.Expr))
		}
		if got != w {
			t.Errorf("MayPanic(%s) = %q, want %q", types.ExprString(e), got, w)
		}
		return true
	})
	if seen != len(want) {
		t.Errorf("checked %d expressions, want %d", seen, len(want))
	}
}
