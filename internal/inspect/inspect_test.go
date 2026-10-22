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
		"18-33   func Classify(n int) string   [16 lines, complexity 6, nesting 3, no side effects]",
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
	if strings.Contains(out, "const Pi2") {
		t.Errorf("function view has the outline of the whole file:\n%s", out)
	}
	// A bare method name resolves when unambiguous.
	if out, err := Run(ws, Query{File: "shapes.go", Function: "Area"}); err != nil || !strings.Contains(out, "35  func (c Circle) Area()") {
		t.Errorf("method lookup failed: %v\n%s", err, out)
	}
}

// A file is shown with its function bodies elided: the declarations, doc
// comments and types in full, each function's metrics, and the lines to
// ask for to read a body.
func TestFileElidesBodies(t *testing.T) {
	ws := load(t)
	out, err := Run(ws, Query{File: "shapes.go"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"shapes.go  (package shapes, 35 lines)",
		" 6  type Shape interface{ Area() float64 }",
		"12  \tCircle struct{ R float64 }",
		"18  // Classify buckets n.\n19  func Classify(n int) string {\n        … lines 20-32   [16 lines, complexity 6, nesting 3, no side effects]\n33  }\n",
		"35  func (c Circle) Area() float64 { return math.Pi * c.R * c.R }\n        [1 line, complexity 1, nesting 0, no side effects]",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `return "neg"`) {
		t.Errorf("a body was shown:\n%s", out)
	}
}

// A range shows those lines only, without the outline of the file.
func TestFileRange(t *testing.T) {
	out, err := Run(load(t), Query{File: "shapes.go", StartLine: 6, EndLine: 6})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "6  type Shape interface") || strings.Contains(out, "func Classify") {
		t.Errorf("range view:\n%s", out)
	}
}

// Several targets are read in one call; a target that is refused says so
// in its place, and the others are still shown.
func TestTargets(t *testing.T) {
	ws := load(t)
	out, err := Run(ws, Query{Targets: []Query{
		{File: "shapes.go", Function: "Classify"},
		{File: "shapes.go", StartLine: 6, EndLine: 6},
		{File: "nope.go"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"source (lines 18-33):", "6  type Shape interface", "file nope.go is not part of the module"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	for _, q := range []Query{
		{File: "shapes.go", Targets: []Query{{File: "shapes.go"}}},
		{Targets: []Query{{Targets: []Query{{File: "shapes.go"}}}}},
	} {
		if _, err := Run(ws, q); !refactor.IsRefusal(err) {
			t.Errorf("%+v: want refusal, got %v", q, err)
		}
	}
}

func TestNames(t *testing.T) {
	t.Setenv("GOWORK", "off")
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/s\n\ngo 1.22\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "shapes.go"), []byte(shapes), 0o644)
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "sub", "sub.go"), []byte("package sub\n\n// Classify is another.\nfunc Classify(n int) int { return n }\n"), 0o644)
	ws, err := workspace.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Run(ws, Query{Names: []string{"Circle.Area", "Shape", "shapes.Classify", "sub.Classify", "Classify", "Nope"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"35  func (c Circle) Area() float64",
		"6  type Shape interface",
		"source (lines 18-33):",
		"sub/sub.go  (package sub",
		"Classify: declared in shapes.go (package shapes), sub/sub.go (package sub)",
		"Nope: no declaration of that name in the module",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if _, err := Run(ws, Query{File: "shapes.go", Names: []string{"Shape"}}); !refactor.IsRefusal(err) {
		t.Errorf("names with file: want refusal, got %v", err)
	}
}

func TestFileEffects(t *testing.T) {
	t.Setenv("GOWORK", "off")
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/e\n\ngo 1.22\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "e.go"), []byte(`package e

import "fmt"

var counter int

func Log(s string) { fmt.Println(s) }

func Twice(n int) int { return 2 * n }

func Count() int { return counter }

func Bump() { counter++ }

func LogTwice(s string) {
	Log(s)
	Log(s)
}
`), 0o644)
	ws, err := workspace.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Run(ws, Query{File: "e.go"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"func Log(s string) { fmt.Println(s) }\n        [1 line, complexity 1, nesting 0, side effects: calls fmt.Println]",
		"func Twice(n int) int { return 2 * n }\n        [1 line, complexity 1, nesting 0, no side effects]",
		"func Count() int { return counter }\n        [1 line, complexity 1, nesting 0, no side effects]",
		"func Bump() { counter++ }\n        [1 line, complexity 1, nesting 0, side effects: assigns to counter]",
		"func LogTwice(s string) {\n        … lines 16-17   [4 lines, complexity 1, nesting 0, side effects: calls Log, which calls fmt.Println]\n18  }",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("outline lacks %q:\n%s", want, out)
		}
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
	// One line per package: its files with their line counts.
	for _, want := range []string{"\n. (package shapes, ", " lines): shapes.go 35, shapes_test.go ", "1 packages, 2 files"} {
		if !strings.Contains(out, want) {
			t.Errorf("module overview lacks %q:\n%s", want, out)
		}
	}
}
