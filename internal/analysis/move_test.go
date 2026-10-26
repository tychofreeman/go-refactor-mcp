package analysis

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"
)

const moveSrc = `package p

type T struct{ n int }

var g int

func log(s string) int   { println(s); return 0 }
func read(p *T) int      { return p.n }
func pure(a int) int     { return a + 1 }
func risky(a, b int) int { return a / b }

func f(p *T, a, b int, xs []int) {
	var r int
	_ = a + 1       // MOVE value past: log("x")
	_ = p.n         // MOVE pointer past: log("x")
	_ = p.n         // MOVE pointer past: a = 2
	_ = p.n         // MOVE pointer past: read(p)
	_ = a / b       // MOVE panic past: log("x")
	_ = a / b       // MOVE panic past: pure(a)
	_ = a / b       // MOVE panic past: xs[0]
	_ = &p.n        // MOVE nil past: log("x")
	_ = &p.n        // MOVE nil past: xs[0]
	_ = log("y")    // MOVE effect past: pure(a)
	_ = log("y")    // MOVE effect past: xs[0]
	_ = a + r       // MOVE local past: r = 3
	_ = a + r       // MOVE local past: a = 3
	_ = a + 1       // MOVE value past: return
	_ = a / b       // MOVE panic past: return
	_ = a / b       // TIMES several: a / b
	_ = a + 1       // TIMES several: a + 1
	_ = &T{}        // TIMES shared: &T{}
	_ = &T{}        // TIMES several: &T{}
	_ = p.n         // TIMES several: p.n
	_ = a / b       // TIMES maybe: a / b
	_ = p.n         // TIMES maybe: p.n
	log("x")
	a = 2
	a = 3
	r = 3
	_ = read(p)
	_ = pure(a)
	_ = xs[0]
	return
}
`

// TestMove checks which moves of an expression keep behavior. Each MOVE
// line moves its expression past the statement or expression named after
// the colon, which appears later in f; each TIMES line changes how often
// its expression is evaluated.
func TestMove(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p.go", moveSrc, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	info := newMoveInfo()
	if _, err := (&types.Config{}).Check("example.com/p", fset, []*ast.File{file}, info); err != nil {
		t.Fatal(err)
	}
	fd := file.Decls[len(file.Decls)-1].(*ast.FuncDecl)
	f := AnalyzeFunc(info, fd)
	e := NewEffects()
	e.AddPackage([]*ast.File{file}, info)
	// what each statement of f is, by its text, for the "past" nodes.
	byText := map[string]ast.Node{}
	for _, s := range fd.Body.List {
		text := exprText(s)
		if _, dup := byText[text]; !dup {
			byText[text] = s
		}
	}
	want := map[string]string{ // the comment -> the obstacle's text, "" for none
		"value past: log(\"x\")":   "",
		"pointer past: log(\"x\")": "p.n", // log could change what p points to
		"pointer past: a = 2":      "",
		"pointer past: read(p)":    "", // read only reads
		"panic past: log(\"x\")":   "a / b",
		"panic past: pure(a)":      "",
		"panic past: xs[0]":        "a / b",
		"nil past: log(\"x\")":     "", // a nil dereference may move past a call that returns
		"nil past: xs[0]":          "p.n",
		"effect past: pure(a)":     "", // pure does nothing another evaluation could notice
		"effect past: xs[0]":       "log(\"y\")",
		"local past: r = 3":        "r",
		"local past: a = 3":        "a",
		"value past: return":       "",
		"panic past: return":       "a / b",
		"several: a / b":           "a / b",
		"several: a + 1":           "",
		"shared: &T{}":             "&T{}",
		"several: &T{}":            "",
		"several: p.n":             "p.n",
		"maybe: a / b":             "a / b",
		"maybe: p.n":               "p.n", // a panic that would not always happen
	}
	seen := 0
	for _, cg := range file.Comments {
		c := cg.List[0].Text
		kind, rest, ok := strings.Cut(strings.TrimPrefix(c, "// "), " ")
		if !ok || (kind != "MOVE" && kind != "TIMES") {
			continue
		}
		w, known := want[rest]
		if !known {
			t.Errorf("no expectation for %q", rest)
			continue
		}
		seen++
		var x ast.Expr
		for _, s := range fd.Body.List {
			if as, ok := s.(*ast.AssignStmt); ok && fset.Position(s.Pos()).Line == fset.Position(cg.Pos()).Line {
				x = as.Rhs[0]
			}
		}
		m := Move{Effects: e, Func: f, Expr: x}
		_, past, _ := strings.Cut(rest, ": ")
		if kind == "MOVE" {
			n, ok := byText[past]
			if !ok {
				t.Fatalf("%s: no statement %q", rest, past)
			}
			m.Past = []ast.Node{n}
		} else {
			m.Times = map[string]Times{"several": Several, "shared": Shared, "maybe": Maybe}[strings.Fields(rest)[0][:len(strings.Fields(rest)[0])-1]]
		}
		got := ""
		if o := m.Check(); o != nil {
			got = exprText(o.In)
			if o.Why == "" {
				t.Errorf("%s: obstacle without a reason", rest)
			}
		}
		if got != w {
			t.Errorf("%s: obstacle %q, want %q", rest, got, w)
		}
	}
	if seen != len(want) {
		t.Errorf("checked %d moves, want %d", seen, len(want))
	}
}

func newMoveInfo() *types.Info {
	return &types.Info{
		Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{},
		Types: map[ast.Expr]types.TypeAndValue{}, Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
}

// exprText is the source-like text of a statement or expression.
func exprText(n ast.Node) string {
	switch n := n.(type) {
	case ast.Expr:
		return types.ExprString(n)
	case *ast.ExprStmt:
		return types.ExprString(n.X)
	case *ast.AssignStmt:
		if len(n.Lhs) == 1 && len(n.Rhs) == 1 && types.ExprString(n.Lhs[0]) == "_" {
			return types.ExprString(n.Rhs[0])
		}
		if len(n.Lhs) == 1 && len(n.Rhs) == 1 {
			return types.ExprString(n.Lhs[0]) + " " + n.Tok.String() + " " + types.ExprString(n.Rhs[0])
		}
	case *ast.ReturnStmt:
		return "return"
	}
	return "?"
}
