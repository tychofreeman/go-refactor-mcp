package rename

import (
	"fmt"
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

func run(t *testing.T, ws *workspace.Workspace, args string) *refactor.Result {
	t.Helper()
	res := tstest.Run(t, ws, New(), args)
	return res
}

func read(t *testing.T, dir, rel string) string {
	t.Helper()
	return tstest.Read(t, dir, rel)
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

// drip is a file with a class's members and locals of the same name in
// two functions.
const drip = `export class Drip {
  rate = 1;
  run(liters: number): number {
    const left = liters - this.rate;
    return left;
  }
}

export function soak(liters: number): number {
  const left = liters;
  if (left > 1) {
    const left = 1;
    return left;
  }
  return left;
}
`

// withDrip is the solution with src/drip.ts.
func withDrip(t *testing.T) (*workspace.Workspace, string) {
	t.Helper()
	return withFile(t, "src/drip.ts", drip)
}

// withFile is the solution with one more file.
func withFile(t *testing.T, name, src string) (*workspace.Workspace, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("../../workspace/testdata/solution")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return ws, dir
}

// Without line, Class.member names a member; with function, name is a
// local of that function, not of another.
func TestRenameMemberAndLocal(t *testing.T) {
	for _, tc := range []struct {
		args  string
		wants []string
	}{
		{`{"file": "src/drip.ts", "name": "Drip.run", "new_name": "flow"}`, []string{"  flow(liters: number): number {\n"}},
		{`{"file": "src/drip.ts", "name": "Drip.rate", "new_name": "speed"}`, []string{"  speed = 1;\n", "liters - this.speed;\n"}},
		{`{"file": "src/drip.ts", "function": "Drip.run", "name": "left", "new_name": "rest"}`, []string{"    const rest = liters - this.rate;\n    return rest;\n", "export function soak(liters: number): number {\n  const left = liters;\n"}},
		{`{"file": "src/drip.ts", "function": "Drip.run", "name": "liters", "new_name": "amount"}`, []string{"  run(amount: number): number {\n    const left = amount - this.rate;\n", "soak(liters: number)"}},
	} {
		ws, dir := withDrip(t)
		res := run(t, ws, tc.args)
		if !res.Applied {
			t.Errorf("%s: not applied: %s", tc.args, res)
			continue
		}
		got := read(t, dir, "src/drip.ts")
		for _, want := range tc.wants {
			if !strings.Contains(got, want) {
				t.Errorf("%s: drip.ts:\n%s\nwant it to hold:\n%s", tc.args, got, want)
			}
		}
	}
	for _, tc := range []struct{ args, want string }{
		{`{"file": "src/drip.ts", "name": "Drip.pump", "new_name": "push"}`, `src/drip.ts declares no top-level "Drip.pump"`},
		{`{"file": "src/drip.ts", "function": "soak", "name": "rate", "new_name": "speed"}`, "soak declares no rate"},
		{`{"file": "src/drip.ts", "function": "soak", "name": "left", "new_name": "rest"}`, "soak declares left 2 times, on lines 10, 12: give line"},
	} {
		ws, _ := withDrip(t)
		if res := run(t, ws, tc.args); res.Applied || !strings.Contains(res.Refused, tc.want) {
			t.Errorf("%s: want a refusal with %q, got %s", tc.args, tc.want, res)
		}
	}
}

// plants is a file whose Plant values reach code that finds properties by
// name (passed as uses), and console.log.
const plants = `declare const console: { log(...a: unknown[]): void };

export interface Plant {
  name: string;
  height: number;
}

export const garden: Plant[] = [{ name: "rose", height: 2 }];
%s
`

// A member renamed where a value holding its type reaches code that finds
// properties by name is refused; where console prints one, the rename
// goes ahead with a note.
func TestRenameKeyedMember(t *testing.T) {
	for _, tc := range []struct{ uses, args, want string }{
		{"export const saved = JSON.stringify(garden);", `{"file": "src/plants.ts", "name": "Plant.height", "new_name": "size"}`, "a value holding Plant is passed to JSON.stringify at plants.ts:9, which finds properties by name"},
		{"export const keys = Object.keys(garden[0]);", `{"file": "src/plants.ts", "name": "Plant.name", "new_name": "title"}`, "passed to Object.keys at plants.ts:9"},
		{"export function tall(x: Plant | string): boolean {\n  return typeof x !== \"string\" && \"height\" in x;\n}", `{"file": "src/plants.ts", "name": "Plant.height", "new_name": "size"}`, `"height" in tests a value of Plant for the property by name at plants.ts:10`},
		{"export function each(p: Plant): void {\n  for (const k in p) {\n    console.log(k);\n  }\n}", `{"file": "src/plants.ts", "name": "Plant.name", "new_name": "title"}`, "looped over with for...in at plants.ts:10"},
	} {
		ws, _ := withFile(t, "src/plants.ts", fmt.Sprintf(plants, tc.uses))
		if res := run(t, ws, tc.args); res.Applied || !strings.Contains(res.Refused, tc.want) {
			t.Errorf("%s: want a refusal with %q, got %s", tc.uses, tc.want, res)
		}
	}
	ws, _ := withFile(t, "src/plants.ts", fmt.Sprintf(plants, "export function show(p: Plant): void {\n  console.log(p);\n}"))
	res := run(t, ws, `{"file": "src/plants.ts", "name": "Plant.name", "new_name": "title"}`)
	if !res.Applied || !strings.Contains(strings.Join(res.Notes, "\n"), "what these calls print changes, since they print property names: plants.ts:10 console.log") {
		t.Errorf("want a note on console.log, got %s", res)
	}
	ws, _ = withFile(t, "src/plants.ts", fmt.Sprintf(plants, "export const n = garden.length;"))
	if res := run(t, ws, `{"file": "src/plants.ts", "name": "Plant.height", "new_name": "size"}`); !res.Applied {
		t.Errorf("a member no keyed code reaches: %s", res)
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
