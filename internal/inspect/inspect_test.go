package inspect

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

const shapes = `package shapes

import "math"

// Shape has an area.
type Shape interface{ Area() float64 }

type Box[T any] struct{ v T }

type (
	// Circle is round.
	Circle struct{ R float64 }
	ID     = int
)

const Pi2 = math.Pi * 2

// Classify buckets n.
func Classify(n int) string {
	if n < 0 {
		return "neg"
	}
	for i := 0; i < n; i++ {
		if i > 10 && n%2 == 0 {
			switch {
			case i > 20:
				return "big"
			default:
			}
		}
	}
	return "small"
}

func (c Circle) Area() float64 { return math.Pi * c.R * c.R }
`

func load(t *testing.T) *workspace.Workspace {
	t.Helper()
	t.Setenv("GOWORK", "off")
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/s\n\ngo 1.22\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "shapes.go"), []byte(shapes), 0o644)
	os.WriteFile(filepath.Join(dir, "shapes_test.go"), []byte("package shapes_test\n"), 0o644)
	ws, err := workspace.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestOutline(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "shapes.go", shapes, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range Outline(fset, f) {
		got = append(got, d.Signature)
	}
	want := []string{
		"type Shape interface",
		"type Box[T any] struct",
		"type Circle struct",
		"type ID = int",
		"const Pi2",
		"func Classify(n int) string",
		"func (c Circle) Area() float64",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("outline:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestMetrics(t *testing.T) {
	fset := token.NewFileSet()
	f, _ := parser.ParseFile(fset, "shapes.go", shapes, 0)
	var classify *ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == "Classify" {
			classify = fd
		}
	}
	c, n := Metrics(classify.Body)
	// 1 + if + for + if + && + case(non-default) = 6; for>if>switch = 3.
	if c != 6 || n != 3 {
		t.Errorf("Metrics = complexity %d, nesting %d; want 6, 3", c, n)
	}
}

func TestFileFunction(t *testing.T) {
	ws := load(t)
	out, err := Run(ws, Query{File: "shapes.go", Function: "Classify"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"shapes.go  (package shapes, 35 lines)",
		"18-33   func Classify(n int) string   [16 lines, complexity 6, nesting 3]",
		"source (lines 18-33):",
		"18  // Classify buckets n.",
		"33  }",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "35  func (c Circle)") {
		t.Error("function view leaked past the function")
	}
	// A bare method name resolves when unambiguous.
	if out, err := Run(ws, Query{File: "shapes.go", Function: "Area"}); err != nil || !strings.Contains(out, "35  func (c Circle) Area()") {
		t.Errorf("method lookup failed: %v\n%s", err, out)
	}
}

func TestFileErrors(t *testing.T) {
	ws := load(t)
	for _, q := range []Query{
		{File: "nope.go"},
		{File: "shapes.go", Function: "Missing"},
		{File: "shapes.go", StartLine: 50},
		{Function: "Classify"},
	} {
		if _, err := Run(ws, q); !refactor.IsRefusal(err) {
			t.Errorf("%+v: want refusal, got %v", q, err)
		}
	}
}

func TestModule(t *testing.T) {
	out := Module(load(t))
	for _, want := range []string{"./  (package shapes)", "shapes.go", "shapes_test.go", "1 packages, 2 files"} {
		if !strings.Contains(out, want) {
			t.Errorf("module overview lacks %q:\n%s", want, out)
		}
	}
}
