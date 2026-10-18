// Command refactor-ab runs coding tasks with Claude Code through the
// refactoring tools and by hand, and compares the cost and the results.
//
//	refactor-ab -tasks eval-tasks/ab.json -runs 2 -budget 2
//
// Each run costs real API money (bounded by -budget per run). Results go
// to -out: each run's stream-json, results.jsonl and summary.md.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/tychofreeman/go-refactor-mcp/internal/ab"
	"github.com/tychofreeman/go-refactor-mcp/internal/tokens"
)

func main() {
	tasksFile := flag.String("tasks", "eval-tasks/ab.json", "JSON array of tasks; a task's dir is relative to this file")
	only := flag.String("only", "", "comma-separated task names to run (default all)")
	runs := flag.Int("runs", 1, "runs per task and arm")
	arms := flag.String("arms", strings.Join(ab.Arms, ","), "arms to run")
	model := flag.String("model", "", "claude --model (default: claude's own)")
	budget := flag.Float64("budget", 2, "maximum dollars per run (claude --max-budget-usd)")
	timeout := flag.Duration("timeout", 30*time.Minute, "per run, including the check")
	claude := flag.String("claude", "claude", "the claude binary")
	mcpServer := flag.String("mcp", "", "the refactor-mcp binary (default: built from ./cmd/refactor-mcp)")
	out := flag.String("out", "", "output directory (default eval-runs/ab-<time>)")
	flag.Parse()
	log.SetFlags(0)

	tasks, err := loadTasks(*tasksFile, *only)
	if err != nil {
		log.Fatal(err)
	}
	if *out == "" {
		*out = filepath.Join("eval-runs", "ab-"+time.Now().Format("20060102-150405"))
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	if *mcpServer == "" {
		*mcpServer, err = filepath.Abs(filepath.Join(*out, "refactor-mcp"))
		if err != nil {
			log.Fatal(err)
		}
		if b, err := exec.Command("go", "build", "-o", *mcpServer, "./cmd/refactor-mcp").CombinedOutput(); err != nil {
			log.Fatalf("building refactor-mcp: %v\n%s", err, b)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	cfg := ab.Config{Claude: *claude, MCPServer: *mcpServer, Model: *model, BudgetUSD: *budget,
		Timeout: *timeout, Runs: *runs, Arms: strings.Split(*arms, ","), OutDir: *out}
	results, err := ab.RunAll(ctx, cfg, tasks)
	if err != nil {
		log.Printf("stopped early: %v", err)
	}
	summary := ab.Summary(results) + byArm(results)
	if err := os.WriteFile(filepath.Join(*out, "summary.md"), []byte(summary), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Print(summary)
}

func loadTasks(path, only string) ([]ab.Task, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tasks []ab.Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	var keep []ab.Task
	for _, t := range tasks {
		if only != "" && !strings.Contains(","+only+",", ","+t.Name+",") {
			continue
		}
		if t.Dir == "" {
			t.Dir = "."
		}
		if !filepath.IsAbs(t.Dir) {
			t.Dir = filepath.Join(filepath.Dir(path), t.Dir)
		}
		keep = append(keep, t)
	}
	if len(keep) == 0 {
		return nil, fmt.Errorf("no tasks to run in %s", path)
	}
	return keep, nil
}

// byArm shows where each arm's tokens went, by tool, over all its runs.
func byArm(results []ab.Result) string {
	var order []string
	merged := map[string]*tokens.Report{}
	for _, r := range results {
		if r.Tokens == nil {
			continue
		}
		if merged[r.Arm] == nil {
			order = append(order, r.Arm)
			merged[r.Arm] = &tokens.Report{}
		}
		merged[r.Arm].Merge(r.Tokens)
	}
	var sb strings.Builder
	for _, arm := range order {
		fmt.Fprintf(&sb, "\n## %s: tokens by tool, all runs\n\n%s", arm, merged[arm].Markdown())
	}
	return sb.String()
}
