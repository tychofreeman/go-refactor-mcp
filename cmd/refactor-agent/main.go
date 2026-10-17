// Command refactor-agent lets a local model, served by Ollama, refactor a
// Go module with the same tools the MCP server gives Claude Code: it
// inspects the code, applies one behavior-preserving refactoring at a time,
// and stops when nothing clearly improves.
//
//	refactor-agent -C path/to/module -model qwen2.5-coder:14b -goal "Simplify calc/calc.go"
//
// Every applied step type-checks and can be undone. With -tests, go test
// runs before and after; if it passed before and fails after, every step is
// undone (unless -keep). -transcript writes one JSON line per event, for
// later analysis.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/tychofreeman/go-refactor-mcp/internal/agent"
)

func main() {
	dir := flag.String("C", ".", "directory inside the Go module to refactor")
	pattern := flag.String("p", "./...", "package pattern to load, relative to the module root")
	model := flag.String("model", os.Getenv("OLLAMA_MODEL"), "Ollama model name (default $OLLAMA_MODEL)")
	host := flag.String("host", defaultHost(), "Ollama server URL (default $OLLAMA_HOST, else http://localhost:11434)")
	numCtx := flag.Int("ctx", 16384, "context window in tokens (Ollama's own default is too small for inspect output)")
	goal := flag.String("goal", "", "what to do, e.g. \"Simplify the function parse in calc/calc.go\"")
	file := flag.String("file", "", "shorthand for -goal \"Simplify the code in FILE...\"")
	maxSteps := flag.Int("max-steps", 30, "maximum tool calls")
	maxFailures := flag.Int("max-failures", 6, "stop after this many failed or refused tool calls in a row")
	tests := flag.Bool("tests", false, "run go test before and after; undo everything if they passed before and fail after")
	keep := flag.Bool("keep", false, "with -tests, keep the changes even if the tests fail afterwards")
	revert := flag.Bool("revert", false, "undo every applied step at the end (to try a model without changing the code)")
	transcript := flag.String("transcript", "", "write a JSON-lines transcript of the run to this file")
	quiet := flag.Bool("q", false, "do not print progress")
	timeout := flag.Duration("timeout", 30*time.Minute, "give up after this long")
	flag.Parse()

	if *model == "" {
		log.Fatal("refactor-agent: -model is required (or set $OLLAMA_MODEL), e.g. -model qwen2.5-coder:14b")
	}
	if *goal == "" && *file != "" {
		*goal = fmt.Sprintf("Simplify the code in %s using the refactoring tools: reduce nesting and complexity, and give hard-to-read parts good names. Start by inspecting the file.", *file)
	}
	if *goal == "" {
		log.Fatal("refactor-agent: give -goal or -file")
	}

	cfg := agent.Config{
		Dir: *dir, Patterns: []string{*pattern}, Goal: *goal,
		Model:    &agent.Ollama{Host: *host, Model: *model, NumCtx: *numCtx},
		MaxSteps: *maxSteps, MaxFailures: *maxFailures,
		Tests: *tests, Keep: *keep, Revert: *revert,
	}
	if !*quiet {
		cfg.Log = os.Stderr
	}
	if *transcript != "" {
		f, err := os.Create(*transcript)
		if err != nil {
			log.Fatalf("refactor-agent: %v", err)
		}
		defer f.Close()
		cfg.Transcript = f
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	rep, err := agent.Run(ctx, cfg)
	if err != nil {
		log.Fatalf("refactor-agent: %v", err)
	}
	printReport(os.Stdout, rep)
	if rep.RolledBack && !*revert {
		os.Exit(1)
	}
}

func defaultHost() string {
	if h := os.Getenv("OLLAMA_HOST"); h != "" {
		if len(h) < 7 || (h[:7] != "http://" && (len(h) < 8 || h[:8] != "https://")) {
			h = "http://" + h // OLLAMA_HOST is often host:port
		}
		return h
	}
	return "http://localhost:11434"
}

func printReport(w io.Writer, rep *agent.Report) {
	fmt.Fprintf(w, "stopped: %s\n", rep.StopReason)
	fmt.Fprintf(w, "tool calls: %d (errors: %d)\n", rep.Steps, rep.Errors)
	for _, line := range counts("applied", rep.Applied) {
		fmt.Fprintln(w, line)
	}
	for _, line := range counts("refused", rep.Refused) {
		fmt.Fprintln(w, line)
	}
	if rep.TestsBefore != "" {
		fmt.Fprintf(w, "tests: %s before, %s after\n", rep.TestsBefore, rep.TestsAfter)
	}
	if rep.RolledBack {
		fmt.Fprintln(w, "all applied steps were undone")
	}
	if rep.Summary != "" {
		fmt.Fprintf(w, "\nmodel's summary:\n%s\n", rep.Summary)
	}
	if rep.Diff != "" {
		fmt.Fprintf(w, "\n%s", rep.Diff)
	} else {
		fmt.Fprintln(w, "\nno changes")
	}
}

func counts(what string, m map[string]int) []string {
	var names []string
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []string
	for _, n := range names {
		out = append(out, fmt.Sprintf("  %s %-17s %d", what, n, m[n]))
	}
	return out
}
