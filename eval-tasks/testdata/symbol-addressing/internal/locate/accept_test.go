package locate_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

func acceptLoadModule(t *testing.T, files map[string]string) *workspace.Workspace {
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

var acceptSymbolModule = map[string]string{
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
func TestAcceptIdentBySymbol(t *testing.T) {
	ws := acceptLoadModule(t, acceptSymbolModule)
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

func TestAcceptIdentBySymbolRefusals(t *testing.T) {
	ws := acceptLoadModule(t, acceptSymbolModule)
	for _, tc := range []struct{ name, _ string }{
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
		})
	}
}
