package eval

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/agent"
	"github.com/tychofreeman/go-refactor-mcp/internal/metrics"
)

func TestTargetsPicksTheMostComplex(t *testing.T) {
	funcs := []metrics.Func{
		{File: "a.go", Name: "small", Complexity: 2, Nesting: 1},
		{File: "a.go", Name: "big", Complexity: 9, Nesting: 2},
		{File: "b.go", Name: "deep", Complexity: 9, Nesting: 4},
		{File: "b.go", Name: "mid", Complexity: 5, Nesting: 1},
	}
	got := Targets(funcs, 3)
	want := []Target{
		{File: "b.go", Func: "deep", Complexity: 9, Nesting: 4},
		{File: "a.go", Func: "big", Complexity: 9, Nesting: 2},
		{File: "b.go", Func: "mid", Complexity: 5, Nesting: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Targets:\n got %+v\nwant %+v", got, want)
	}
	if got := Targets(funcs, 10); len(got) != 4 {
		t.Errorf("Targets(funcs, 10) returned %d targets, want all 4", len(got))
	}
}

const src = `package m

import "fmt"

func run(ok bool) {
	if ok {
		fmt.Println("then")
	}
}
`

func module(t *testing.T) string {
	t.Helper()
	t.Setenv("GOWORK", "off")
	dir := t.TempDir()
	for name, content := range map[string]string{"go.mod": "module example.com/m\n\ngo 1.22\n", "m.go": src} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// guardModel turns run's if into a guard clause, then stops.
type guardModel struct{ goals []string }

func (m *guardModel) Chat(ctx context.Context, msgs []agent.Message, tools []agent.ToolDef) (agent.Message, error) {
	if len(msgs) == 2 {
		m.goals = append(m.goals, msgs[1].Content)
		return agent.Message{ToolCalls: []agent.ToolCall{{Function: agent.FunctionCall{Name: "invert_if", Arguments: json.RawMessage(`{"file":"m.go","line":6}`)}}}}, nil
	}
	return agent.Message{Content: "done"}, nil
}

func TestRunWorksOnACopy(t *testing.T) {
	dir := module(t)
	out := t.TempDir()
	model := &guardModel{}
	results, err := Run(context.Background(), Config{
		Dir: dir, Model: model, ModelName: "fake", OutDir: out,
		Targets: []Target{{File: "m.go", Func: "run", Complexity: 2, Nesting: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "m.go")); string(got) != src {
		t.Errorf("the module itself was changed:\n%s", got)
	}
	if len(model.goals) != 1 || !strings.Contains(model.goals[0], "run") || !strings.Contains(model.goals[0], "m.go") {
		t.Errorf("goals = %q", model.goals)
	}
	if len(results) != 1 {
		t.Fatalf("results = %+v", results)
	}
	r := results[0]
	if r.Model != "fake" || r.Applied["invert_if"] != 1 || r.StopReason != "the model finished" || r.Err != "" {
		t.Errorf("result = %+v", r)
	}
	if r.TargetBefore.Complexity != 2 || r.TargetAfter == nil || r.TargetAfter.Lines != 6 || r.Before.Funcs != 1 || r.After.Funcs != 1 {
		t.Errorf("metrics: before %+v after %+v, files %+v -> %+v", r.TargetBefore, r.TargetAfter, r.Before, r.After)
	}
	if r.DiffLines == 0 {
		t.Error("DiffLines = 0")
	}

	// Results are appended as JSON lines as each run ends, and each run
	// leaves its transcript and diff.
	f, err := os.Open(filepath.Join(out, "results.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var n int
	for sc.Scan() {
		var got Result
		if err := json.Unmarshal(sc.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n != 1 {
		t.Errorf("results.jsonl has %d lines", n)
	}
	for _, name := range []string{"01-m.go-run.jsonl", "01-m.go-run.diff"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Error(err)
		}
	}
}

func TestMarkdown(t *testing.T) {
	after := metrics.Func{Complexity: 4, Nesting: 1}
	results := []Result{
		{
			Target: Target{File: "a.go", Func: "f", Complexity: 12, Nesting: 4}, Model: "m",
			StopReason: "the model finished", Steps: 5, Applied: map[string]int{"extract_function": 2},
			Refused:     map[string]int{"invert_if": 1},
			Failures:    []agent.Failure{{Tool: "invert_if", Status: "refused", Reason: "REFUSED: no if statement at line 9"}},
			TestsBefore: "passed", TestsAfter: "passed",
			TargetBefore: metrics.Func{Complexity: 12, Nesting: 4}, TargetAfter: &after,
			Before:    metrics.Summary{Funcs: 1, Complexity: 12, MaxComplexity: 12, MaxNesting: 4},
			After:     metrics.Summary{Funcs: 3, Complexity: 14, MaxComplexity: 5, MaxNesting: 2},
			DiffLines: 20,
		},
		{
			Target: Target{File: "b.go", Func: "g", Complexity: 8, Nesting: 3}, Model: "m",
			StopReason: "6 tool calls in a row failed or were refused", Steps: 6,
			Applied: map[string]int{}, Refused: map[string]int{},
			TestsBefore: "passed", TestsAfter: "passed",
			TargetBefore: metrics.Func{Complexity: 8, Nesting: 3}, TargetAfter: &metrics.Func{Complexity: 8, Nesting: 3},
		},
	}
	md := Markdown(results)
	for _, want := range []string{
		"| a.go | f | 12 → 4 | 4 → 1 |",
		"| b.go | g | 8 → 8 | 3 → 3 |",
		"runs: 2", "changed the code: 1", "tests broken: 0",
		"| extract_function | 2 | 0 |",
		"| invert_if | 0 | 1 |",
		"| 1 | invert_if | REFUSED: no if statement at line N |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
}

func TestModuleFuncsSkipsTestdataAndTests(t *testing.T) {
	dir := module(t)
	for name, content := range map[string]string{
		"m_test.go":          "package m\n\nfunc helperForTests() {}\n",
		"testdata/x/in/x.go": "package x\n\nfunc golden() {}\n",
		"sub/sub.go":         "package sub\n\nfunc Sub() {}\n",
		".hidden/h.go":       "package h\n\nfunc hidden() {}\n",
	} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	funcs, err := ModuleFuncs(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range funcs {
		names = append(names, f.File+":"+f.Name)
	}
	if got := strings.Join(names, " "); got != "m.go:run sub/sub.go:Sub" {
		t.Errorf("ModuleFuncs = %s", got)
	}
}

// An output directory inside the module is not copied into each run's
// copy: it grows during the evaluation and holds no code.
func TestCopyModuleSkipsOutDir(t *testing.T) {
	dir := module(t)
	out := filepath.Join(dir, "eval-runs", "x")
	os.MkdirAll(out, 0o755)
	os.WriteFile(filepath.Join(out, "results.jsonl"), []byte("{}\n"), 0o644)
	dst := t.TempDir()
	if err := copyModule(dir, dst, out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "m.go")); err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "eval-runs", "x")); err == nil {
		t.Error("the output directory was copied")
	}
}
