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

// allowedTools are the tools args allows, one argument each.
func allowedTools(args []string) []string {
	i := slices.Index(args, "--allowedTools")
	if i < 0 {
		return nil
	}
	var out []string
	for _, a := range args[i+1:] {
		if strings.HasPrefix(a, "--") {
			break
		}
		out = append(out, a)
	}
	return out
}

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
	if !slices.Contains(allowedTools(manual), "Edit") {
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
	script := "#!/bin/sh\ntest ! -f CLAUDE.md || grep -q arm-guidance CLAUDE.md || exit 3\ntest ! -f hidden.txt || exit 4\necho done >> notes.txt\ncat " + filepath.Join(dir, "stream.jsonl") + "\n"
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
	twice := "\ta := 1\n\tb := 2\n\tc := 3\n\td := 4\n\tprintln(a, b, c, d)\n}\n"
	os.WriteFile(filepath.Join(src, "p.go"), []byte("package p\n\nfunc A() {\n"+twice+"\nfunc B() {\n"+twice), 0o644)
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
		if r.Error != "" || r.Unfinished || r.CostUSD != 0.25 || r.Turns != 2 || r.Input != 1000 || r.Output != 20 || r.DiffLines != 1 {
			t.Errorf("%s/%s: %+v", r.Task, r.Arm, r)
		}
		if q := r.Quality; q.DupBefore != 10 || q.DupAfter != 10 || q.Changed != 0 {
			t.Errorf("%s/%s: quality %+v, want 10 duplicated lines before and after, nothing changed", r.Task, r.Arm, q)
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
	if len(results) != 1 || results[0].Error == "" || results[0].Passed || !results[0].Unfinished {
		t.Errorf("want a failed run, got %+v", results)
	}
}

func TestSummary(t *testing.T) {
	md := Summary([]Result{
		{Task: "rename", Arm: "mcp", Passed: true, CostUSD: 0.20, Turns: 4, Input: 1000, Output: 100, DiffLines: 10,
			Quality: Quality{Changed: 2, MaxComplexity: 4, MaxNesting: 1, MaxLines: 10, Complexity: -2, OverThreshold: -1, DupBefore: 14, DupAfter: 4}},
		{Task: "rename", Arm: "mcp", Passed: true, CostUSD: 0.40, Turns: 6, Input: 3000, Output: 300, DiffLines: 10,
			Quality: Quality{Changed: 1, MaxComplexity: 6, MaxNesting: 2, MaxLines: 20, Complexity: 1, OverThreshold: 0, DupBefore: 14, DupAfter: 8}},
		{Task: "rename", Arm: "manual", Passed: false, CostUSD: 0.50, Turns: 9, Input: 5000, Output: 500, DiffLines: 12},
	})
	for _, want := range []string{
		"| task | arm | passed | cost | input tokens | output tokens | turns | diff lines |",
		"| rename | mcp | 2/2 | $0.30 | 2000 | 200 | 5.0 | 10 |",
		"| rename | manual | 0/1 | $0.50 | 5000 | 500 | 9.0 | 12 |",
		"| task | arm | changed funcs | max complexity | max nesting | longest | Δ complexity | Δ over threshold | dup lines before → after |",
		"| rename | mcp | 1.5 | 5.0 | 1.5 | 15.0 | -0.5 | -0.5 | 14 → 6 |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
	if strings.Index(md, "| rename | mcp") > strings.Index(md, "| rename | manual") {
		t.Errorf("arms not in the order run:\n%s", md)
	}
}

// A run cut off before claude -p's result has no cost and only part of
// its tokens, so the means leave it out, and say so.
func TestSummaryLeavesOutUnfinishedRuns(t *testing.T) {
	md := Summary([]Result{
		{Task: "rename", Arm: "manual", Passed: true, CostUSD: 0.50, Turns: 9, Input: 5000, Output: 500, DiffLines: 12},
		{Task: "rename", Arm: "manual", Unfinished: true, Turns: 0, Input: 2000, Output: 100, DiffLines: 4},
	})
	for _, want := range []string{
		"| rename | manual | 1/2 | $0.50 | 5000 | 500 | 9.0 | 8 |",
		"rename manual: 1 of 2",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
}

// A run whose functions could not be measured is left out of the means
// of the function columns, and the table says why.
func TestSummaryLeavesOutUnmeasuredRuns(t *testing.T) {
	md := Summary([]Result{
		{Task: "rename", Arm: "mcp", Run: 1, Passed: true, Quality: Quality{Changed: 2, MaxComplexity: 4, Complexity: 3, DupBefore: 10, DupAfter: 10}},
		{Task: "rename", Arm: "mcp", Run: 2, Passed: true, Quality: Quality{Unmeasured: "bad.go:2:7: expected ')'", DupBefore: 10, DupAfter: 14}},
	})
	for _, want := range []string{
		"| rename | mcp | 2.0 | 4.0 | 0.0 | 0.0 | 3.0 | 0.0 | 10 → 12 |",
		"rename mcp run 2: bad.go:2:7: expected ')'",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
}

func TestMeasureQuality(t *testing.T) {
	f := "package p\n\nfunc F(x int) int {\n\tif x > 0 {\n\t\treturn 1\n\t}\n\treturn 0\n}\n"
	g := "\nfunc G(x int) int {\n\tfor i := 0; i < x; i++ {\n\t\tif i > 2 {\n\t\t\treturn i\n\t\t}\n\t}\n\treturn 0\n}\n"
	got := measureQuality(map[string][]byte{"p.go": []byte(f)}, map[string][]byte{"p.go": []byte(f + g)})
	want := Quality{Changed: 1, MaxComplexity: 3, MaxNesting: 2, MaxLines: 8, Complexity: 3}
	if got != want {
		t.Errorf("measureQuality:\n got %+v\nwant %+v", got, want)
	}

	// A file that does not parse leaves the functions unmeasured, and says
	// which; the duplicated lines, counted from the text, are still there.
	got = measureQuality(map[string][]byte{"p.go": []byte(f)}, map[string][]byte{"p.go": []byte(f + g), "bad.go": []byte("package p\nfunc (")})
	if !strings.Contains(got.Unmeasured, "bad.go") || got.Changed != 0 || got.Complexity != 0 {
		t.Errorf("unparsable file: %+v, want Unmeasured naming bad.go and no function numbers", got)
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
	if tools := allowedTools(args); !slices.Contains(tools, "mcp__gorefactor") || !slices.Contains(tools, "Edit") {
		t.Errorf("hybrid arm should allow gorefactor and Edit: %q", args)
	}
	if i := slices.Index(args, "--disallowedTools"); i >= 0 && strings.Contains(args[i+1], "Edit") {
		t.Errorf("hybrid arm disallows Edit: %q", args)
	}
	if _, err := RunAll(context.Background(), Config{Arms: []string{"hybrid"}, OutDir: t.TempDir()}, nil); err != nil {
		t.Errorf("hybrid should be a known arm: %v", err)
	}
}

// The names arm is the mcp arm reading only by name: its server shows
// source only for what a request names, and it has no other way to read
// Go code (no Read, Grep or Glob, and no Bash that shows a file's lines).
func TestNamesArm(t *testing.T) {
	args := claudeArgs(Config{MCPServer: "/bin/refactor-mcp"}, "names", "/work", "do it")
	i := slices.Index(args, "--mcp-config")
	if i < 0 || !strings.Contains(args[i+1], `"-read-by-name"`) || !strings.Contains(args[i+1], `"/work"`) {
		t.Errorf("names arm: no server reading by name for /work in %q", args)
	}
	tools := allowedTools(args)
	for _, reads := range []string{"Read", "Grep", "Glob", "Bash(cat:*)", "Bash(grep:*)", "Bash(head:*)", "Bash(tail:*)"} {
		if slices.Contains(tools, reads) {
			t.Errorf("names arm allows %s: %q", reads, tools)
		}
	}
	if !slices.Contains(tools, "mcp__gorefactor") || !slices.Contains(tools, "Bash(go test:*)") {
		t.Errorf("names arm lacks gorefactor or go test: %q", tools)
	}
	j := slices.Index(args, "--disallowedTools")
	for _, no := range []string{"Edit", "Write", "Read", "Grep", "Glob"} {
		if j < 0 || !slices.Contains(strings.Split(args[j+1], ","), no) {
			t.Errorf("names arm: %s not disallowed in %q", no, args)
		}
	}
	if _, err := RunAll(context.Background(), Config{Arms: []string{"names"}, OutDir: t.TempDir()}, nil); err != nil {
		t.Errorf("names should be a known arm: %v", err)
	}
}

// The guarded arm is the mcp arm with the read-guard hook: a PreToolUse
// hook on Read and Bash, given through --settings, that runs the binary
// in the config. Its tools are the mcp arm's.
func TestGuardedArm(t *testing.T) {
	args := claudeArgs(Config{MCPServer: "/bin/refactor-mcp", ReadGuard: "/bin/read-guard"}, "guarded", "/work", "do it")
	i := slices.Index(args, "--settings")
	if i < 0 {
		t.Fatalf("guarded arm: no --settings in %q", args)
	}
	var settings struct {
		Hooks struct {
			PreToolUse []struct {
				Matcher string
				Hooks   []struct{ Type, Command string }
			}
		}
	}
	if err := json.Unmarshal([]byte(args[i+1]), &settings); err != nil {
		t.Fatalf("guarded arm: settings %q: %v", args[i+1], err)
	}
	pre := settings.Hooks.PreToolUse
	if len(pre) != 1 || pre[0].Matcher != "Read|Bash" || len(pre[0].Hooks) != 1 || pre[0].Hooks[0].Command != "/bin/read-guard" {
		t.Errorf("guarded arm: want a Read|Bash hook running /bin/read-guard, got %s", args[i+1])
	}
	if !slices.Equal(allowedTools(args), allowedTools(claudeArgs(Config{}, "mcp", "/work", "do it"))) {
		t.Errorf("guarded arm's tools differ from the mcp arm's: %q", allowedTools(args))
	}
	if slices.Contains(claudeArgs(Config{ReadGuard: "/bin/read-guard"}, "mcp", "/work", "do it"), "--settings") {
		t.Errorf("mcp arm has the hook")
	}
	if _, err := RunAll(context.Background(), Config{Arms: []string{"guarded"}, OutDir: t.TempDir()}, nil); err != nil {
		t.Errorf("guarded should be a known arm: %v", err)
	}
}

// An arm can start from its own CLAUDE.md, part of the starting state and
// so not of the run's diff.
func TestClaudeMDPerArm(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "notes.txt"), []byte("start\n"), 0o644)
	os.WriteFile(filepath.Join(src, "CLAUDE.md"), []byte("use the MCP\n"), 0o644)
	guidance := map[string]string{"manual": "arm-guidance: refactor by hand\n"}
	for arm, check := range map[string]string{
		"manual": `grep -q 'by hand' CLAUDE.md && ! grep -q 'use the MCP' CLAUDE.md`,
		"hybrid": `test ! -f CLAUDE.md`, // no guidance for it: the module's is still removed
	} {
		cfg := Config{Claude: fakeClaude(t), Timeout: time.Minute, Runs: 1, Arms: []string{arm}, OutDir: t.TempDir(), ClaudeMD: guidance}
		results, err := RunAll(context.Background(), cfg, []Task{{Name: "md", Prompt: "p", Dir: src, Check: check}})
		if err != nil {
			t.Fatal(err)
		}
		if r := results[0]; !r.Passed || r.DiffLines != 1 || r.Error != "" {
			t.Errorf("%s: %+v (check output %q)", arm, r, r.Check)
		}
	}
}

// The mcp arm's Bash can build, test and look, but not edit files.
func TestMCPArmBashCannotEdit(t *testing.T) {
	allowed := func(arm string) []string {
		args := claudeArgs(Config{MCPServer: "m"}, arm, "/work", "p")
		return allowedTools(args)
	}
	mcp := allowed("mcp")
	if slices.Contains(mcp, "Bash") || (!slices.Contains(mcp, "Bash(go test:*)") || !slices.Contains(mcp, "Bash(cat:*)") || !slices.Contains(mcp, "Bash(cd:*)")) || !slices.Contains(mcp, "mcp__gorefactor") {
		t.Errorf("mcp arm tools: %q", mcp)
	}
	for _, arm := range []string{"hybrid", "manual"} {
		if got := allowed(arm); !slices.Contains(got, "Bash") || !slices.Contains(got, "Edit") {
			t.Errorf("%s arm tools: %q", arm, got)
		}
	}
}

// A task's later stages run in the copy the stage before left, each as a
// session of its own, measured and checked on its own.
func TestThenStages(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "notes.txt"), []byte("start\n"), 0o644)
	tests := t.TempDir()
	os.WriteFile(filepath.Join(tests, "hidden.txt"), []byte("accept\n"), 0o644)
	out := t.TempDir()
	cfg := Config{Claude: fakeClaude(t), Timeout: time.Minute, Runs: 1, Arms: []string{"manual"}, OutDir: out}
	task := Task{Name: "chain", Prompt: "first", Dir: src, Check: "test $(grep -c done notes.txt) = 1",
		Then: []Stage{{Prompt: "second", Tests: tests, Check: "test $(grep -c done notes.txt) = 2 && test -f hidden.txt"}}}
	results, err := RunAll(context.Background(), cfg, []Task{task})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Task != "chain" || results[1].Task != "chain#2" {
		t.Fatalf("want a result per stage, got %+v", results)
	}
	for _, r := range results {
		if !r.Passed || r.DiffLines != 1 || r.CostUSD != 0.25 || r.Error != "" {
			t.Errorf("%s: %+v (check output %q)", r.Task, r, r.Check)
		}
		if _, err := os.Stat(filepath.Join(out, r.Task+"-manual-1.jsonl")); err != nil {
			t.Errorf("%s: stream not kept: %v", r.Task, err)
		}
	}
}
