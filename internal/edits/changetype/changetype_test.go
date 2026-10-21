package changetype

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/edits/adddecl"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/replacestmts"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor/refactortest"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings/changesig"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

func TestGolden(t *testing.T) {
	refactortest.Run(t, New(), "testdata")
}

// In a batch checked at its end, change_type may leave errors, which its
// result lists, for the next step to fix.
func TestBatchFixesWhatBreaks(t *testing.T) {
	dir := t.TempDir()
	src := "package m\n\ntype renderArg = string\n\nfunc first(layout ...renderArg) string { return layout[0] }\n"
	for name, content := range map[string]string{"go.mod": "module example.com/m\n\ngo 1.22\n", "m.go": src} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ws, err := workspace.Load(dir, "./...")
	if err != nil {
		t.Fatal(err)
	}
	b, err := refactor.RunBatch(ws, []refactor.Step{
		{Refactoring: New(), Args: json.RawMessage(`{"file": "m.go", "name": "renderArg", "type": "any"}`)},
		{Refactoring: replacestmts.New(), Args: json.RawMessage(`{"file": "m.go", "function": "first", "content": "s, _ := layout[0].(string)\nreturn s"}`)},
	}, refactor.Options{Unchecked: true})
	if err != nil {
		t.Fatal(err)
	}
	if !b.Applied {
		t.Fatalf("batch refused: %s", b.Refused)
	}
	if notes := strings.Join(b.Steps[0].Notes, "\n"); !strings.Contains(notes, "m.go:5:") || !strings.Contains(notes, "cannot use") {
		t.Errorf("change_type's result does not list the error it left:\n%s", notes)
	}
}

// The way a parameter's type changes by hand, through the tools in one
// batch: an alias of the old type, the parameter written with it, the
// alias changed, and the body fixed.
func TestAliasWorkflow(t *testing.T) {
	dir := t.TempDir()
	src := `package m

type Renderer interface {
	Render(ctx int, layout ...string) string
}

type page struct{}

func (p page) Render(ctx int, layout ...string) string { return layout[0] }

func use(r Renderer) string { return r.Render(1, "a", "b") }
`
	for name, content := range map[string]string{"go.mod": "module example.com/m\n\ngo 1.22\n", "m.go": src} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ws, err := workspace.Load(dir, "./...")
	if err != nil {
		t.Fatal(err)
	}
	b, err := refactor.RunBatch(ws, []refactor.Step{
		{Refactoring: adddecl.New(), Args: json.RawMessage(`{"file": "m.go", "before": "Renderer", "content": "type renderArg = string"}`)},
		{Refactoring: changesig.NewChangeParameterType(), Args: json.RawMessage(`{"file": "m.go", "name": "Renderer.Render", "parameter": "layout", "type": "renderArg"}`)},
		{Refactoring: New(), Args: json.RawMessage(`{"file": "m.go", "name": "renderArg", "type": "any"}`)},
		{Refactoring: replacestmts.New(), Args: json.RawMessage(`{"file": "m.go", "function": "page.Render", "content": "s, _ := layout[0].(string)\nreturn s"}`)},
	}, refactor.Options{Unchecked: true})
	if err != nil {
		t.Fatal(err)
	}
	if !b.Applied {
		t.Fatalf("batch refused: %s", b.Refused)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "m.go"))
	for _, want := range []string{"type renderArg = any", "Render(ctx int, layout ...renderArg) string\n}", "func (p page) Render(ctx int, layout ...renderArg) string", `r.Render(1, "a", "b")`} {
		if !strings.Contains(string(got), want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}
