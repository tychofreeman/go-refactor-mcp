// Command refactor-eval measures whether the refactoring tools help a
// model: it runs refactor-agent's loop on the most complex functions of a
// module, each in a throwaway copy (the module itself is never changed),
// and reports what happened to complexity, nesting and tests, and how the
// model used the tools.
//
//	refactor-eval -C path/to/module -model qwen3.5:9b -n 5 -out eval-runs/qwen
//
// The output directory gets results.jsonl (one line per run, written as it
// ends), summary.md, and a transcript and diff per run.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/tychofreeman/go-refactor-mcp/internal/agent"
	"github.com/tychofreeman/go-refactor-mcp/internal/eval"
)

type targetList []string

func (l *targetList) String() string     { return strings.Join(*l, ",") }
func (l *targetList) Set(s string) error { *l = append(*l, s); return nil }

func main() {
	dir := flag.String("C", ".", "the module to evaluate on (it is copied for each run, never changed)")
	model := flag.String("model", os.Getenv("OLLAMA_MODEL"), "Ollama model name (default $OLLAMA_MODEL)")
	host := flag.String("host", agent.DefaultHost(), "Ollama server URL (default $OLLAMA_HOST, else http://localhost:11434)")
	numCtx := flag.Int("ctx", 16384, "context window in tokens")
	n := flag.Int("n", 5, "evaluate the n most complex functions (ignored with -target)")
	var targets targetList
	flag.Var(&targets, "target", "a function to evaluate, as file:Func or file:Type.Method (repeatable)")
	out := flag.String("out", "", "output directory (default eval-runs/<model>-<time>)")
	maxSteps := flag.Int("max-steps", 30, "maximum tool calls per run")
	maxFailures := flag.Int("max-failures", 6, "stop a run after this many failed or refused tool calls in a row")
	timeout := flag.Duration("timeout", 45*time.Minute, "time limit for each run")
	tests := flag.Bool("tests", true, "run go test before and after each run")
	quiet := flag.Bool("q", false, "do not print progress")
	flag.Parse()

	if *model == "" {
		log.Fatal("refactor-eval: -model is required (or set $OLLAMA_MODEL)")
	}
	funcs, err := eval.ModuleFuncs(*dir)
	if err != nil {
		log.Fatalf("refactor-eval: %v", err)
	}
	var picked []eval.Target
	if len(targets) == 0 {
		picked = eval.Targets(funcs, *n)
	}
	for _, t := range targets {
		file, name, ok := strings.Cut(t, ":")
		if !ok {
			log.Fatalf("refactor-eval: -target %q: want file:Func", t)
		}
		found := false
		for _, f := range funcs {
			if f.File == file && f.Name == name {
				picked = append(picked, eval.Target{File: file, Func: name, Complexity: f.Complexity, Nesting: f.Nesting})
				found = true
			}
		}
		if !found {
			log.Fatalf("refactor-eval: -target %q: no such function", t)
		}
	}
	if *out == "" {
		*out = filepath.Join("eval-runs", strings.NewReplacer(":", "_", "/", "_").Replace(*model)+"-"+time.Now().Format("20060102-150405"))
	}

	cfg := eval.Config{
		Dir: *dir, Targets: picked,
		Model:     &agent.Ollama{Host: *host, Model: *model, NumCtx: *numCtx},
		ModelName: *model,
		MaxSteps:  *maxSteps, MaxFailures: *maxFailures, Timeout: *timeout, Tests: *tests,
		OutDir: *out,
	}
	if !*quiet {
		cfg.Log = os.Stderr
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	results, err := eval.Run(ctx, cfg)
	md := eval.Markdown(results)
	if werr := os.WriteFile(filepath.Join(*out, "summary.md"), []byte(md), 0o644); werr != nil {
		log.Printf("refactor-eval: %v", werr)
	}
	fmt.Print(md)
	fmt.Printf("\nresults in %s\n", *out)
	if err != nil {
		log.Fatalf("refactor-eval: %v", err)
	}
}
