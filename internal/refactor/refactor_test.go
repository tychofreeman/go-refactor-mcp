package refactor_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// buggy "renames" the declaration of F but none of its uses, which breaks
// the build. The engine must notice and roll it back.
type buggy struct{}

func (buggy) Name() string                 { return "buggy" }
func (buggy) Description() string          { return "" }
func (buggy) InputSchema() json.RawMessage { return json.RawMessage(`{}`) }
func (buggy) Plan(ws *workspace.Workspace, _ json.RawMessage) (*refactor.Plan, error) {
	path := ws.Abs("m.go")
	src, _ := os.ReadFile(path)
	i := strings.Index(string(src), "func F") + len("func ")
	return &refactor.Plan{
		Summary: "broken rename",
		Edits:   []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{Start: i, End: i + 1, NewText: "G"}}}},
	}, nil
}

const src = "package m\n\nfunc F() int { return 1 }\n\nvar X = F()\n"

func setup(t *testing.T, code string) (*workspace.Workspace, string) {
	t.Helper()
	t.Setenv("GOWORK", "off")
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/m\n\ngo 1.22\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "m.go"), []byte(code), 0o644)
	ws, err := workspace.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return ws, filepath.Join(dir, "m.go")
}

func TestRollbackOnBrokenBuild(t *testing.T) {
	ws, path := setup(t, src)
	res, err := refactor.Run(ws, buggy{}, nil, refactor.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied || !strings.Contains(res.Refused, "rolled back") {
		t.Fatalf("want rollback refusal, got %+v", res)
	}
	if got, _ := os.ReadFile(path); string(got) != src {
		t.Errorf("file not restored:\n%s", got)
	}
	if ws.CanUndo() {
		t.Error("rolled-back change left on the undo stack")
	}
}

func TestRefusesBrokenInput(t *testing.T) {
	ws, _ := setup(t, "package m\n\nvar X int = \"s\"\n")
	res, err := refactor.Run(ws, buggy{}, nil, refactor.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Refused, "does not type-check") {
		t.Fatalf("want type-check refusal, got %+v", res)
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	ws, path := setup(t, src)
	res, err := refactor.Run(ws, buggy{}, nil, refactor.Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Diff, "+func G() int") {
		t.Errorf("dry run should show the diff, got %q", res.Diff)
	}
	if got, _ := os.ReadFile(path); string(got) != src {
		t.Error("dry run modified the file")
	}
}

// breakingEdit is buggy as an edit: a step that may change behavior, but
// must still leave code that type-checks.
type breakingEdit struct{ buggy }

func (breakingEdit) ChangesBehavior() {}

func TestEditThatBreaksTheBuildIsRefused(t *testing.T) {
	ws, path := setup(t, src)
	res, err := refactor.Run(ws, breakingEdit{}, nil, refactor.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied || !strings.Contains(res.Refused, "does not type-check") || !strings.Contains(res.Refused, "undefined: F") {
		t.Fatalf("want a refusal listing the error, got %+v", res)
	}
	if strings.Contains(res.Refused, "bug") {
		t.Errorf("an edit's type error comes from its input, not a bug in the tool: %s", res.Refused)
	}
	if got, _ := os.ReadFile(path); string(got) != src {
		t.Errorf("file not restored:\n%s", got)
	}
	if ws.CanUndo() {
		t.Error("rolled-back edit left on the undo stack")
	}
}

func TestEditRefusesBrokenInput(t *testing.T) {
	ws, _ := setup(t, "package m\n\nvar X int = \"s\"\n")
	res, err := refactor.Run(ws, breakingEdit{}, nil, refactor.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied || !strings.Contains(res.Refused, "does not type-check") {
		t.Fatalf("want a refusal on code that does not type-check, got %+v", res)
	}
}

// bump changes F's result from 1 to 2.
type bump struct{}

func (bump) Name() string                 { return "bump" }
func (bump) Description() string          { return "" }
func (bump) InputSchema() json.RawMessage { return json.RawMessage(`{}`) }
func (bump) Plan(ws *workspace.Workspace, _ json.RawMessage) (*refactor.Plan, error) {
	path := ws.Abs("m.go")
	src, _ := os.ReadFile(path)
	i := strings.Index(string(src), "return 1") + len("return ")
	return &refactor.Plan{
		Summary: "bumped",
		Edits:   []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{Start: i, End: i + 1, NewText: "2"}}}},
	}, nil
}

func TestCompactDiff(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		ws, _ := setup(t, src)
		res, err := refactor.Run(ws, bump{}, nil, refactor.Options{Compact: true, DryRun: dryRun})
		if err != nil {
			t.Fatal(err)
		}
		want := "--- a/m.go\n+++ b/m.go\n@@ -3 +3 @@\n-func F() int { return 1 }\n+func F() int { return 2 }\n"
		if res.Diff != want {
			t.Errorf("dry run %v: got diff:\n%s\nwant:\n%s", dryRun, res.Diff, want)
		}
	}
}

// sameEverywhere is bump, reported as a change that is the same at every site.
type sameEverywhere struct{ bump }

func (s sameEverywhere) Plan(ws *workspace.Workspace, args json.RawMessage) (*refactor.Plan, error) {
	p, err := s.bump.Plan(ws, args)
	if p != nil {
		p.Locations = true
	}
	return p, err
}

func TestLocationsResult(t *testing.T) {
	ws, _ := setup(t, src)
	res, err := refactor.Run(ws, sameEverywhere{}, nil, refactor.Options{Compact: true})
	if err != nil {
		t.Fatal(err)
	}
	if want := "changed lines:\n  m.go: 3\n"; res.Diff != want {
		t.Errorf("compact: got:\n%s\nwant:\n%s", res.Diff, want)
	}

	ws, _ = setup(t, src)
	res, err = refactor.Run(ws, sameEverywhere{}, nil, refactor.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Diff, "+func F() int { return 2 }") {
		t.Errorf("not compact: want the full diff, got:\n%s", res.Diff)
	}
}

// setX changes X's initializer; its arguments only matter to RunBatch.
type setX struct{}

func (setX) Name() string                 { return "set_x" }
func (setX) Description() string          { return "" }
func (setX) InputSchema() json.RawMessage { return json.RawMessage(`{}`) }
func (setX) Plan(ws *workspace.Workspace, _ json.RawMessage) (*refactor.Plan, error) {
	path := ws.Abs("m.go")
	src, _ := os.ReadFile(path)
	i := strings.Index(string(src), "var X = F()") + len("var X = F()")
	return &refactor.Plan{
		Summary: "set X",
		Edits:   []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{Start: i, End: i, NewText: " + 1"}}}},
	}, nil
}

func TestBatchIsOneChange(t *testing.T) {
	ws, path := setup(t, src)
	b, err := refactor.RunBatch(ws, []refactor.Step{{Refactoring: bump{}}, {Refactoring: setX{}}}, refactor.Options{Compact: true})
	if err != nil {
		t.Fatal(err)
	}
	if !b.Applied || len(b.Steps) != 2 {
		t.Fatalf("want both steps applied, got %+v", b)
	}
	want := "--- a/m.go\n+++ b/m.go\n@@ -3 +3 @@\n-func F() int { return 1 }\n+func F() int { return 2 }\n@@ -5 +5 @@\n-var X = F()\n+var X = F() + 1\n"
	if b.Diff != want {
		t.Errorf("diff:\n%s\nwant:\n%s", b.Diff, want)
	}
	for _, s := range []string{"APPLIED batch: 2 steps", "1. bump: bumped", "2. set_x: set X", want} {
		if !strings.Contains(b.String(), s) {
			t.Errorf("missing %q in:\n%s", s, b)
		}
	}
	if _, err := ws.Undo(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != src || ws.CanUndo() {
		t.Errorf("one undo should undo the batch; file:\n%s", got)
	}
}

func TestBatchRollsBackOnRefusal(t *testing.T) {
	ws, path := setup(t, src)
	b, err := refactor.RunBatch(ws, []refactor.Step{{Refactoring: bump{}}, {Refactoring: buggy{}}}, refactor.Options{Compact: true})
	if err != nil {
		t.Fatal(err)
	}
	if b.Applied || b.FailedStep != 2 || b.Refused == "" {
		t.Fatalf("want a refusal at step 2, got %+v", b)
	}
	for _, s := range []string{"REFUSED batch at step 2 of 2 (buggy):", "step 1 was rolled back; nothing changed"} {
		if !strings.Contains(b.String(), s) {
			t.Errorf("missing %q in:\n%s", s, b)
		}
	}
	if got, _ := os.ReadFile(path); string(got) != src || ws.CanUndo() {
		t.Errorf("not rolled back; file:\n%s", got)
	}
}

func TestBatchRefusesStaleLines(t *testing.T) {
	byLine := json.RawMessage(`{"file": "m.go", "line": 5}`)

	ws, path := setup(t, src)
	b, err := refactor.RunBatch(ws, []refactor.Step{{Refactoring: bump{}}, {Refactoring: setX{}, Args: byLine}}, refactor.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if b.Applied || b.FailedStep != 2 || !strings.Contains(b.Refused, "step 1 changed m.go") {
		t.Errorf("want step 2 refused for stale lines, got %+v", b)
	}
	if got, _ := os.ReadFile(path); string(got) != src {
		t.Errorf("not rolled back; file:\n%s", got)
	}

	// Before any step changed the file, its line numbers are right.
	ws, _ = setup(t, src)
	b, err = refactor.RunBatch(ws, []refactor.Step{{Refactoring: setX{}, Args: byLine}, {Refactoring: bump{}}}, refactor.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !b.Applied {
		t.Errorf("want the batch applied, got %+v", b)
	}
}

func TestBatchOfRenamesListsLocations(t *testing.T) {
	ws, _ := setup(t, src)
	b, err := refactor.RunBatch(ws, []refactor.Step{{Refactoring: sameEverywhere{}}}, refactor.Options{Compact: true})
	if err != nil {
		t.Fatal(err)
	}
	if want := "changed lines:\n  m.go: 3\n"; b.Diff != want {
		t.Errorf("got:\n%s\nwant:\n%s", b.Diff, want)
	}
}

// strict decodes its arguments as a real tool does.
type strict struct{}

func (strict) Name() string        { return "strict" }
func (strict) Description() string { return "" }
func (strict) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {
  "file": {"type": "string", "description": "The file."},
  "line": {"type": "integer", "description": "The line."}
}, "required": ["file"]}`)
}
func (strict) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File string `json:"file"`
		Line int    `json:"line"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	return nil, refactor.Refusef("never applies")
}

// A model that guessed an argument's name is told what the tool takes.
func TestBadArgumentsListTheArguments(t *testing.T) {
	ws, _ := setup(t, src)
	res, err := refactor.Run(ws, strict{}, json.RawMessage(`{"file": "m.go", "type": "int"}`), refactor.Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := "invalid arguments: json: unknown field \"type\"\nstrict takes:\n  file (required): The file.\n  line: The line."
	if res.Refused != want {
		t.Errorf("got:\n%s\nwant:\n%s", res.Refused, want)
	}
	if res, _ := refactor.Run(ws, strict{}, json.RawMessage(`{"file": "m.go"}`), refactor.Options{}); strings.Contains(res.Refused, "takes:") {
		t.Errorf("other refusals should not list the arguments: %s", res.Refused)
	}
}
