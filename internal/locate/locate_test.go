package locate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

func loadModule(t *testing.T, files map[string]string) *workspace.Workspace {
	t.Helper()
	t.Setenv("GOWORK", "off")
	dir := t.TempDir()
	for name, src := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ws, err := workspace.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

var symbolModule = map[string]string{
	"go.mod": "module example.com/demo\n\ngo 1.22\n",
	"a.go": `package demo

const Limit = 10

type Counter struct {
	n int
}

func Total(cs []Counter) int {
	sum := 0
	for _, c := range cs {
		sum += c.n
	}
	return sum
}
`,
	"b.go": `package demo

func (c *Counter) Inc() { c.n++ }

type Shape interface {
	Area() float64
}
`,
}

// Without a line, name is a package-level symbol of file's package:
// "Name", or "Type.Method" / "Type.Field".
func TestIdentBySymbol(t *testing.T) {
	ws := loadModule(t, symbolModule)
	for _, tc := range []struct {
		name     string
		wantFile string
		wantLine int
		wantName string
	}{
		{"Total", "a.go", 9, "Total"},
		{"Counter", "a.go", 5, "Counter"},
		{"Limit", "a.go", 3, "Limit"},
		{"Counter.n", "a.go", 6, "n"},
		{"Counter.Inc", "b.go", 3, "Inc"},
		{"Shape.Area", "b.go", 6, "Area"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, id, err := locate.Ident(ws, locate.IdentQuery{File: "a.go", Name: tc.name})
			if err != nil {
				t.Fatal(err)
			}
			pos := ws.Fset.Position(id.Pos())
			if id.Name != tc.wantName || filepath.Base(pos.Filename) != tc.wantFile || pos.Line != tc.wantLine {
				t.Errorf("got %s at %s:%d, want %s at %s:%d", id.Name, filepath.Base(pos.Filename), pos.Line, tc.wantName, tc.wantFile, tc.wantLine)
			}
		})
	}
}

func TestIdentBySymbolRefusals(t *testing.T) {
	ws := loadModule(t, symbolModule)
	for _, tc := range []struct{ name, want string }{
		{"Nope", `no package-level "Nope"`},
		{"sum", `no package-level "sum"`},
		{"Counter.Dec", `Counter has no field or method "Dec"`},
		{"Total.x", "Total is not a type"},
		{"Nope.x", `no package-level "Nope"`},
		{"a.b.c", `"a.b.c"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := locate.Ident(ws, locate.IdentQuery{File: "a.go", Name: tc.name})
			if err == nil || !refactor.IsRefusal(err) {
				t.Fatalf("want refusal, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("refusal %q does not contain %q", err, tc.want)
			}
		})
	}
}

func TestIdentByFunction(t *testing.T) {
	ws := loadModule(t, map[string]string{
		"go.mod": "module example.com/demo\n\ngo 1.22\n",
		"a.go": `package demo

func Total(xs []int) int {
	sum := 0
	for _, x := range xs {
		sum += x
	}
	return sum
}

func Twice(n int) int {
	if n > 0 {
		v := n * 2
		return v
	}
	v := 0
	return v
}
`,
	})
	for _, c := range []struct {
		function, name string
		line           int    // of the declaration found
		refused        string // or the refusal
	}{
		{"Total", "sum", 4, ""},
		{"Total", "xs", 3, ""},
		{"Total", "x", 5, ""},
		{"Twice", "v", 0, "v is declared 2 times in Twice (lines 13, 16); give the line"},
		{"Total", "missing", 0, "Total declares no missing"},
		{"Nope", "x", 0, "no function Nope in a.go"},
	} {
		_, id, err := locate.Ident(ws, locate.IdentQuery{File: "a.go", Function: c.function, Name: c.name})
		if c.refused != "" {
			if err == nil || !strings.Contains(err.Error(), c.refused) {
				t.Errorf("%s.%s: got %v, want a refusal containing %q", c.function, c.name, err, c.refused)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s.%s: %v", c.function, c.name, err)
			continue
		}
		if got := ws.Fset.Position(id.Pos()).Line; id.Name != c.name || got != c.line {
			t.Errorf("%s.%s: found %s on line %d, want line %d", c.function, c.name, id.Name, got, c.line)
		}
	}
}
