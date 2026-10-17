package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tychofreeman/go-refactor-mcp/internal/metrics"
)

// script is a fake model: each turn calls the next function with the
// conversation so far.
type script struct {
	t     *testing.T
	turns []func(msgs []Message) Message
	n     int
}

func (s *script) Chat(ctx context.Context, msgs []Message, tools []ToolDef) (Message, error) {
	if len(tools) == 0 {
		s.t.Error("no tools offered to the model")
	}
	if s.n >= len(s.turns) {
		return Message{Role: "assistant", Content: "done"}, nil
	}
	f := s.turns[s.n]
	s.n++
	return f(msgs), nil
}

func call(name, args string) ToolCall {
	return ToolCall{Function: FunctionCall{Name: name, Arguments: json.RawMessage(args)}}
}

func calls(cs ...ToolCall) func([]Message) Message {
	return func([]Message) Message { return Message{Role: "assistant", ToolCalls: cs} }
}

const src = `package m

import "fmt"

func run(ok bool) {
	if ok {
		fmt.Println("then")
	}
}
`

func module(t *testing.T, extra map[string]string) string {
	t.Helper()
	t.Setenv("GOWORK", "off")
	dir := t.TempDir()
	files := map[string]string{"go.mod": "module example.com/m\n\ngo 1.22\n", "m.go": src}
	for k, v := range extra {
		files[k] = v
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestRunAppliesAndReports(t *testing.T) {
	dir := module(t, nil)
	var transcript bytes.Buffer
	var sawNotRun, sawSystem bool
	model := &script{t: t, turns: []func([]Message) Message{
		func(msgs []Message) Message {
			sawSystem = msgs[0].Role == "system" && strings.Contains(msgs[0].Content, "Call exactly one tool per turn") &&
				strings.Contains(msgs[0].Content, "Apply exactly ONE tool per call")
			return calls(call("inspect", `{"file":"m.go"}`))(msgs)
		},
		// The line as a string, as small models often send it; a second
		// call in the same turn must not run.
		calls(call("invert_if", `{"file":"m.go","line":"6"}`), call("undo", `{}`)),
		func(msgs []Message) Message {
			for _, m := range msgs {
				sawNotRun = sawNotRun || (m.Role == "tool" && m.ToolName == "undo" && strings.HasPrefix(m.Content, "NOT RUN"))
			}
			return Message{Role: "assistant", Content: "Turned the if into a guard clause."}
		},
	}}
	rep, err := Run(context.Background(), Config{Dir: dir, Goal: "Simplify m.go", Model: model, Transcript: &transcript})
	if err != nil {
		t.Fatal(err)
	}
	if !sawSystem {
		t.Error("system prompt lacks the server instructions or the rules")
	}
	if !sawNotRun {
		t.Error("the second tool call of a turn should get a NOT RUN result")
	}
	if rep.Steps != 2 || rep.Applied["invert_if"] != 1 || rep.StopReason != "the model finished" || rep.Summary != "Turned the if into a guard clause." {
		t.Errorf("report = %+v", rep)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "m.go"))
	if !strings.Contains(string(got), "if !ok {\n\t\treturn\n\t}") {
		t.Errorf("m.go was not changed as expected:\n%s", got)
	}
	if !strings.Contains(rep.Diff, "+\tif !ok {") {
		t.Errorf("diff = %q", rep.Diff)
	}
	var kinds []string
	for _, line := range strings.Split(strings.TrimSpace(transcript.String()), "\n") {
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("bad transcript line %q: %v", line, err)
		}
		kinds = append(kinds, ev["kind"].(string))
		if ev["kind"] == "tool" && ev["name"] == "invert_if" && ev["status"] != "applied" {
			t.Errorf("invert_if event = %v", ev)
		}
	}
	if strings.Join(kinds, " ") != "start model tool model tool model end" {
		t.Errorf("transcript kinds = %v", kinds)
	}
}

func TestRunStopsAfterRepeatedFailures(t *testing.T) {
	dir := module(t, nil)
	model := &script{t: t}
	for i := 0; i < 10; i++ {
		model.turns = append(model.turns, calls(call("frobnicate", `{}`)))
	}
	rep, err := Run(context.Background(), Config{Dir: dir, Goal: "x", Model: model, MaxFailures: 3})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Steps != 3 || rep.Errors != 3 || !strings.Contains(rep.StopReason, "3 tool calls in a row") {
		t.Errorf("report = %+v", rep)
	}
}

func TestRunStepLimit(t *testing.T) {
	dir := module(t, nil)
	model := &script{t: t}
	for i := 0; i < 10; i++ {
		model.turns = append(model.turns, calls(call("inspect", `{}`)))
	}
	rep, err := Run(context.Background(), Config{Dir: dir, Goal: "x", Model: model, MaxSteps: 4})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Steps != 4 || !strings.Contains(rep.StopReason, "limit of 4") {
		t.Errorf("report = %+v", rep)
	}
}

func TestRunRevert(t *testing.T) {
	dir := module(t, nil)
	model := &script{t: t, turns: []func([]Message) Message{calls(call("invert_if", `{"file":"m.go","line":6}`))}}
	rep, err := Run(context.Background(), Config{Dir: dir, Goal: "x", Model: model, Revert: true})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "m.go"))
	if rep.Applied["invert_if"] != 1 || !rep.RolledBack || rep.Diff != "" || string(got) != src {
		t.Errorf("report = %+v\nm.go:\n%s", rep, got)
	}
}

// A test that fails when the code changes stands in for a refactoring bug:
// tests passed before and fail after, so every step is undone.
func TestRunUndoesWhenTestsBreak(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test")
	}
	dir := module(t, map[string]string{"m_test.go": `package m

import (
	"os"
	"strings"
	"testing"
)

func TestUnchanged(t *testing.T) {
	b, _ := os.ReadFile("m.go")
	if strings.Contains(string(b), "!ok") {
		t.Fatal("changed")
	}
}
`})
	model := &script{t: t, turns: []func([]Message) Message{calls(call("invert_if", `{"file":"m.go","line":6}`))}}
	rep, err := Run(context.Background(), Config{Dir: dir, Goal: "x", Model: model, Tests: true})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "m.go"))
	if rep.TestsBefore != "passed" || rep.TestsAfter != "failed" || !rep.RolledBack || rep.Diff != "" || string(got) != src {
		t.Errorf("report = %+v\nm.go:\n%s", rep, got)
	}
}

// slowModel applies one step, then blocks until the run's context ends,
// like a local model that takes longer than -timeout.
type slowModel struct{ n int }

func (m *slowModel) Chat(ctx context.Context, msgs []Message, tools []ToolDef) (Message, error) {
	m.n++
	if m.n == 1 {
		return Message{Role: "assistant", ToolCalls: []ToolCall{call("invert_if", `{"file":"m.go","line":6}`)}}, nil
	}
	<-ctx.Done()
	return Message{}, ctx.Err()
}

// When the run times out, -revert must still undo the applied steps: the
// clean-up cannot use the expired context.
func TestRunRevertAfterTimeout(t *testing.T) {
	dir := module(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rep, err := Run(ctx, Config{Dir: dir, Goal: "x", Model: &slowModel{}, Revert: true})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "m.go"))
	if rep.Applied["invert_if"] != 1 || !rep.RolledBack || rep.UndoFailed != "" || string(got) != src {
		t.Errorf("report = %+v\nm.go:\n%s", rep, got)
	}
	if !strings.Contains(rep.StopReason, "ran out of time") {
		t.Errorf("stop reason = %q", rep.StopReason)
	}
}

// The report measures the functions in the files the run changed, before
// and after, so a run can be judged by its effect on complexity.
func TestRunMeasuresChangedFiles(t *testing.T) {
	dir := module(t, map[string]string{"other.go": "package m\n\nfunc other() {}\n"})
	model := &script{t: t, turns: []func([]Message) Message{calls(call("invert_if", `{"file":"m.go","line":6}`))}}
	rep, err := Run(context.Background(), Config{Dir: dir, Goal: "x", Model: model})
	if err != nil {
		t.Fatal(err)
	}
	want := []metrics.Func{{File: "m.go", Name: "run", Lines: 5, Complexity: 2, Nesting: 1}}
	if !reflect.DeepEqual(rep.FuncsBefore, want) {
		t.Errorf("FuncsBefore = %+v, want %+v", rep.FuncsBefore, want)
	}
	want[0].Lines = 6
	if !reflect.DeepEqual(rep.FuncsAfter, want) {
		t.Errorf("FuncsAfter = %+v, want %+v", rep.FuncsAfter, want)
	}
}

// Every failed or refused call is kept with its reason, to see which tools
// a model misuses and how.
func TestRunRecordsFailures(t *testing.T) {
	dir := module(t, nil)
	model := &script{t: t, turns: []func([]Message) Message{
		calls(call("frobnicate", `{}`)),
		calls(call("invert_if", `{"file":"m.go","line":1}`)),
		calls(call("inspect", `{}`)),
	}}
	rep, err := Run(context.Background(), Config{Dir: dir, Goal: "x", Model: model})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Failures) != 2 {
		t.Fatalf("failures = %+v", rep.Failures)
	}
	if f := rep.Failures[0]; f.Tool != "frobnicate" || f.Status != "error" || !strings.HasPrefix(f.Reason, "ERROR: there is no tool") {
		t.Errorf("failure 0 = %+v", f)
	}
	if f := rep.Failures[1]; f.Tool != "invert_if" || f.Status != "refused" || !strings.HasPrefix(f.Reason, "REFUSED") || strings.Contains(f.Reason, "\n") {
		t.Errorf("failure 1 = %+v", f)
	}
}
