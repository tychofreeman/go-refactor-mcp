// Package ab runs the same coding tasks with Claude Code two ways, through
// the refactoring tools (arm "mcp") and by hand (arm "manual"), and
// compares what each cost and whether the result passed its check.
//
// Every run starts from a fresh copy of the task's module, without its
// CLAUDE.md (which would tell the manual arm to use tools it does not
// have), as a git repository so the change can be measured afterwards.
// Claude runs headless (claude -p) with stream-json output, which
// internal/tokens reads.
package ab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/tychofreeman/go-refactor-mcp/internal/tokens"
)

// Task is one coding task.
type Task struct {
	Name   string `json:"name"`
	Prompt string `json:"prompt"`
	Dir    string `json:"dir,omitempty"`   // the module to work on; default "."
	Rev    string `json:"rev,omitempty"`   // a git revision of Dir to start from; default its files as they are
	Check  string `json:"check,omitempty"` // shell command that must pass afterwards; default "go test ./..."
}

// Arms are the ways a task is run.
var Arms = []string{"mcp", "manual"}

// Config controls a set of runs.
type Config struct {
	Claude    string        // the claude binary
	MCPServer string        // the refactor-mcp binary, for the mcp arm
	Model     string        // passed to claude --model, if set
	BudgetUSD float64       // per run, passed to claude --max-budget-usd
	Timeout   time.Duration // per run, including the check
	Runs      int           // per task and arm
	Arms      []string      // default Arms
	OutDir    string        // each run's stream and the results are written here
}

// Result is one run of one task in one arm.
type Result struct {
	Task      string  `json:"task"`
	Arm       string  `json:"arm"`
	Run       int     `json:"run"`
	Passed    bool    `json:"passed"`
	CostUSD   float64 `json:"cost_usd"`
	Turns     int     `json:"turns"`
	Input     int     `json:"input_tokens"` // summed over requests, cached or not
	Output    int     `json:"output_tokens"`
	DiffLines int     `json:"diff_lines"` // lines added and removed
	Seconds   float64 `json:"seconds"`
	Error     string  `json:"error,omitempty"`
	Check     string  `json:"check_output,omitempty"` // the end of it, when it failed

	Tokens *tokens.Report `json:"-"`
}

// claudeArgs are the flags for one run in arm, working in dir.
func claudeArgs(cfg Config, arm, dir, prompt string) []string {
	args := []string{"-p", prompt, "--output-format", "stream-json", "--verbose", "--strict-mcp-config"}
	// Neither arm may hand the work to a subagent or the web.
	disallowed := []string{"Agent", "Task", "WebFetch", "WebSearch"}
	switch arm {
	case "mcp":
		server, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{
			"gorefactor": map[string]any{"command": cfg.MCPServer, "args": []string{"-C", dir}},
		}})
		args = append(args, "--mcp-config", string(server), "--allowedTools", "mcp__gorefactor,Read,Bash,Grep,Glob")
		disallowed = append(disallowed, "Edit", "Write", "MultiEdit", "NotebookEdit")
	case "manual":
		args = append(args, "--allowedTools", "Read,Edit,Write,MultiEdit,Bash,Grep,Glob")
	}
	args = append(args, "--disallowedTools", strings.Join(disallowed, ","))
	if cfg.BudgetUSD > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", cfg.BudgetUSD))
	}
	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}
	return args
}

// RunAll runs every task cfg.Runs times in each arm, one run at a time,
// and returns the results, also written to cfg.OutDir/results.jsonl.
func RunAll(ctx context.Context, cfg Config, tasks []Task) ([]Result, error) {
	arms := cfg.Arms
	if len(arms) == 0 {
		arms = Arms
	}
	for _, arm := range arms {
		if !slices.Contains(Arms, arm) {
			return nil, fmt.Errorf("unknown arm %q (want one of %v)", arm, Arms)
		}
	}
	if cfg.Runs == 0 {
		cfg.Runs = 1
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Minute
	}
	if err := os.MkdirAll(cfg.OutDir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.Create(filepath.Join(cfg.OutDir, "results.jsonl"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	var results []Result
	// Arms alternate within a task, so a change over time (load, a model
	// update) does not favour one of them.
	for _, t := range tasks {
		for i := 1; i <= cfg.Runs; i++ {
			for _, arm := range arms {
				if err := ctx.Err(); err != nil {
					return results, err
				}
				r := runOne(ctx, cfg, t, arm, i)
				results = append(results, r)
				if err := enc.Encode(r); err != nil {
					return results, err
				}
			}
		}
	}
	return results, nil
}

// Summary renders results as Markdown: per task and arm, how many runs
// passed and their mean cost.
func Summary(results []Result) string {
	type key struct{ task, arm string }
	var order []key
	groups := map[key][]Result{}
	for _, r := range results {
		k := key{r.Task, r.Arm}
		if groups[k] == nil {
			order = append(order, k)
		}
		groups[k] = append(groups[k], r)
	}
	var sb strings.Builder
	sb.WriteString("Means per run.\n\n| task | arm | passed | cost | input tokens | output tokens | turns | diff lines |\n|---|---|---|---|---|---|---|---|\n")
	for _, k := range order {
		rs := groups[k]
		passed := 0
		var cost, in, out, turns, diff float64
		for _, r := range rs {
			if r.Passed {
				passed++
			}
			cost += r.CostUSD
			in += float64(r.Input)
			out += float64(r.Output)
			turns += float64(r.Turns)
			diff += float64(r.DiffLines)
		}
		n := float64(len(rs))
		fmt.Fprintf(&sb, "| %s | %s | %d/%d | $%.2f | %.0f | %.0f | %.1f | %.0f |\n",
			k.task, k.arm, passed, len(rs), cost/n, in/n, out/n, turns/n, diff/n)
	}
	return sb.String()
}

// runOne runs task t once in arm and checks the result.
func runOne(ctx context.Context, cfg Config, t Task, arm string, i int) Result {
	res := Result{Task: t.Name, Arm: arm, Run: i}
	start := time.Now()
	dir, err := os.MkdirTemp("", "ab-"+t.Name+"-")
	if err != nil {
		res.Error = err.Error()
		return res
	}
	defer os.RemoveAll(dir)
	if err := prepare(t, dir); err != nil {
		res.Error = "preparing the copy: " + err.Error()
		return res
	}

	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	streamPath := filepath.Join(cfg.OutDir, fmt.Sprintf("%s-%s-%d.jsonl", t.Name, arm, i))
	f, err := os.Create(streamPath)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	cmd := exec.CommandContext(ctx, cfg.Claude, claudeArgs(cfg, arm, dir, t.Prompt)...)
	cmd.Dir = dir
	cmd.Stdout = f
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	f.Close()
	if runErr != nil {
		res.Error = fmt.Sprintf("claude: %v %s", runErr, tail(stderr.String(), 20))
	}
	if rep, err := readReport(streamPath); err == nil {
		res.Tokens = rep
		res.CostUSD, res.Turns, res.Input, res.Output = rep.CostUSD, rep.Turns, rep.InputTokens, rep.OutputTokens
	}
	res.DiffLines, _ = diffLines(dir)

	check := t.Check
	if check == "" {
		check = "go test ./..."
	}
	out, err := run(ctx, dir, "sh", "-xc", check)
	res.Passed = err == nil && runErr == nil
	if err != nil {
		res.Check = tail(out, 40)
	}
	res.Seconds = time.Since(start).Seconds()
	return res
}

// prepare puts the task's module in dir, without CLAUDE.md, committed so
// the run's change can be measured.
func prepare(t Task, dir string) error {
	src := t.Dir
	if abs, err := filepath.Abs(src); err == nil {
		src = abs // the commands below run in dir
	}
	if t.Rev != "" {
		if _, err := run(context.Background(), dir, "sh", "-c", `git -C "$1" archive "$2" | tar -x -C "$3"`, "sh", src, t.Rev, dir); err != nil {
			return err
		}
	} else {
		if _, err := run(context.Background(), dir, "cp", "-a", src+"/.", dir); err != nil {
			return err
		}
		os.RemoveAll(filepath.Join(dir, ".git"))
	}
	os.Remove(filepath.Join(dir, "CLAUDE.md"))
	_, err := run(context.Background(), dir, "sh", "-c",
		"git init -q && git add -A && git -c user.name=ab -c user.email=ab@localhost commit -q --allow-empty -m start")
	return err
}

// diffLines counts the lines the run added and removed.
func diffLines(dir string) (int, error) {
	out, err := run(context.Background(), dir, "sh", "-c", "git add -A && git diff --cached --numstat")
	if err != nil {
		return 0, err
	}
	n := 0
	for _, l := range strings.Split(out, "\n") {
		var added, removed int
		if _, err := fmt.Sscanf(l, "%d\t%d", &added, &removed); err == nil {
			n += added + removed
		}
	}
	return n, nil
}

func readReport(path string) (*tokens.Report, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return tokens.Analyze(f)
}

// run runs a command in dir and returns its combined output.
func run(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s: %v: %s", name, err, tail(string(out), 10))
	}
	return string(out), nil
}

// tail keeps the last n lines of s.
func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
