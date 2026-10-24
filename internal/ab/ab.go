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

	"github.com/tychofreeman/go-refactor-mcp/internal/metrics"
	"github.com/tychofreeman/go-refactor-mcp/internal/tokens"
)

// Task is one coding task.
type Task struct {
	Name   string  `json:"name"`
	Prompt string  `json:"prompt"`
	Dir    string  `json:"dir,omitempty"`   // the module to work on; default "."
	Rev    string  `json:"rev,omitempty"`   // a git revision of Dir to start from; default its files as they are
	Check  string  `json:"check,omitempty"` // shell command that must pass afterwards; default "go test ./..."
	Tests  string  `json:"tests,omitempty"` // files copied over the copy after the run, before the check: acceptance tests the run did not see
	Then   []Stage `json:"then,omitempty"`  // later changes, each a new session in the copy the stage before left
}

// Stage is one change asked of a run: a task's own, or one of the later
// ones in Then, each made in the copy the stage before left.
type Stage struct {
	Prompt string `json:"prompt"`
	Tests  string `json:"tests,omitempty"` // files copied over the copy after the stage, before its check
	Check  string `json:"check,omitempty"` // shell command that must pass afterwards; default "go test ./..."
}

// first is t's own stage, the one before Then.
func (t Task) first() Stage { return Stage{Prompt: t.Prompt, Tests: t.Tests, Check: t.Check} }

// Arms are the ways a task is run.
var Arms = []string{"mcp", "manual"}

// ExtraArms are run only when asked for: hybrid has the refactoring tools
// and Claude's own editing tools too; names is the mcp arm reading Go
// code only by name (refactor-mcp -read-by-name, and no other reads).
var ExtraArms = []string{"hybrid", "names"}

// lookingBash is the Bash the mcp arm may run: building, testing and
// looking, but nothing that edits a file, so its edits go through the MCP.
var lookingBash = []string{
	"Bash(go build:*)", "Bash(go test:*)", "Bash(go vet:*)", "Bash(gofmt -l:*)",
	"Bash(git diff:*)", "Bash(git status:*)", "Bash(ls:*)", "Bash(grep:*)",
	"Bash(cd:*)", "Bash(cat:*)", "Bash(head:*)", "Bash(tail:*)", "Bash(wc:*)",
}

// namesBash is the Bash the names arm may run: the mcp arm's, without
// what shows a file's lines (cat, head, tail, grep), so that it reads Go
// code only through inspect, by name.
var namesBash = []string{
	"Bash(go build:*)", "Bash(go test:*)", "Bash(go vet:*)", "Bash(gofmt -l:*)",
	"Bash(git diff:*)", "Bash(git status:*)", "Bash(ls:*)", "Bash(cd:*)", "Bash(wc:*)",
}

// toolSet is what an arm gives the model.
type toolSet struct {
	mcp        bool     // the gorefactor server
	server     []string // the server's flags beyond -C
	allowed    []string
	disallowed []string // beyond what no arm may use
}

// armTools are the tools of each arm.
var armTools = map[string]toolSet{
	"mcp": {
		mcp:        true,
		allowed:    append([]string{"mcp__gorefactor", "Read", "Grep", "Glob"}, lookingBash...),
		disallowed: []string{"Edit", "Write", "MultiEdit", "NotebookEdit"},
	},
	"hybrid": {
		mcp:     true,
		allowed: []string{"mcp__gorefactor", "Read", "Edit", "Write", "MultiEdit", "Bash", "Grep", "Glob"},
	},
	"manual": {
		allowed: []string{"Read", "Edit", "Write", "MultiEdit", "Bash", "Grep", "Glob"},
	},
	"names": {
		mcp:        true,
		server:     []string{"-read-by-name"},
		allowed:    append([]string{"mcp__gorefactor"}, namesBash...),
		disallowed: []string{"Edit", "Write", "MultiEdit", "NotebookEdit", "Read", "Grep", "Glob"},
	},
}

// Config controls a set of runs.
type Config struct {
	Claude    string            // the claude binary
	MCPServer string            // the refactor-mcp binary, for the mcp arm
	Model     string            // passed to claude --model, if set
	BudgetUSD float64           // per run, passed to claude --max-budget-usd
	Timeout   time.Duration     // per run, including the check
	Runs      int               // per task and arm
	Arms      []string          // default Arms
	OutDir    string            // each run's stream and the results are written here
	ClaudeMD  map[string]string // by arm: a CLAUDE.md to start the copy with, in place of the module's own (which is always removed)
}

// Result is one run of one task in one arm.
type Result struct {
	Task       string  `json:"task"`
	Arm        string  `json:"arm"`
	Run        int     `json:"run"`
	Passed     bool    `json:"passed"`
	CostUSD    float64 `json:"cost_usd"`
	Turns      int     `json:"turns"`
	Input      int     `json:"input_tokens"` // summed over requests, cached or not
	Output     int     `json:"output_tokens"`
	DiffLines  int     `json:"diff_lines"` // lines added and removed
	Seconds    float64 `json:"seconds"`
	Error      string  `json:"error,omitempty"`
	Check      string  `json:"check_output,omitempty"` // the end of it, when it failed
	Unfinished bool    `json:"unfinished,omitempty"`   // the session ended without claude -p's result (a timeout, say), so it has no cost and only part of its tokens

	Quality Quality `json:"quality"`

	Tokens *tokens.Report `json:"-"`
}

// Quality is how good the code a run left behind is, by the numbers
// inspect shows the model: of the functions the run added or changed, and
// of the whole module before and after (tests left out).
type Quality struct {
	Changed       int `json:"changed_funcs"`
	MaxComplexity int `json:"changed_max_complexity"`
	MaxNesting    int `json:"changed_max_nesting"`
	MaxLines      int `json:"changed_max_lines"`
	Complexity    int `json:"complexity_delta"`     // the module's total, after minus before
	OverThreshold int `json:"over_threshold_delta"` // functions over the complexity threshold, plus those over the nesting one: after minus before
	DupBefore     int `json:"dup_lines_before"`
	DupAfter      int `json:"dup_lines_after"`
	// Unmeasured says why the function numbers above could not be
	// measured (a Go file of the module that does not parse), if they
	// could not; the duplicated lines, counted from the text, still are.
	Unmeasured string `json:"unmeasured,omitempty"`
}

// measureQuality compares the module's Go files (path -> source) before
// and after a run.
func measureQuality(before, after map[string][]byte) Quality {
	q := Quality{DupBefore: metrics.Duplicated(before), DupAfter: metrics.Duplicated(after)}
	fb, err := metrics.Measure(before)
	if err != nil {
		q.Unmeasured = "before the run: " + err.Error()
		return q
	}
	changed, err := metrics.Changed(before, after)
	if err != nil {
		q.Unmeasured = err.Error()
		return q
	}
	for _, f := range changed {
		q.Changed++
		q.MaxComplexity = max(q.MaxComplexity, f.Complexity)
		q.MaxNesting = max(q.MaxNesting, f.Nesting)
		q.MaxLines = max(q.MaxLines, f.Lines)
	}
	fa, _ := metrics.Measure(after) // it parsed in Changed
	sb, sa := metrics.Summarize(fb), metrics.Summarize(fa)
	q.Complexity = sa.Complexity - sb.Complexity
	q.OverThreshold = sa.OverComplexity + sa.OverNesting - sb.OverComplexity - sb.OverNesting
	return q
}

// values are q's numbers in the order of the summary's columns.
func (q Quality) values() []int {
	return []int{q.Changed, q.MaxComplexity, q.MaxNesting, q.MaxLines, q.Complexity, q.OverThreshold, q.DupBefore, q.DupAfter}
}

// claudeArgs are the flags for one run in arm, working in dir.
func claudeArgs(cfg Config, arm, dir, prompt string) []string {
	args := []string{"-p", prompt, "--output-format", "stream-json", "--verbose", "--strict-mcp-config"}
	// No arm may hand the work to a subagent or the web.
	disallowed := []string{"Agent", "Task", "WebFetch", "WebSearch"}
	tools := armTools[arm]
	if tools.mcp {
		server, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{
			"gorefactor": map[string]any{"command": cfg.MCPServer, "args": append([]string{"-C", dir}, tools.server...)},
		}})
		args = append(args, "--mcp-config", string(server))
	}
	args = append(append(args, "--allowedTools"), tools.allowed...)
	disallowed = append(disallowed, tools.disallowed...)
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
		if !slices.Contains(Arms, arm) && !slices.Contains(ExtraArms, arm) {
			return nil, fmt.Errorf("unknown arm %q (want one of %v)", arm, slices.Concat(Arms, ExtraArms))
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
				for _, r := range runOne(ctx, cfg, t, arm, i) {
					results = append(results, r)
					if err := enc.Encode(r); err != nil {
						return results, err
					}
				}
			}
		}
	}
	return results, nil
}

type taskArm struct{ task, arm string }

// Summary renders results as Markdown: per task and arm, how many runs
// passed and their mean cost. The means of cost, tokens and turns leave
// out unfinished runs, which have no cost and only part of their tokens.
func Summary(results []Result) string {
	var order []taskArm
	groups := map[taskArm][]Result{}
	for _, r := range results {
		k := taskArm{r.Task, r.Arm}
		if groups[k] == nil {
			order = append(order, k)
		}
		groups[k] = append(groups[k], r)
	}
	var sb strings.Builder
	sb.WriteString("Means per run.\n\n| task | arm | passed | cost | input tokens | output tokens | turns | diff lines |\n|---|---|---|---|---|---|---|---|\n")
	var unfinished []string
	for _, k := range order {
		rs := groups[k]
		passed, finished := 0, 0
		var cost, in, out, turns, diff float64
		for _, r := range rs {
			if r.Passed {
				passed++
			}
			diff += float64(r.DiffLines)
			if r.Unfinished {
				continue
			}
			finished++
			cost += r.CostUSD
			in += float64(r.Input)
			out += float64(r.Output)
			turns += float64(r.Turns)
		}
		if finished < len(rs) {
			unfinished = append(unfinished, fmt.Sprintf("%s %s: %d of %d", k.task, k.arm, len(rs)-finished, len(rs)))
		}
		n, f := float64(len(rs)), float64(max(finished, 1))
		fmt.Fprintf(&sb, "| %s | %s | %d/%d | $%.2f | %.0f | %.0f | %.1f | %.0f |\n",
			k.task, k.arm, passed, len(rs), cost/f, in/f, out/f, turns/f, diff/n)
	}
	if len(unfinished) > 0 {
		fmt.Fprintf(&sb, "\nUnfinished runs (cut off before claude -p's result, by a timeout say), left out of the means of cost, tokens and turns: %s.\n", strings.Join(unfinished, "; "))
	}
	writeQuality(&sb, order, groups)
	return sb.String()
}

// writeQuality writes the table of the quality of the code each task and
// arm left behind. The means of the function columns leave out runs whose
// functions could not be measured, which it lists below the table.
func writeQuality(sb *strings.Builder, order []taskArm, groups map[taskArm][]Result) {
	sb.WriteString("\nQuality of the code left behind, means per run: the functions the run added or changed (how many; their highest complexity, nesting and length), " +
		"the change in the module's total complexity and in functions over the thresholds, and its duplicated lines.\n\n" +
		"| task | arm | changed funcs | max complexity | max nesting | longest | Δ complexity | Δ over threshold | dup lines before → after |\n|---|---|---|---|---|---|---|---|---|\n")
	var unmeasured []string
	for _, k := range order {
		rs := groups[k]
		n := len(Quality{}.values())
		sums := make([]float64, n)
		measured := 0
		for _, r := range rs {
			if r.Quality.Unmeasured != "" {
				unmeasured = append(unmeasured, fmt.Sprintf("%s %s run %d: %s", k.task, k.arm, r.Run, r.Quality.Unmeasured))
			} else {
				measured++
			}
			for i, v := range r.Quality.values() {
				if i < n-2 && r.Quality.Unmeasured != "" {
					continue // a function column
				}
				sums[i] += float64(v)
			}
		}
		means := []any{k.task, k.arm}
		for i, s := range sums {
			if i < n-2 {
				s /= float64(max(measured, 1))
			} else {
				s /= float64(len(rs))
			}
			means = append(means, s)
		}
		fmt.Fprintf(sb, "| %s | %s | %.1f | %.1f | %.1f | %.1f | %.1f | %.1f | %.0f → %.0f |\n", means...)
	}
	if len(unmeasured) > 0 {
		fmt.Fprintf(sb, "\nRuns whose functions could not be measured, left out of the function columns: %s.\n", strings.Join(unmeasured, "; "))
	}
}

// runOne runs task t once in arm, stage after stage, in one copy, and
// checks each stage.
func runOne(ctx context.Context, cfg Config, t Task, arm string, i int) []Result {
	first := Result{Task: t.Name, Arm: arm, Run: i}
	start := time.Now()
	dir, err := os.MkdirTemp("", "ab-"+t.Name+"-")
	if err != nil {
		first.Error = err.Error()
		return []Result{first}
	}
	defer os.RemoveAll(dir)
	if err := prepare(t, dir, cfg.ClaudeMD[arm]); err != nil {
		first.Error = "preparing the copy: " + err.Error()
		return []Result{first}
	}

	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	stream := func(name string) string {
		return filepath.Join(cfg.OutDir, fmt.Sprintf("%s-%s-%d.jsonl", name, arm, i))
	}
	results := []Result{runStage(ctx, cfg, arm, first, dir, start, t.first(), stream(t.Name))}
	for k, s := range t.Then {
		res := Result{Task: fmt.Sprintf("%s#%d", t.Name, k+2), Arm: arm, Run: i}
		// This stage starts where the one before ended, its tests included,
		// committed so that its diff is this stage's alone.
		if _, err := run(ctx, dir, "sh", "-c", "git add -A && git -c user.name=ab -c user.email=ab@localhost commit -q --allow-empty -m stage"); err != nil {
			res.Error = "committing the stage before: " + err.Error()
			results = append(results, res)
			continue
		}
		results = append(results, runStage(ctx, cfg, arm, res, dir, time.Now(), s, stream(res.Task)))
	}
	return results
}

// runStage runs one stage: claude in dir, which holds the copy, writing
// its stream to streamPath, then the stage's check.
func runStage(ctx context.Context, cfg Config, arm string, res Result, dir string, start time.Time, s Stage, streamPath string) Result {
	f, err := os.Create(streamPath)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	before, _ := metrics.ReadModule(dir)
	cmd := exec.CommandContext(ctx, cfg.Claude, claudeArgs(cfg, arm, dir, s.Prompt)...)
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
	res.Unfinished = res.Tokens == nil || !res.Tokens.Finished
	res.DiffLines, _ = diffLines(dir)
	after, _ := metrics.ReadModule(dir)
	res.Quality = measureQuality(before, after)
	if s.Tests != "" {
		tests := s.Tests
		if abs, err := filepath.Abs(tests); err == nil {
			tests = abs
		}
		if _, err := run(ctx, dir, "cp", "-a", tests+"/.", dir); err != nil {
			res.Error = "copying the tests in: " + err.Error()
		}
	}

	check := s.Check
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
func prepare(t Task, dir, claudeMD string) error {
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
	if claudeMD != "" {
		if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(claudeMD), 0o644); err != nil {
			return err
		}
	}
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
