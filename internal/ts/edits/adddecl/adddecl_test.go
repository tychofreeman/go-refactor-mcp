package adddecl

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

func read(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func run(t *testing.T, ws *workspace.Workspace, args string) *refactor.Result {
	t.Helper()
	res, err := refactor.Run(ws, New(), json.RawMessage(args), refactor.OptionsOf[*workspace.Workspace]{})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestAddDeclaration(t *testing.T) {
	ws, dir := solution(t)
	res := run(t, ws, `{"file": "src/plant.ts", "content": "/** mist sprays a little water. */\nexport function mist(): string {\n  return water(0.1);\n}", "after": "water"}`)
	if !res.Applied {
		t.Fatalf("not added after water: %s", res)
	}
	want := "export function water(liters: number): string {\n  return `${liters} L`;\n}\n\n/** mist sprays a little water. */\nexport function mist(): string {\n  return water(0.1);\n}\n"
	if got := read(t, dir, "src/plant.ts"); got != want {
		t.Fatalf("plant.ts:\n%s\nwant:\n%s", got, want)
	}
	res = run(t, ws, `{"file": "src/plant.ts", "content": "export const unit = \"L\";", "before": "water"}`)
	if !res.Applied || !strings.HasPrefix(read(t, dir, "src/plant.ts"), "export const unit = \"L\";\n\nexport function water") {
		t.Fatalf("not added before water: %s\n%s", res, read(t, dir, "src/plant.ts"))
	}
	res = run(t, ws, `{"file": "src/bed.ts", "content": "export const evening = water(4);"}`)
	if !res.Applied || !strings.HasSuffix(read(t, dir, "src/bed.ts"), "export const morning = water(2);\n\nexport const evening = water(4);\n") {
		t.Fatalf("not added at the end: %s\n%s", res, read(t, dir, "src/bed.ts"))
	}
	for _, tc := range []struct{ args, want string }{
		{`{"file": "src/bed.ts", "content": "export const x = 1;", "after": "dusk"}`, `declares no "dusk"`},
		{`{"file": "src/bed.ts", "content": "export const y: string = water;"}`, "is not assignable to type 'string'"},
		{`{"file": "src/none.ts", "content": "export const z = 1;"}`, "use create_file"},
	} {
		if res := run(t, ws, tc.args); res.Applied || !strings.Contains(res.Refused, tc.want) {
			t.Errorf("%s: want a refusal with %q, got %s", tc.args, tc.want, res)
		}
	}
}

// Test first: a batch checked at its end adds a test calling what nothing
// declares yet, then declares it; without the declaration it is refused.
func TestTestFirstInABatch(t *testing.T) {
	ws, dir := solution(t)
	step := func(file, content string) refactor.StepOf[*workspace.Workspace] {
		args, _ := json.Marshal(map[string]string{"file": file, "content": content})
		return refactor.StepOf[*workspace.Workspace]{Refactoring: New(), Args: args}
	}
	test := step("src/plant.test.ts", "export const shaded: number = shade(10);")
	impl := step("src/plant.test.ts", "function shade(sun: number): number {\n  return sun / 2;\n}")
	b, err := refactor.RunBatch(ws, []refactor.StepOf[*workspace.Workspace]{test}, refactor.OptionsOf[*workspace.Workspace]{Unchecked: true})
	if err != nil {
		t.Fatal(err)
	}
	if b.Applied || !strings.Contains(b.Refused, "Cannot find name 'shade'") {
		t.Fatalf("a test calling what nothing declares was applied: %s", b)
	}
	b, err = refactor.RunBatch(ws, []refactor.StepOf[*workspace.Workspace]{test, impl}, refactor.OptionsOf[*workspace.Workspace]{Unchecked: true})
	if err != nil {
		t.Fatal(err)
	}
	if !b.Applied || !strings.Contains(read(t, dir, "src/plant.test.ts"), "return sun / 2;") {
		t.Fatalf("the test with its function was not applied: %s", b)
	}
}
