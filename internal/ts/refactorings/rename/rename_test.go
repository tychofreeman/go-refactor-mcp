package rename

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
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

func run(t *testing.T, ws *workspace.Workspace, args string) *refactor.Result {
	t.Helper()
	res, err := refactor.Run(ws, New(), json.RawMessage(args), refactor.OptionsOf[*workspace.Workspace]{})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func read(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Renaming a function renames its declaration, imports and calls in every
// project, the test that only the spec project has included.
func TestRenameAcrossProjects(t *testing.T) {
	ws, dir := solution(t)
	res := run(t, ws, `{"file": "src/plant.ts", "line": 1, "name": "water", "new_name": "irrigate"}`)
	if !res.Applied {
		t.Fatalf("not applied: %s", res)
	}
	if !strings.Contains(res.Summary, `renamed "water" to "irrigate" (5 occurrences in 3 files)`) {
		t.Errorf("summary: %s", res.Summary)
	}
	for _, f := range []string{"src/plant.ts", "src/bed.ts", "src/plant.test.ts"} {
		if src := read(t, dir, f); strings.Contains(src, "water") || !strings.Contains(src, "irrigate") {
			t.Errorf("%s not renamed:\n%s", f, src)
		}
	}
}

// Without line, name is a top-level declaration of file.
func TestRenameByName(t *testing.T) {
	ws, dir := solution(t)
	res := run(t, ws, `{"file": "src/bed.ts", "name": "morning", "new_name": "dawn"}`)
	if !res.Applied || !strings.Contains(read(t, dir, "src/bed.ts"), "export const dawn = water(2);") {
		t.Fatalf("not renamed by name: %s", res)
	}
}

func TestRenameRefuses(t *testing.T) {
	for _, tc := range []struct{ args, want string }{
		{`{"file": "src/bed.ts", "name": "morning", "new_name": "water"}`, `"water" is already used in src/bed.ts:1`},
		{`{"file": "src/bed.ts", "name": "morning", "new_name": "2am"}`, `"2am" is not a valid name`},
		{`{"file": "src/bed.ts", "name": "morning", "new_name": "class"}`, `"class" is a reserved word`},
		{`{"file": "src/bed.ts", "line": 1, "name": "morning", "new_name": "dawn"}`, `no "morning" on line 1 of src/bed.ts`},
		{`{"file": "src/bed.ts", "name": "evening", "new_name": "dusk"}`, `src/bed.ts declares no top-level "evening"`},
	} {
		ws, dir := solution(t)
		before := read(t, dir, "src/bed.ts")
		res := run(t, ws, tc.args)
		if res.Applied || !strings.Contains(res.Refused, tc.want) {
			t.Errorf("%s: want a refusal with %q, got %s", tc.args, tc.want, res)
		}
		if read(t, dir, "src/bed.ts") != before {
			t.Errorf("%s: a refusal changed the file", tc.args)
		}
	}
}
