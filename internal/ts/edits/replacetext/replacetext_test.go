package replacetext

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tstest"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

func solution(t *testing.T) (*workspace.Workspace, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("../../workspace/testdata/solution")); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return ws, dir
}

func read(t *testing.T, dir, rel string) string {
	t.Helper()
	return tstest.Read(t, dir, rel)
}

func run(t *testing.T, ws *workspace.Workspace, args string) *refactor.Result {
	t.Helper()
	res := tstest.Run(t, ws, New(), args)
	return res
}

func TestReplaceText(t *testing.T) {
	ws, dir := solution(t)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("Water: 2 L.\nWater again: 2 L.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := run(t, ws, `{"file": "README.md", "old": "2 L", "new": "3 L", "all_occurrences": true}`)
	if !res.Applied || read(t, dir, "README.md") != "Water: 3 L.\nWater again: 3 L.\n" {
		t.Fatalf("README not changed: %s", res)
	}
	res = run(t, ws, `{"files": ["src/bed.ts", "src/plant.test.ts"], "old": "import { water } from \"./plant\";", "new": "import { water } from \"./plant.ts\";"}`)
	if res.Applied {
		t.Fatalf("an import TypeScript cannot resolve here was applied: %s", res)
	}
	res = run(t, ws, `{"file": "src/plant.ts", "old": "${liters} L", "new": "${liters} litres"}`)
	if !res.Applied || !strings.Contains(read(t, dir, "src/plant.ts"), "litres") {
		t.Fatalf("a string in source not changed: %s", res)
	}
	for _, tc := range []struct{ args, want string }{
		{`{"file": "README.md", "old": "3 L", "new": "4 L"}`, "occurs 2 times"},
		{`{"file": "README.md", "old": "gallons", "new": "x"}`, "was not found"},
		{`{"file": "README.md", "old": "", "new": "x"}`, "old is empty"},
	} {
		if res := run(t, ws, tc.args); res.Applied || !strings.Contains(res.Refused, tc.want) {
			t.Errorf("%s: want a refusal with %q, got %s", tc.args, tc.want, res)
		}
	}
}

// append adds to the end, function limits the search to one function, and
// spacing within lines may differ from the file's.
func TestReplaceTextOptions(t *testing.T) {
	ws, dir := solution(t)
	if err := os.WriteFile(filepath.Join(dir, "NOTES.md"), []byte("- a"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := run(t, ws, `{"file": "NOTES.md", "append": true, "new": "- b\n"}`)
	if !res.Applied || read(t, dir, "NOTES.md") != "- a\n- b\n" {
		t.Fatalf("not appended: %s", res)
	}
	res = run(t, ws, `{"file": "src/plant.ts", "function": "water", "old": "return    `+"`"+`${liters} L`+"`"+`", "new": "return `+"`"+`${liters} litres`+"`"+`"}`)
	if !res.Applied || !strings.Contains(read(t, dir, "src/plant.ts"), "litres") {
		t.Fatalf("not replaced in water: %s", res)
	}
	if res := run(t, ws, `{"file": "src/plant.ts", "function": "prune", "old": "x", "new": "y"}`); res.Applied || !strings.Contains(res.Refused, "prune") {
		t.Errorf("want a refusal naming prune, got %s", res)
	}
}
