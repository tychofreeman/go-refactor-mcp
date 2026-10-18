package ab

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestClaudeArgs(t *testing.T) {
	cfg := Config{MCPServer: "/bin/refactor-mcp", Model: "sonnet", BudgetUSD: 0.5}
	common := []string{"-p", "do it", "--output-format", "stream-json", "--verbose", "--strict-mcp-config",
		"--max-budget-usd", "0.50", "--model", "sonnet"}

	mcp := claudeArgs(cfg, "mcp", "/work", "do it")
	for _, want := range common {
		if !slices.Contains(mcp, want) {
			t.Errorf("mcp arm: missing %q in %q", want, mcp)
		}
	}
	i := slices.Index(mcp, "--mcp-config")
	if i < 0 || !strings.Contains(mcp[i+1], `"/bin/refactor-mcp"`) || !strings.Contains(mcp[i+1], `"/work"`) {
		t.Errorf("mcp arm: no gorefactor server for /work in %q", mcp)
	}
	if i := slices.Index(mcp, "--disallowedTools"); i < 0 || !strings.Contains(mcp[i+1], "Edit") {
		t.Errorf("mcp arm: Edit not disallowed in %q", mcp)
	}

	manual := claudeArgs(cfg, "manual", "/work", "do it")
	for _, want := range common {
		if !slices.Contains(manual, want) {
			t.Errorf("manual arm: missing %q in %q", want, manual)
		}
	}
	if slices.Contains(manual, "--mcp-config") {
		t.Errorf("manual arm has an MCP server: %q", manual)
	}
	if i := slices.Index(manual, "--allowedTools"); i < 0 || !strings.Contains(manual[i+1], "Edit") {
		t.Errorf("manual arm: Edit not allowed in %q", manual)
	}
}

const stream = `{"type":"system","subtype":"init"}
{"type":"assistant","request_id":"r1","parent_tool_use_id":null,"message":{"usage":{"input_tokens":1000,"output_tokens":20},"content":[{"type":"text","text":"done"}]}}
{"type":"result","subtype":"success","total_cost_usd":0.25,"num_turns":2}
`

// fakeClaude refuses to run where CLAUDE.md was left in, adds a line to
// notes.txt, and prints a canned stream.
func fakeClaude(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "stream.jsonl"), []byte(stream), 0o644)
	script := "#!/bin/sh\ntest ! -f CLAUDE.md || exit 3\ntest ! -f hidden.txt || exit 4\necho done >> notes.txt\ncat " + filepath.Join(dir, "stream.jsonl") + "\n"
	path := filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunAll(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "notes.txt"), []byte("start\n"), 0o644)
	os.WriteFile(filepath.Join(src, "CLAUDE.md"), []byte("use the MCP\n"), 0o644)
	out := t.TempDir()
	cfg := Config{Claude: fakeClaude(t), MCPServer: "refactor-mcp", Timeout: time.Minute, Runs: 1, OutDir: out}
	tasks := []Task{
		{Name: "note", Prompt: "add a note", Dir: src, Check: "grep -q done notes.txt"},
		{Name: "fails", Prompt: "add a note", Dir: src, Check: "echo nope; exit 1"},
	}

	results, err := RunAll(context.Background(), cfg, tasks)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 4 {
		t.Fatalf("want 4 results (2 tasks, 2 arms), got %d: %+v", len(results), results)
	}
	for _, r := range results {
		if r.Error != "" || r.CostUSD != 0.25 || r.Turns != 2 || r.Input != 1000 || r.Output != 20 || r.DiffLines != 1 {
			t.Errorf("%s/%s: %+v", r.Task, r.Arm, r)
		}
		if want := r.Task == "note"; r.Passed != want {
			t.Errorf("%s/%s: passed = %v, want %v", r.Task, r.Arm, r.Passed, want)
		}
		if r.Task == "fails" && !(strings.Contains(r.Check, "nope") && strings.Contains(r.Check, "+ exit 1")) {
			t.Errorf("%s/%s: check output not kept: %q", r.Task, r.Arm, r.Check)
		}
		if _, err := os.Stat(filepath.Join(out, r.Task+"-"+r.Arm+"-1.jsonl")); err != nil {
			t.Errorf("stream not kept: %v", err)
		}
	}
	data, err := os.ReadFile(filepath.Join(out, "results.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var first Result
	if len(lines) != 4 || json.Unmarshal([]byte(lines[0]), &first) != nil || first.Task != "note" {
		t.Errorf("results.jsonl:\n%s", data)
	}
	if got, _ := os.ReadFile(filepath.Join(src, "notes.txt")); string(got) != "start\n" {
		t.Errorf("the source was changed: %q", got)
	}
}

func TestRunAllReportsAFailedRun(t *testing.T) {
	src := t.TempDir()
	cfg := Config{Claude: "/bin/false", Timeout: time.Minute, Runs: 1, Arms: []string{"manual"}, OutDir: t.TempDir()}
	results, err := RunAll(context.Background(), cfg, []Task{{Name: "x", Prompt: "p", Dir: src, Check: "true"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Error == "" || results[0].Passed {
		t.Errorf("want a failed run, got %+v", results)
	}
}

func TestSummary(t *testing.T) {
	md := Summary([]Result{
		{Task: "rename", Arm: "mcp", Passed: true, CostUSD: 0.20, Turns: 4, Input: 1000, Output: 100, DiffLines: 10},
		{Task: "rename", Arm: "mcp", Passed: true, CostUSD: 0.40, Turns: 6, Input: 3000, Output: 300, DiffLines: 10},
		{Task: "rename", Arm: "manual", Passed: false, CostUSD: 0.50, Turns: 9, Input: 5000, Output: 500, DiffLines: 12},
	})
	for _, want := range []string{
		"| task | arm | passed | cost | input tokens | output tokens | turns | diff lines |",
		"| rename | mcp | 2/2 | $0.30 | 2000 | 200 | 5.0 | 10 |",
		"| rename | manual | 0/1 | $0.50 | 5000 | 500 | 9.0 | 12 |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
	if strings.Index(md, "| rename | mcp") > strings.Index(md, "| rename | manual") {
		t.Errorf("arms not in the order run:\n%s", md)
	}
}

// A task's dir may be relative (to the current directory), and may be read
// at a git revision.
func TestRunAllRelativeDirAtRev(t *testing.T) {
	// Under the package directory, so the relative path cannot also resolve
	// from the copy's directory.
	src, err := os.MkdirTemp(".", "src-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(src) })
	os.WriteFile(filepath.Join(src, "notes.txt"), []byte("at the revision\n"), 0o644)
	if _, err := run(context.Background(), src, "sh", "-c",
		"git init -q && git add -A && git -c user.name=t -c user.email=t@t commit -q -m one && echo later > notes.txt"); err != nil {
		t.Fatal(err)
	}
	rel := filepath.Base(src)
	cfg := Config{Claude: fakeClaude(t), Timeout: time.Minute, Runs: 1, Arms: []string{"manual"}, OutDir: t.TempDir()}
	tasks := []Task{{Name: "rev", Prompt: "p", Dir: rel, Rev: "HEAD", Check: "grep -q 'at the revision' notes.txt"}}
	results, err := RunAll(context.Background(), cfg, tasks)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].Passed {
		t.Errorf("want the run to start from the revision, got %+v", results)
	}
}

// A task's tests are copied in after the run and before the check: the
// run does not see them, and they are not part of its diff.
func TestHiddenTests(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "notes.txt"), []byte("start\n"), 0o644)
	tests := t.TempDir()
	os.WriteFile(filepath.Join(tests, "hidden.txt"), []byte("accept\n"), 0o644)
	os.MkdirAll(filepath.Join(tests, "sub"), 0o755)
	os.WriteFile(filepath.Join(tests, "sub", "h.txt"), []byte("accept\n"), 0o644)
	cfg := Config{Claude: fakeClaude(t), Timeout: time.Minute, Runs: 1, Arms: []string{"manual"}, OutDir: t.TempDir()}
	tasks := []Task{{Name: "hidden", Prompt: "p", Dir: src, Tests: tests, Check: "test -f hidden.txt && test -f sub/h.txt && grep -q done notes.txt"}}
	results, err := RunAll(context.Background(), cfg, tasks)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].Passed || results[0].DiffLines != 1 || results[0].Error != "" {
		t.Errorf("want the hidden tests seen only by the check, got %+v", results)
	}
}

func TestHybridArm(t *testing.T) {
	args := claudeArgs(Config{MCPServer: "/bin/refactor-mcp"}, "hybrid", "/work", "do it")
	if !slices.Contains(args, "--mcp-config") {
		t.Errorf("hybrid arm has no MCP server: %q", args)
	}
	i := slices.Index(args, "--allowedTools")
	if i < 0 || !strings.Contains(args[i+1], "mcp__gorefactor") || !strings.Contains(args[i+1], "Edit") {
		t.Errorf("hybrid arm should allow gorefactor and Edit: %q", args)
	}
	if i := slices.Index(args, "--disallowedTools"); i >= 0 && strings.Contains(args[i+1], "Edit") {
		t.Errorf("hybrid arm disallows Edit: %q", args)
	}
	if _, err := RunAll(context.Background(), Config{Arms: []string{"hybrid"}, OutDir: t.TempDir()}, nil); err != nil {
		t.Errorf("hybrid should be a known arm: %v", err)
	}
}
