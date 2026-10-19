package refactor_test

import (
	"encoding/json"
	"fmt"
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

// compileChecked is buggy as a refactoring whose last precondition is
// that the code compiles after it.
type compileChecked struct{ buggy }

func (compileChecked) ChecksByCompiling() {}

func TestCompileCheckedRefusal(t *testing.T) {
	ws, _ := setup(t, src)
	res, err := refactor.Run(ws, compileChecked{}, nil, refactor.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied || !strings.Contains(res.Refused, "would not compile, so it was not applied") || strings.Contains(res.Refused, "bug") {
		t.Errorf("want a plain refusal with the errors, got %+v", res)
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

func TestBatchTranslatesLines(t *testing.T) {
	// Line numbers in a batch are those of the files as they were when it
	// began: the second mark goes before var X, which the first moved down.
	ws, path := setup(t, src)
	b, err := refactor.RunBatch(ws, []refactor.Step{
		{Refactoring: markLine{}, Args: json.RawMessage(`{"file": "m.go", "line": 3}`)},
		{Refactoring: markLine{}, Args: json.RawMessage(`{"file": "m.go", "line": 5}`)},
	}, refactor.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !b.Applied || b.Steps[1].Summary != "marked line 6" {
		t.Fatalf("want both steps applied, the second at line 6 now, got %+v", b)
	}
	want := "package m\n\n// mark\nfunc F() int { return 1 }\n\n// mark\nvar X = F()\n"
	if got, _ := os.ReadFile(path); string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}

	// A line an earlier step rewrote cannot be addressed.
	ws, path = setup(t, src)
	b, err = refactor.RunBatch(ws, []refactor.Step{
		{Refactoring: bump{}},
		{Refactoring: markLine{}, Args: json.RawMessage(`{"file": "m.go", "line": 3}`)},
	}, refactor.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if b.Applied || b.FailedStep != 2 || !strings.Contains(b.Refused, "line 3 of m.go") || !strings.Contains(b.Refused, "step 1") {
		t.Errorf("want step 2 refused, naming the line and the step that changed it, got %+v", b)
	}
	if got, _ := os.ReadFile(path); string(got) != src {
		t.Errorf("not rolled back; file:\n%s", got)
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

// A batch checks every step's argument names before it runs any step, so
// a model that guessed several names learns all of them in one refusal.
func TestBatchChecksArgumentsFirst(t *testing.T) {
	ws, path := setup(t, src)
	b, err := refactor.RunBatch(ws, []refactor.Step{
		{Refactoring: bump{}, Args: json.RawMessage(`{"anything": 1}`)},
		{Refactoring: strict{}, Args: json.RawMessage(`{"file": "m.go", "type": "int", "kind": 2}`)},
		{Refactoring: strict{}, Args: json.RawMessage(`{"line": "$prev.end"}`)},
		{Refactoring: strict{}, Args: json.RawMessage(`{"file": "m.go", "line": 3}`)},
	}, refactor.Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := `REFUSED batch: the arguments of 2 steps are not what their tools take:
  step 2 (strict): no argument "kind" or "type"
  step 3 (strict): missing file
strict takes:
  file (required): The file.
  line: The line.
Nothing changed.
`
	if b.Applied || b.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", b, want)
	}
	if got, _ := os.ReadFile(path); string(got) != src || ws.CanUndo() {
		t.Errorf("no step should have run; file:\n%s", got)
	}
}

// markLine puts a "// mark" line before the line its arguments name.
type markLine struct{}

func (markLine) Name() string                 { return "mark_line" }
func (markLine) Description() string          { return "" }
func (markLine) InputSchema() json.RawMessage { return json.RawMessage(`{}`) }
func (markLine) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File string `json:"file"`
		Line int    `json:"line"`
		Text string `json:"text"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.Text == "" {
		a.Text = "mark"
	}
	path := ws.Abs(a.File)
	src, _ := os.ReadFile(path)
	off := 0
	for i := 1; i < a.Line; i++ {
		off += strings.Index(string(src[off:]), "\n") + 1
	}
	return &refactor.Plan{
		Summary: fmt.Sprintf("marked line %d", a.Line),
		Edits:   []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{Start: off, End: off, NewText: "// " + a.Text + "\n"}}}},
	}, nil
}

// twoPlaces changes F's body and X's initializer: two places in m.go.
type twoPlaces struct{}

func (twoPlaces) Name() string                 { return "two_places" }
func (twoPlaces) Description() string          { return "" }
func (twoPlaces) InputSchema() json.RawMessage { return json.RawMessage(`{}`) }
func (twoPlaces) Plan(ws *workspace.Workspace, _ json.RawMessage) (*refactor.Plan, error) {
	path := ws.Abs("m.go")
	src, _ := os.ReadFile(path)
	i := strings.Index(string(src), "return 1") + len("return ")
	j := strings.Index(string(src), "= F()") + len("= F()")
	return &refactor.Plan{
		Summary: "changed two places",
		Edits: []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{
			{Start: i, End: i + 1, NewText: "2"},
			{Start: j, End: j, NewText: " + 1"},
		}}},
	}, nil
}

// rewriteLine replaces line of m.go with "// rewritten"; file must be
// m.go.
type rewriteLine struct{}

func (rewriteLine) Name() string                 { return "rewrite_line" }
func (rewriteLine) Description() string          { return "" }
func (rewriteLine) InputSchema() json.RawMessage { return json.RawMessage(`{}`) }
func (rewriteLine) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File string `json:"file"`
		Line int    `json:"line"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs("m.go")
	src, _ := os.ReadFile(path)
	start := 0
	for i := 1; i < a.Line; i++ {
		start += strings.Index(string(src[start:]), "\n") + 1
	}
	end := start + strings.Index(string(src[start:]), "\n")
	return &refactor.Plan{
		Summary: fmt.Sprintf("rewrote line %d", a.Line),
		Edits:   []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{Start: start, End: end, NewText: "// rewritten"}}}},
	}, nil
}

// mark is a markLine step at line (a number or a reference), with id.
func mark(id string, line any, text string) refactor.Step {
	args, _ := json.Marshal(map[string]any{"file": "m.go", "line": line, "text": text})
	return refactor.Step{ID: id, Refactoring: markLine{}, Args: args}
}

// A step can address the lines an earlier one made: by id, by position or
// as $prev, at its start or end, with an offset. The range follows later
// steps: $a.end+1 is still right after "// a" once "// b" went in above it.
func TestBatchStepReferences(t *testing.T) {
	for _, c := range []struct {
		name  string
		steps []refactor.Step
		want  string
	}{
		{"by id", []refactor.Step{mark("a", 5, "a"), mark("", 3, "b"), mark("", "$a.end+1", "after a")},
			"package m\n\n// b\nfunc F() int { return 1 }\n\n// a\n// after a\nvar X = F()\n"},
		{"by position and prev", []refactor.Step{mark("", 3, "one"), mark("", "$1.start", "zero"), mark("", "$prev.end+1", "half")},
			"package m\n\n// zero\n// half\n// one\nfunc F() int { return 1 }\n\nvar X = F()\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ws, path := setup(t, src)
			b, err := refactor.RunBatch(ws, c.steps, refactor.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if !b.Applied {
				t.Fatalf("refused: %s", b.Refused)
			}
			if got, _ := os.ReadFile(path); string(got) != c.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, c.want)
			}
		})
	}
}

// A reference that names no step before this one, or a step without one
// range in the file, is refused, and the batch rolled back.
func TestBatchStepReferenceRefusals(t *testing.T) {
	for _, c := range []struct {
		name  string
		steps []refactor.Step
		want  string
	}{
		{"unknown id", []refactor.Step{mark("a", 3, "a"), mark("", "$b.end", "x")}, `no step before this one has id "b"`},
		{"later step", []refactor.Step{mark("", "$2.end", "x"), mark("", 3, "a")}, "step 2 has not run yet"},
		{"prev of the first", []refactor.Step{mark("", "$prev.end", "x")}, "no step before this one"},
		{"syntax", []refactor.Step{mark("", 3, "a"), mark("", "$prev.middle", "x")}, "$prev.end"},
		{"several places", []refactor.Step{{Refactoring: twoPlaces{}}, mark("", "$prev.end", "x")}, "step 1 changed m.go in 2 places"},
		{"rewritten since", []refactor.Step{mark("a", 3, "a"), {Refactoring: rewriteLine{}, Args: json.RawMessage(`{"file": "m.go", "line": "$a.start"}`)}, mark("", "$a.end", "x")}, "what step 1 made was rewritten by step 2"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ws, path := setup(t, src)
			b, err := refactor.RunBatch(ws, c.steps, refactor.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if b.Applied || !strings.Contains(b.Refused, c.want) {
				t.Errorf("want a refusal containing %q, got %+v", c.want, b)
			}
			if got, _ := os.ReadFile(path); string(got) != src {
				t.Errorf("not rolled back; file:\n%s", got)
			}
		})
	}

	// Ids must be unique, and neither prev nor a number.
	for _, ids := range [][]string{{"a", "a"}, {"prev"}, {"2"}} {
		ws, _ := setup(t, src)
		var steps []refactor.Step
		for i, id := range ids {
			steps = append(steps, mark(id, 3+i, "x"))
		}
		b, err := refactor.RunBatch(ws, steps, refactor.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if b.Applied || !strings.Contains(b.Refused, "id") {
			t.Errorf("ids %q: want a refusal, got %+v", ids, b)
		}
	}
}

// declareN is an edit that declares n in F, unused: alone, it does not
// type-check.
type declareN struct{}

func (declareN) Name() string                 { return "declare_n" }
func (declareN) Description() string          { return "" }
func (declareN) InputSchema() json.RawMessage { return json.RawMessage(`{}`) }
func (declareN) ChangesBehavior()             {}
func (declareN) Plan(ws *workspace.Workspace, _ json.RawMessage) (*refactor.Plan, error) {
	path := ws.Abs("m.go")
	src, _ := os.ReadFile(path)
	i := strings.Index(string(src), "{ return 1 }") + 1
	return &refactor.Plan{Summary: "declared n", Edits: []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{Start: i, End: i, NewText: " n := 2;"}}}}}, nil
}

// useN is an edit that returns n from F.
type useN struct{}

func (useN) Name() string                 { return "use_n" }
func (useN) Description() string          { return "" }
func (useN) InputSchema() json.RawMessage { return json.RawMessage(`{}`) }
func (useN) ChangesBehavior()             {}
func (useN) Plan(ws *workspace.Workspace, _ json.RawMessage) (*refactor.Plan, error) {
	path := ws.Abs("m.go")
	src, _ := os.ReadFile(path)
	i := strings.Index(string(src), "return 1") + len("return ")
	return &refactor.Plan{Summary: "used n", Edits: []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{Start: i, End: i + 1, NewText: "n"}}}}}, nil
}

// A batch checked at its end lets a declaration and its use be two steps.
func TestBatchCheckedAtEnd(t *testing.T) {
	steps := []refactor.Step{{Refactoring: declareN{}}, {Refactoring: useN{}}}

	ws, path := setup(t, src)
	b, err := refactor.RunBatch(ws, steps, refactor.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !b.Applied {
		t.Errorf("checked at each step, want it applied on a retry, got %+v", b)
	}

	ws, path = setup(t, src)
	b, err = refactor.RunBatch(ws, steps, refactor.Options{Unchecked: true})
	if err != nil {
		t.Fatal(err)
	}
	if !b.Applied {
		t.Fatalf("checked at the end, want it applied, got %+v", b)
	}
	if got, _ := os.ReadFile(path); !strings.Contains(string(got), "func F() int { n := 2; return n }") {
		t.Errorf("file:\n%s", got)
	}
	if _, err := ws.Undo(); err != nil || ws.CanUndo() {
		t.Errorf("want one undo for the batch: %v", err)
	}

	// Code that does not type-check at the end is rolled back.
	ws, path = setup(t, src)
	b, err = refactor.RunBatch(ws, steps[:1], refactor.Options{Unchecked: true})
	if err != nil {
		t.Fatal(err)
	}
	if b.Applied || !strings.Contains(b.String(), "declared and not used") || !strings.Contains(b.String(), "after all 1 step") {
		t.Errorf("want the batch refused at its end, got:\n%s", b)
	}
	if got, _ := os.ReadFile(path); string(got) != src || ws.CanUndo() {
		t.Errorf("not rolled back; file:\n%s", got)
	}

	// A refactoring still needs code that type-checks.
	ws, _ = setup(t, src)
	b, err = refactor.RunBatch(ws, []refactor.Step{{Refactoring: declareN{}}, {Refactoring: bump{}}}, refactor.Options{Unchecked: true})
	if err != nil {
		t.Fatal(err)
	}
	if b.Applied || b.FailedStep != 2 || !strings.Contains(b.Refused, "an earlier edit of this batch") {
		t.Errorf("want the refactoring refused, got %+v", b)
	}
}

// declare is a fix: it appends decl to m.go, choosing it.
type declare struct{ decl string }

func (declare) Name() string                 { return "declare" }
func (declare) Description() string          { return "" }
func (declare) InputSchema() json.RawMessage { return json.RawMessage(`{}`) }
func (declare) ChangesBehavior()             {}
func (declare) Fixes()                       {}
func (d declare) Plan(ws *workspace.Workspace, _ json.RawMessage) (*refactor.Plan, error) {
	path := ws.Abs("m.go")
	src, _ := os.ReadFile(path)
	return &refactor.Plan{
		Summary: "declared",
		Edits:   []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{Start: len(src), End: len(src), NewText: d.decl}}}},
		Choices: []string{strings.TrimSpace(d.decl)},
	}, nil
}

// A fix starts from code that does not type-check, and must leave fewer
// errors and no new one.
func TestFix(t *testing.T) {
	const broken = "package m\n\nvar X int = F()\n\nvar Y = G()\n"

	ws, _ := setup(t, broken)
	res, err := refactor.Run(ws, declare{"\nfunc F() int { return 1 }\n"}, nil, refactor.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Applied {
		t.Fatalf("want it applied with G still undefined, got %+v", res)
	}
	if out := res.String(); !strings.Contains(out, "chose: func F() int { return 1 }\n") {
		t.Errorf("the result does not show the choice:\n%s", out)
	}

	ws, path := setup(t, broken)
	res, err = refactor.Run(ws, declare{"\nfunc F() string { return \"\" }\n"}, nil, refactor.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied || !strings.Contains(res.Refused, "cannot use F()") {
		t.Errorf("want a new error refused, got %+v", res)
	}
	if got, _ := os.ReadFile(path); string(got) != broken {
		t.Errorf("not rolled back:\n%s", got)
	}

	ws, _ = setup(t, broken)
	res, err = refactor.Run(ws, declare{"\nfunc H() int { return 1 }\n"}, nil, refactor.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied || !strings.Contains(res.Refused, "fixes none of the errors") {
		t.Errorf("want a fix that fixes nothing refused, got %+v", res)
	}
}

// A batch whose step does not type-check until a later step completes it
// runs again, checked at its end, and says so.
func TestBatchRetriesCheckedAtEnd(t *testing.T) {
	ws, path := setup(t, src)
	b, err := refactor.RunBatch(ws, []refactor.Step{{Refactoring: declareN{}}, {Refactoring: useN{}}}, refactor.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !b.Applied || !strings.Contains(b.String(), "note: step 1 did not type-check on its own, so the batch ran again with check \"end\"\n") {
		t.Fatalf("want it applied on the retry, got:\n%s", b)
	}
	if got, _ := os.ReadFile(path); !strings.Contains(string(got), "n := 2; return n") {
		t.Errorf("file:\n%s", got)
	}
	if _, err := ws.Undo(); err != nil || ws.CanUndo() {
		t.Errorf("want one undo for the batch: %v", err)
	}

	// When the retry is refused too, the first refusal stands and says so.
	ws, path = setup(t, src)
	b, err = refactor.RunBatch(ws, []refactor.Step{{Refactoring: declareN{}}, {Refactoring: bump{}}}, refactor.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if b.Applied || b.FailedStep != 1 || !strings.Contains(b.Refused, "Run again with check \"end\", the batch was refused at step 2 (bump): the code does not type-check here") {
		t.Errorf("want step 1's refusal and the retry's, got:\n%s", b)
	}
	if got, _ := os.ReadFile(path); string(got) != src || ws.CanUndo() {
		t.Errorf("not rolled back; file:\n%s", got)
	}
}

// callG is an edit that adds a call of name, which nothing declares, to X.
type callG struct{ name string }

func (callG) Name() string                 { return "call_g" }
func (callG) Description() string          { return "" }
func (callG) InputSchema() json.RawMessage { return json.RawMessage(`{}`) }
func (callG) ChangesBehavior()             {}
func (c callG) Plan(ws *workspace.Workspace, _ json.RawMessage) (*refactor.Plan, error) {
	path := ws.Abs("m.go")
	src, _ := os.ReadFile(path)
	i := strings.LastIndex(string(src), "F()") + len("F()")
	return &refactor.Plan{Summary: "called " + c.name, Edits: []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{Start: i, End: i, NewText: " + " + c.name + "()"}}}}}, nil
}

// A batch whose code calls functions nothing declares declares them with
// opts.Declare, as create_from_usage does, and says so.
func TestBatchDeclaresMissing(t *testing.T) {
	opts := refactor.Options{Declare: declare{"\nfunc G() int { return 0 }\n"}}
	ws, path := setup(t, src)
	b, err := refactor.RunBatch(ws, []refactor.Step{{Refactoring: callG{"G"}}}, opts)
	if err != nil {
		t.Fatal(err)
	}
	want := "2. declare: declared\n   chose: func G() int { return 0 }\n   note: added by the batch, since the steps call G and nothing declares it; check that it is meant to be new, not a misspelled name\n"
	if !b.Applied || !strings.Contains(b.String(), want) {
		t.Fatalf("want G declared, got:\n%s", b)
	}
	if got, _ := os.ReadFile(path); !strings.Contains(string(got), "var X = F() + G()") || !strings.Contains(string(got), "func G() int") {
		t.Errorf("file:\n%s", got)
	}
	if _, err := ws.Undo(); err != nil || ws.CanUndo() {
		t.Errorf("want one undo for the batch: %v", err)
	}

	// A name close to a declared one may be a misspelling: it is not declared.
	ws, path = setup(t, src)
	b, err = refactor.RunBatch(ws, []refactor.Step{{Refactoring: callG{"x"}}}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if b.Applied || !strings.Contains(b.String(), "undefined: x") || !strings.Contains(b.String(), "x was not declared for you, since it is close to X") {
		t.Errorf("want x refused as a likely misspelling, got:\n%s", b)
	}
	if got, _ := os.ReadFile(path); string(got) != src || ws.CanUndo() {
		t.Errorf("not rolled back; file:\n%s", got)
	}
}

// echo is a tool that shows the arguments it was given, as JSON.
type echo struct{}

func (echo) Name() string        { return "echo" }
func (echo) Description() string { return "" }
func (echo) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {
  "file": {"type": "string"}, "name": {"type": "string"}, "parameter": {"type": "string"},
  "old": {"type": "string"}, "new": {"type": "string"}, "content": {"type": "string"}
}, "required": ["file"]}`)
}
func (echo) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a map[string]string
	json.Unmarshal(raw, &a)
	b, _ := json.Marshal(a)
	return &refactor.Plan{Summary: string(b)}, nil
}

// A guessed argument name that can only mean one argument the tool takes,
// and that is not given, is taken as that one, and the result says so.
func TestArgumentAliases(t *testing.T) {
	ws, _ := setup(t, src)
	for _, c := range []struct{ args, summary, note string }{
		{`{"file": "m.go", "old_text": "a", "new_text": "b"}`, `{"file":"m.go","new":"b","old":"a"}`, `took "new_text" as new and "old_text" as old`},
		{`{"file": "m.go", "name": "F", "parameter_name": "p"}`, `{"file":"m.go","name":"F","parameter":"p"}`, `took "parameter_name" as parameter`},
		{`{"file": "m.go", "new": "a", "new_code": "b"}`, `{"content":"b","file":"m.go","new":"a"}`, `took "new_code" as content`},
		// parameter_name could be parameter or name.
		{`{"file": "m.go", "parameter_name": "p"}`, `{"file":"m.go","parameter_name":"p"}`, ""},
	} {
		res, err := refactor.Run(ws, echo{}, json.RawMessage(c.args), refactor.Options{DryRun: true})
		if err != nil {
			t.Fatal(err)
		}
		if res.Summary != c.summary {
			t.Errorf("%s: got %s, want %s", c.args, res.Summary, c.summary)
		}
		if note := strings.Join(res.Notes, "\n"); !strings.Contains(note, c.note) || c.note == "" && note != "" {
			t.Errorf("%s: notes %q, want %q", c.args, note, c.note)
		}
	}
}
