package rename

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor/refactortest"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

func TestGolden(t *testing.T) {
	refactortest.Run(t, New(), "testdata")
}

// Pointing the tool at a package subdirectory must still update references
// in sibling packages of the same module (regression: they were not loaded).
func TestRenameFromSubdirUpdatesWholeModule(t *testing.T) {
	t.Setenv("GOWORK", "off")
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":       "module example.com/demo\n\ngo 1.22\n",
		"calc/calc.go": "package calc\n\nvar Rate = 3\n",
		"app/app.go":   "package app\n\nimport \"example.com/demo/calc\"\n\nfunc Run() int { return calc.Rate }\n",
	}
	for name, src := range files {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(src), 0o644)
	}

	ws, err := workspace.Load(filepath.Join(dir, "calc"))
	if err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{"file": "calc.go", "line": 3, "name": "Rate", "new_name": "Factor"})
	res, err := refactor.Run(ws, New(), args, refactor.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Applied {
		t.Fatalf("not applied: %s", res)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "app", "app.go"))
	if !strings.Contains(string(got), "calc.Factor") {
		t.Errorf("app/app.go was not updated:\n%s", got)
	}
}
