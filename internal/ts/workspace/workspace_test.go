package workspace

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
)

// solution copies testdata/solution, a root tsconfig.json that references
// a build project (sources, no tests) and a spec project (sources and
// tests), and loads it.
func solution(t *testing.T) (*Workspace, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("testdata/solution")); err != nil {
		t.Fatal(err)
	}
	ws, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return ws, dir
}

// replaceIn is an edit of the first occurrence of old in the file at
// rel, relative to the workspace root.
func replaceIn(t *testing.T, ws *Workspace, rel, old, new string) []edit.FileEdit {
	t.Helper()
	path := ws.Abs(rel)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	i := bytes.Index(src, []byte(old))
	if i < 0 {
		t.Fatalf("%q not in %s", old, rel)
	}
	return []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{Start: i, End: i + len(old), NewText: new}}}}
}

func TestLoadSolution(t *testing.T) {
	ws, _ := solution(t)
	if errs := ws.Errors(); len(errs) > 0 {
		t.Fatalf("the fixture has errors: %v", errs)
	}
	if got := ws.Projects(); strings.Join(got, " ") != "tsconfig.build.json tsconfig.spec.json" {
		t.Fatalf("projects = %v; want the two the root references", got)
	}
}

// A change is checked in every project: the build project has bed.ts,
// only the spec project has the test, and both call water.
func TestApplyChecksEveryProject(t *testing.T) {
	ws, dir := solution(t)
	before, _ := os.ReadFile(filepath.Join(dir, "src/plant.ts"))
	c, err := ws.Apply("add a parameter", replaceIn(t, ws, "src/plant.ts", "liters: number", "liters: number, unit: string"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.Diff, "+export function water(liters: number, unit: string): string {") {
		t.Fatalf("diff: %s", c.Diff)
	}
	errs := strings.Join(ws.Errors(), "\n")
	for _, want := range []string{"src/bed.ts:3:24: Expected 2 arguments, but got 1.", "src/plant.test.ts:3:25: Expected 2 arguments, but got 1."} {
		if !strings.Contains(errs, want) {
			t.Errorf("errors do not have %q:\n%s", want, errs)
		}
	}
	if strings.Count(errs, "src/bed.ts:") != 1 {
		t.Errorf("an error two projects report is listed twice:\n%s", errs)
	}

	if _, err := ws.Undo(); err != nil {
		t.Fatal(err)
	}
	if errs := ws.Errors(); len(errs) > 0 {
		t.Fatalf("errors after undo: %v", errs)
	}
	if after, _ := os.ReadFile(filepath.Join(dir, "src/plant.ts")); !bytes.Equal(after, before) {
		t.Fatalf("undo did not restore plant.ts:\n%s", after)
	}
}

func TestPreviewWritesNothing(t *testing.T) {
	ws, dir := solution(t)
	before, _ := os.ReadFile(filepath.Join(dir, "src/bed.ts"))
	c, err := ws.Preview("rename", replaceIn(t, ws, "src/bed.ts", "morning", "dawn"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.Diff, "+export const dawn = water(2);") {
		t.Fatalf("diff: %s", c.Diff)
	}
	if after, _ := os.ReadFile(filepath.Join(dir, "src/bed.ts")); !bytes.Equal(after, before) {
		t.Fatal("preview wrote the file")
	}
}

// A file created or edited on disk between calls is seen after Refresh.
func TestRefreshSeesFilesOnDisk(t *testing.T) {
	ws, dir := solution(t)
	if err := os.WriteFile(filepath.Join(dir, "src/soil.ts"), []byte("export const ph: number = \"acid\";\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Refresh(); err != nil {
		t.Fatal(err)
	}
	if errs := strings.Join(ws.Errors(), "\n"); !strings.Contains(errs, "src/soil.ts:1:14:") {
		t.Fatalf("a new file's error is not seen: %q", errs)
	}
	if err := os.WriteFile(filepath.Join(dir, "src/soil.ts"), []byte("export const ph: number = 6;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Refresh(); err != nil {
		t.Fatal(err)
	}
	if errs := ws.Errors(); len(errs) > 0 {
		t.Fatalf("an edit on disk is not seen: %v", errs)
	}
}

// replaceWord replaces the first occurrence of old in file with new.
type replaceWord struct{}

func (replaceWord) Name() string        { return "replace_word" }
func (replaceWord) Description() string { return "" }
func (replaceWord) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file":{"type":"string"},"old":{"type":"string"},"new":{"type":"string"}},"required":["file","old","new"]}`)
}
func (replaceWord) Plan(ws *Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct{ File, Old, New string }
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, refactor.Refusef("invalid arguments: %v", err)
	}
	src, err := os.ReadFile(ws.Abs(a.File))
	if err != nil {
		return nil, err
	}
	i := bytes.Index(src, []byte(a.Old))
	if i < 0 {
		return nil, refactor.Refusef("%q is not in %s", a.Old, a.File)
	}
	return &refactor.Plan{Summary: "replaced " + a.Old, Edits: []edit.FileEdit{{Path: ws.Abs(a.File), Edits: []edit.TextEdit{{Start: i, End: i + len(a.Old), NewText: a.New}}}}}, nil
}

// Through the engine, a step that breaks the types of another project is
// refused and undone.
func TestEngineRefusesABreakingStep(t *testing.T) {
	ws, dir := solution(t)
	res, err := refactor.Run(ws, replaceWord{}, json.RawMessage(`{"file":"src/plant.ts","old":"): string","new":"): number"}`), refactor.OptionsOf[*Workspace]{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied || !strings.Contains(res.Refused, "src/plant.test.ts:3:7: Type 'number' is not assignable to type 'string'.") {
		t.Fatalf("not refused as expected: %s", res)
	}
	if src, _ := os.ReadFile(filepath.Join(dir, "src/plant.ts")); !bytes.Contains(src, []byte("): string")) {
		t.Fatal("the refused step was not undone")
	}
}

// With errors already in the code, a step may run if it adds none: an
// error that only moved, because the step added a line above it, is not
// new; one the step makes is, and is refused. So is a batch, at its end
// and at a checkpoint.
func TestEngineToleratesErrorsAlreadyThere(t *testing.T) {
	ws, dir := solution(t)
	if err := os.WriteFile(filepath.Join(dir, "src/soil.ts"), []byte("export const depth = 1;\nexport const ph: number = \"acid\";\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Refresh(); err != nil {
		t.Fatal(err)
	}
	run := func(old, new string) *refactor.Result {
		t.Helper()
		args, _ := json.Marshal(map[string]string{"file": "src/soil.ts", "old": old, "new": new})
		res, err := refactor.Run(ws, replaceWord{}, args, refactor.OptionsOf[*Workspace]{})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := run("export const depth = 1;", "export const depth = 1;\nexport const width = 2;"); !res.Applied {
		t.Fatalf("a step that moves an error already there was refused: %s", res)
	}
	res := run("export const width = 2;", "export const width: string = 2;")
	if res.Applied || !strings.Contains(res.Refused, "src/soil.ts:2:14: Type 'number' is not assignable to type 'string'.") || strings.Contains(res.Refused, "'\"acid\"'") {
		t.Fatalf("a step that adds an error was not refused, naming only it: %s", res)
	}

	step := func(old, new string) refactor.StepOf[*Workspace] {
		args, _ := json.Marshal(map[string]string{"file": "src/soil.ts", "old": old, "new": new})
		return refactor.StepOf[*Workspace]{Refactoring: replaceWord{}, Args: args}
	}
	check := refactor.StepOf[*Workspace]{Refactoring: &refactor.CheckpointOf[*Workspace]{}, Args: json.RawMessage(`{}`)}
	b, err := refactor.RunBatch(ws, []refactor.StepOf[*Workspace]{step("width = 2", "width = 3"), check}, refactor.OptionsOf[*Workspace]{Unchecked: true})
	if err != nil {
		t.Fatal(err)
	}
	if !b.Applied {
		t.Fatalf("a batch checked at its end, adding no error, was refused: %s", b)
	}
	b, err = refactor.RunBatch(ws, []refactor.StepOf[*Workspace]{step("width = 3", "width: string = 3")}, refactor.OptionsOf[*Workspace]{Unchecked: true})
	if err != nil {
		t.Fatal(err)
	}
	if b.Applied {
		t.Fatalf("a batch that adds an error was applied: %s", b)
	}
}
