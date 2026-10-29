package replacestmts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/edits/fiximports"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor/refactortest"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

func TestGolden(t *testing.T) {
	refactortest.Run(t, New(), "testdata")
}

// In a batch checked at its end, an import a step leaves unused stays
// until the end, so a later step can still use it (here by an alias no
// search would find); one still unused at the end is removed then.
func TestBatchKeepsImportsToTheEnd(t *testing.T) {
	t.Setenv("GOWORK", "off")
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":       "module example.com/m\n\ngo 1.22\n",
		"wasm/wasm.go": "package wasm\n\nfunc Op() string { return \"op\" }\n",
		"m.go":         "package m\n\nimport (\n\t\"strings\"\n\n\tw \"example.com/m/wasm\"\n)\n\nfunc A(s string) string {\n\treturn w.Op() + strings.ToUpper(s)\n}\n\nfunc B(s string) string {\n\treturn s\n}\n",
	}
	for name, src := range files {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755)
		os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644)
	}
	ws, err := workspace.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	steps := []refactor.Step{
		{Refactoring: New(), Args: json.RawMessage(`{"file": "m.go", "function": "A", "content": "return s"}`)},
		{Refactoring: New(), Args: json.RawMessage(`{"file": "m.go", "function": "B", "content": "return w.Op()"}`)},
	}
	b, err := refactor.RunBatch(ws, steps, refactor.Options{Unchecked: true, Imports: fiximports.New()})
	if err != nil {
		t.Fatal(err)
	}
	if !b.Applied {
		t.Fatalf("refused: %s", b.Refused)
	}
	want := "package m\n\nimport (\n\tw \"example.com/m/wasm\"\n)\n\nfunc A(s string) string {\n\treturn s\n}\n\nfunc B(s string) string {\n\treturn w.Op()\n}\n"
	if got, _ := os.ReadFile(filepath.Join(dir, "m.go")); string(got) != want {
		t.Errorf("m.go:\n%s\nwant:\n%s", got, want)
	}
}
