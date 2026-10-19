// Package eval measures whether the refactoring tools help: it runs the
// agent on the most complex functions of a module, each in a throwaway
// copy, and records what changed (complexity, nesting, tests) and how the
// model used the tools.
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/tychofreeman/go-refactor-mcp/internal/agent"
	"github.com/tychofreeman/go-refactor-mcp/internal/metrics"
)

// Target is a function for the model to simplify.
type Target struct {
	File       string // relative to the module root
	Func       string // "Name" or "Type.Method"
	Complexity int
	Nesting    int
}

// Targets returns the n most complex functions, breaking ties by nesting,
// then by file and name.
func Targets(funcs []metrics.Func, n int) []Target {
	sorted := append([]metrics.Func(nil), funcs...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Complexity != b.Complexity {
			return a.Complexity > b.Complexity
		}
		if a.Nesting != b.Nesting {
			return a.Nesting > b.Nesting
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Name < b.Name
	})
	var out []Target
	for _, f := range sorted[:min(n, len(sorted))] {
		out = append(out, Target{File: f.File, Func: f.Name, Complexity: f.Complexity, Nesting: f.Nesting})
	}
	return out
}

// ModuleFuncs measures the functions of the module at root, leaving out
// tests, testdata, vendor and hidden directories (see
// metrics.ReadModule); file paths are relative to root.
func ModuleFuncs(root string) ([]metrics.Func, error) {
	files, err := metrics.ReadModule(root)
	if err != nil {
		return nil, err
	}
	return metrics.Measure(files)
}

// Goal is what the model is asked to do with a target.
func Goal(t Target) string {
	return fmt.Sprintf("Simplify the function %s in %s using the refactoring tools: reduce its nesting and complexity, "+
		"and give hard-to-read parts good names. Start by inspecting it (inspect with file %q and function %q).",
		t.Func, t.File, t.File, t.Func)
}

// Config controls an evaluation.
type Config struct {
	Dir       string // the module root; it is copied, never changed
	Patterns  []string
	Model     agent.Model
	ModelName string
	Targets   []Target

	MaxSteps    int
	MaxFailures int
	Timeout     time.Duration // per target; 0 means none
	Tests       bool          // run go test before and after each run

	OutDir string    // results.jsonl, and a transcript and diff per run
	Log    io.Writer // progress; may be nil
}

// Result is the outcome of one run.
type Result struct {
	Target
	Model   string
	Seconds float64
	Err     string // the run could not start or finish

	StopReason  string
	Steps       int
	Errors      int
	Applied     map[string]int
	Refused     map[string]int
	Failures    []agent.Failure
	TestsBefore string
	TestsAfter  string
	RolledBack  bool
	UndoFailed  string

	TargetBefore metrics.Func
	TargetAfter  *metrics.Func   // nil if the function is gone
	Before       metrics.Summary // the changed files' functions
	After        metrics.Summary
	DiffLines    int // added and removed lines
}

// Changed reports whether the run left the code changed.
func (r Result) Changed() bool { return r.DiffLines > 0 }

// TestsBroken reports whether tests passed before the run and fail after.
func (r Result) TestsBroken() bool { return r.TestsBefore == "passed" && r.TestsAfter == "failed" }

// Run evaluates every target in turn and returns the results; each is
// also appended to OutDir/results.jsonl as soon as it is known.
func Run(ctx context.Context, cfg Config) ([]Result, error) {
	if err := os.MkdirAll(cfg.OutDir, 0o755); err != nil {
		return nil, err
	}
	jf, err := os.OpenFile(filepath.Join(cfg.OutDir, "results.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	defer jf.Close()
	var results []Result
	for i, t := range cfg.Targets {
		if ctx.Err() != nil {
			break
		}
		name := fmt.Sprintf("%02d-%s-%s", i+1, strings.ReplaceAll(t.File, "/", "_"), t.Func)
		logf(cfg.Log, "== %d/%d %s %s (complexity %d, nesting %d)", i+1, len(cfg.Targets), t.File, t.Func, t.Complexity, t.Nesting)
		r := runOne(ctx, cfg, t, name)
		logf(cfg.Log, "   %s; applied %v; tests %s -> %s; complexity %d -> %s",
			r.StopReason, r.Applied, r.TestsBefore, r.TestsAfter, r.TargetBefore.Complexity, afterComplexity(r))
		results = append(results, r)
		b, err := json.Marshal(r)
		if err != nil {
			return results, err
		}
		if _, err := jf.Write(append(b, '\n')); err != nil {
			return results, err
		}
	}
	return results, nil
}

func runOne(ctx context.Context, cfg Config, t Target, name string) Result {
	r := Result{Target: t, Model: cfg.ModelName}
	start := time.Now()
	defer func() { r.Seconds = time.Since(start).Seconds() }()

	work, err := os.MkdirTemp("", "refactor-eval-")
	if err != nil {
		r.Err = err.Error()
		return r
	}
	defer os.RemoveAll(work)
	if err := copyModule(cfg.Dir, work, cfg.OutDir); err != nil {
		r.Err = err.Error()
		return r
	}
	tf, err := os.Create(filepath.Join(cfg.OutDir, name+".jsonl"))
	if err != nil {
		r.Err = err.Error()
		return r
	}
	defer tf.Close()

	rctx := ctx
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		rctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}
	rep, err := agent.Run(rctx, agent.Config{
		Dir: work, Patterns: cfg.Patterns, Goal: Goal(t), Model: cfg.Model,
		MaxSteps: cfg.MaxSteps, MaxFailures: cfg.MaxFailures, Tests: cfg.Tests,
		Transcript: tf,
	})
	if err != nil {
		r.Err = err.Error()
		return r
	}
	r.StopReason, r.Steps, r.Errors = rep.StopReason, rep.Steps, rep.Errors
	r.Applied, r.Refused, r.Failures = rep.Applied, rep.Refused, rep.Failures
	r.TestsBefore, r.TestsAfter, r.RolledBack, r.UndoFailed = rep.TestsBefore, rep.TestsAfter, rep.RolledBack, rep.UndoFailed
	r.Before, r.After = metrics.Summarize(rep.FuncsBefore), metrics.Summarize(rep.FuncsAfter)
	r.TargetBefore = metrics.Func{File: t.File, Name: t.Func, Complexity: t.Complexity, Nesting: t.Nesting}
	if f, ok := metrics.Find(rep.FuncsBefore, t.File, t.Func); ok {
		r.TargetBefore = f
	}
	if f, ok := metrics.Find(rep.FuncsAfter, t.File, t.Func); ok {
		r.TargetAfter = &f
	} else if _, changed := metrics.Find(rep.FuncsBefore, t.File, t.Func); !changed {
		f := r.TargetBefore // its file did not change
		r.TargetAfter = &f
	}
	r.DiffLines = diffLines(rep.Diff)
	if err := os.WriteFile(filepath.Join(cfg.OutDir, name+".diff"), []byte(rep.Diff), 0o644); err != nil {
		r.Err = err.Error()
	}
	return r
}

// diffLines counts the added and removed lines of a unified diff.
func diffLines(diff string) int {
	n := 0
	for _, line := range strings.Split(diff, "\n") {
		if (strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++")) || (strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---")) {
			n++
		}
	}
	return n
}

// copyModule copies the files under src to dst, skipping version control
// directories and skip (the output directory, which may be inside src).
func copyModule(src, dst, skip string) error {
	skip, err := filepath.Abs(skip)
	if err != nil {
		return err
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			if n := d.Name(); p != src && (n == ".git" || n == ".hg" || n == ".jj") {
				return filepath.SkipDir
			}
			if abs, err := filepath.Abs(p); err == nil && abs == skip {
				return filepath.SkipDir
			}
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, info.Mode().Perm())
	})
}

func afterComplexity(r Result) string {
	if r.TargetAfter == nil {
		return "gone"
	}
	return fmt.Sprint(r.TargetAfter.Complexity)
}

func afterNesting(r Result) string {
	if r.TargetAfter == nil {
		return "gone"
	}
	return fmt.Sprint(r.TargetAfter.Nesting)
}

// Markdown summarizes results: overall outcomes, one row per run, tool
// usage, and the most common refusal and error reasons.
func Markdown(results []Result) string {
	var sb strings.Builder
	sb.WriteString("# Refactoring evaluation\n\n")

	models := map[string]bool{}
	var changed, broken, rolledBack, failed, improved, worse int
	var dComplexity, dNesting, dMaxComplexity, measured int
	stops := map[string]int{}
	for _, r := range results {
		models[r.Model] = true
		if r.Err != "" {
			failed++
			continue
		}
		stops[r.StopReason]++
		if r.Changed() {
			changed++
		}
		if r.TestsBroken() {
			broken++
		}
		if r.RolledBack {
			rolledBack++
		}
		if r.TargetAfter != nil {
			measured++
			dComplexity += r.TargetAfter.Complexity - r.TargetBefore.Complexity
			dNesting += r.TargetAfter.Nesting - r.TargetBefore.Nesting
			switch {
			case r.TargetAfter.Complexity < r.TargetBefore.Complexity || r.TargetAfter.Nesting < r.TargetBefore.Nesting:
				improved++
			case r.TargetAfter.Complexity > r.TargetBefore.Complexity || r.TargetAfter.Nesting > r.TargetBefore.Nesting:
				worse++
			}
		}
		if r.Changed() {
			dMaxComplexity += r.After.MaxComplexity - r.Before.MaxComplexity
		}
	}
	fmt.Fprintf(&sb, "- model: %s\n", strings.Join(sortedKeys(models), ", "))
	fmt.Fprintf(&sb, "- runs: %d (could not run: %d)\n", len(results), failed)
	fmt.Fprintf(&sb, "- changed the code: %d\n", changed)
	fmt.Fprintf(&sb, "- tests broken: %d (rolled back: %d)\n", broken, rolledBack)
	fmt.Fprintf(&sb, "- target function improved (lower complexity or nesting): %d; worse: %d\n", improved, worse)
	if measured > 0 {
		fmt.Fprintf(&sb, "- mean change in the target: complexity %+.1f, nesting %+.1f\n",
			float64(dComplexity)/float64(measured), float64(dNesting)/float64(measured))
	}
	if changed > 0 {
		fmt.Fprintf(&sb, "- mean change in the changed files' highest complexity: %+.1f\n", float64(dMaxComplexity)/float64(changed))
	}
	sb.WriteString("- stopped because:\n")
	for _, s := range sortedKeys(stops) {
		fmt.Fprintf(&sb, "  - %s: %d\n", s, stops[s])
	}

	sb.WriteString("\n## Runs\n\n")
	sb.WriteString("| file | function | complexity | nesting | functions in changed files | applied | refused | errors | tests | stop |\n")
	sb.WriteString("|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range results {
		if r.Err != "" {
			fmt.Fprintf(&sb, "| %s | %s | | | | | | | | could not run: %s |\n", r.File, r.Func, cell(r.Err))
			continue
		}
		tests := ""
		if r.TestsBefore != "" {
			tests = r.TestsBefore + " → " + r.TestsAfter
			if r.RolledBack {
				tests += " (rolled back)"
			}
		}
		fmt.Fprintf(&sb, "| %s | %s | %d → %s | %d → %s | %d → %d | %s | %s | %d | %s | %s |\n",
			r.File, r.Func, r.TargetBefore.Complexity, afterComplexity(r), r.TargetBefore.Nesting, afterNesting(r),
			r.Before.Funcs, r.After.Funcs, countsCell(r.Applied), countsCell(r.Refused), r.Errors, tests, cell(r.StopReason))
	}

	applied, refused, errs := map[string]int{}, map[string]int{}, map[string]int{}
	tools := map[string]bool{}
	reasons := map[string]int{}
	for _, r := range results {
		for k, v := range r.Applied {
			applied[k] += v
			tools[k] = true
		}
		for k, v := range r.Refused {
			refused[k] += v
			tools[k] = true
		}
		for _, f := range r.Failures {
			if f.Status == "error" {
				errs[f.Tool]++
				tools[f.Tool] = true
			}
			reasons[f.Tool+"\x00"+normalize(f.Reason)]++
		}
	}
	sb.WriteString("\n## Tools\n\n| tool | applied | refused | errors |\n|---|---|---|---|\n")
	for _, k := range sortedKeys(tools) {
		fmt.Fprintf(&sb, "| %s | %d | %d | %d |\n", k, applied[k], refused[k], errs[k])
	}

	sb.WriteString("\n## Refusals and errors\n\n")
	if len(reasons) == 0 {
		sb.WriteString("none\n")
		return sb.String()
	}
	keys := sortedKeys(reasons)
	sort.SliceStable(keys, func(i, j int) bool { return reasons[keys[i]] > reasons[keys[j]] })
	sb.WriteString("| count | tool | reason (numbers replaced by N) |\n|---|---|---|\n")
	for _, k := range keys {
		tool, reason, _ := strings.Cut(k, "\x00")
		fmt.Fprintf(&sb, "| %d | %s | %s |\n", reasons[k], tool, cell(reason))
	}
	return sb.String()
}

var number = regexp.MustCompile(`\b\d+\b`)

// normalize groups reasons that differ only in line numbers and counts.
func normalize(reason string) string {
	return number.ReplaceAllString(reason, "N")
}

func cell(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	if len(s) > 160 {
		s = s[:157] + "..."
	}
	return s
}

func countsCell(m map[string]int) string {
	var parts []string
	for _, k := range sortedKeys(m) {
		parts = append(parts, fmt.Sprintf("%s %d", k, m[k]))
	}
	return strings.Join(parts, ", ")
}

func sortedKeys[V any](m map[string]V) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func logf(w io.Writer, format string, args ...any) {
	if w != nil {
		fmt.Fprintf(w, format+"\n", args...)
	}
}
