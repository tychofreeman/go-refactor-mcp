package createfile

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

// overwrite replaces a file that exists.
func TestOverwrite(t *testing.T) {
	ws, dir := solution(t)
	res := run(t, ws, `{"file": "src/plant.ts", "content": "export const water = (liters: number): string => `+"`"+`${liters} L`+"`"+`;\n", "overwrite": true}`)
	if !res.Applied || !strings.Contains(read(t, dir, "src/plant.ts"), "export const water") {
		t.Fatalf("not overwritten: %s", res)
	}
	if res := run(t, ws, `{"file": "src/plant.ts", "content": "x", "overwrite": false}`); res.Applied || !strings.Contains(res.Refused, "overwrite") {
		t.Errorf("want a refusal naming overwrite, got %s", res)
	}
}

func TestCreateFile(t *testing.T) {
	ws, dir := solution(t)
	res := run(t, ws, `{"file": "src/soil.ts", "content": "export const ph = 6.5;"}`)
	if !res.Applied || read(t, dir, "src/soil.ts") != "export const ph = 6.5;\n" {
		t.Fatalf("not created: %s", res)
	}
	// The new file joined the projects: an edit that uses it type-checks.
	res = run(t, ws, `{"file": "src/compost.ts", "content": "import { ph } from \"./soil\";\nexport const acid: boolean = ph < 7;"}`)
	if !res.Applied {
		t.Fatalf("a file importing the new one was refused: %s", res)
	}
	for _, tc := range []struct{ args, want string }{
		{`{"file": "src/bed.ts", "content": "x"}`, "already exists"},
		{`{"file": "src/broken.ts", "content": "export const n: number = \"two\";"}`, "src/broken.ts:1:14: Type 'string' is not assignable to type 'number'."},
		{`{"file": "../outside.ts", "content": ""}`, "outside the root"},
	} {
		if res := run(t, ws, tc.args); res.Applied || !strings.Contains(res.Refused, tc.want) {
			t.Errorf("%s: want a refusal with %q, got %s", tc.args, tc.want, res)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "src/broken.ts")); err == nil {
		t.Error("a refused file was left behind")
	}
}
