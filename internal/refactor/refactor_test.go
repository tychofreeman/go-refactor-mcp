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
