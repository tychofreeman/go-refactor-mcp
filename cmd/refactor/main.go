// Command refactor runs a refactoring step, or a batch of them, from the
// command line. It runs them as the MCP server does, with the same
// arguments, and declares what an edit calls as the server does, so it is
// the easiest way to try and debug a refactoring by hand.
//
//	refactor list
//	refactor [-C dir] [-n] rename file=main.go line=7 name=t new_name=total
//	refactor rename '{"file":"main.go","line":7,"name":"t","new_name":"total"}'
//	refactor batch '{"steps":[{"tool":"rename","args":{...}}, ...]}'
//
// Exit status: 0 applied (or previewed), 2 refused, 1 error.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	fs := flag.NewFlagSet("refactor", flag.ContinueOnError)
	dir := fs.String("C", ".", "workspace root (module directory)")
	pattern := fs.String("p", "./...", "package pattern to load")
	dryRun := fs.Bool("n", false, "dry run: print the diff without writing")
	asJSON := fs.Bool("json", false, "print the result as JSON (without declaring what an edit calls)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: refactor [flags] list\n       refactor [flags] <refactoring> key=value... | '<json>'\n       refactor [flags] batch '<json>'\n\nflags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(argv); err != nil {
		return 1
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return 1
	}

	name := fs.Arg(0)
	if name == "list" {
		for _, r := range refactorings.All() {
			fmt.Printf("%s\n  %s\n\n", r.Name(), strings.ReplaceAll(r.Description(), "\n", "\n  "))
		}
		return 0
	}
	if name == "batch" {
		return runBatch(fs.Args()[1:], *dir, *pattern, *dryRun)
	}
	r, ok := refactorings.Lookup(name)
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown refactoring %q (try: refactor list)\n", name)
		return 1
	}
	args, err := parseArgs(fs.Args()[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	ws, err := workspace.Load(*dir, *pattern)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if *asJSON {
		// The result of the step alone, without the retry that declares
		// what an edit calls.
		res, err := refactor.Run(ws, r, args, refactor.Options{DryRun: *dryRun})
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(res)
		if res.Refused != "" {
			return 2
		}
		return 0
	}
	text, refused, err := refactorings.RunStep(ws, r, args, refactor.Options{DryRun: *dryRun})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	fmt.Print(text)
	if refused {
		return 2
	}
	return 0
}

// runBatch runs the batch tool: argv is its arguments as one JSON object.
func runBatch(argv []string, dir, pattern string, dryRun bool) int {
	if dryRun {
		fmt.Fprintln(os.Stderr, "-n does not apply to batch: a batch writes each step before the next")
		return 1
	}
	if len(argv) != 1 {
		fmt.Fprintln(os.Stderr, `batch takes one JSON object: '{"steps":[{"tool":...,"args":{...}}, ...]}'`)
		return 1
	}
	batch, refused := refactorings.ParseBatch(json.RawMessage(argv[0]))
	if refused != "" {
		fmt.Println(refused)
		return 2
	}
	ws, err := workspace.Load(dir, pattern)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	b, err := refactor.RunBatch(ws, batch.Steps, refactor.Options{Unchecked: batch.Unchecked, Declare: refactorings.Declarer()})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if batch.Summary {
		b.Diff = ""
	}
	fmt.Print(b.String())
	if !b.Applied {
		return 2
	}
	return 0
}

// parseArgs accepts either a single JSON object or key=value pairs; values
// that look like integers or booleans become JSON numbers/booleans.
func parseArgs(kv []string) (json.RawMessage, error) {
	if len(kv) == 1 && strings.HasPrefix(strings.TrimSpace(kv[0]), "{") {
		if !json.Valid([]byte(kv[0])) {
			return nil, fmt.Errorf("invalid JSON arguments")
		}
		return json.RawMessage(kv[0]), nil
	}
	m := map[string]any{}
	for _, s := range kv {
		k, v, ok := strings.Cut(s, "=")
		if !ok {
			return nil, fmt.Errorf("argument %q is not key=value", s)
		}
		if n, err := strconv.Atoi(v); err == nil {
			m[k] = n
		} else if v == "true" || v == "false" { // not ParseBool: it accepts "t", a plausible name
			m[k] = v == "true"
		} else {
			m[k] = v
		}
	}
	return json.Marshal(m)
}
