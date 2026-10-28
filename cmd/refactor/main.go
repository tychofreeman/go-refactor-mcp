// Command refactor runs a refactoring step, or a batch of them, from the
// command line, and reads code as inspect does. It runs them as the MCP server does, with the same
// arguments, and declares what an edit calls as the server does, so it is
// the easiest way to try and debug a refactoring by hand.
//
//	refactor list
//	refactor [-C dir] [-n] rename file=main.go line=7 name=t new_name=total
//	refactor rename '{"file":"main.go","line":7,"name":"t","new_name":"total"}'
//	refactor batch '{"steps":[{"tool":"rename","args":{...}}, ...]}'
//	refactor batch @steps.json      (the JSON from a file; - reads stdin)
//	refactor inspect '{"names":["Settle","Ledger.Post"]}'   (or file=a.go, ...)
//	refactor check run_tests=true    (type-checks, then go test ./...)
//	refactor undo                    (reverts the last step or batch applied)
//
// The undo stack is kept between invocations, a file per module in the
// user's cache directory.
//
// Exit status: 0 applied (or previewed, or the check passed), 2 refused
// (or the check failed), 1 error.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/inspect"
	"github.com/tychofreeman/go-refactor-mcp/internal/mcpserver"
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
		fmt.Fprintf(fs.Output(), "usage: refactor [flags] list\n       refactor [flags] <refactoring> key=value... | '<json>' | @file | -\n       refactor [flags] batch '<json>' | @file | -\n       refactor [flags] inspect key=value... | '<json>' | @file | -\n       refactor [flags] check [run_tests=true] [packages=./...]\n       refactor [flags] undo\n\n@file reads the JSON from a file, - from stdin.\n\nflags:\n")
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
	rest, err := jsonFrom(fs.Args()[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if name == "list" {
		for _, r := range refactorings.All() {
			fmt.Printf("%s\n  %s\n\n", r.Name(), strings.ReplaceAll(r.Description(), "\n", "\n  "))
		}
		return 0
	}
	if name != "check" && name != "undo" {
		// TypeScript's files go to the server's languages, as a tool
		// call does; Go's to the module loaded here.
		if raw, err := parseArgs(rest); err == nil {
			if srv := mcpserver.New(*dir); srv.LanguageFor(raw) == "TypeScript" {
				if *dryRun {
					fmt.Fprintln(os.Stderr, "-n does not apply to TypeScript files")
					return 1
				}
				text, refused, err := srv.Call(name, raw)
				if err != nil {
					fmt.Fprintln(os.Stderr, "error:", err)
					return 1
				}
				fmt.Fprint(stdout, text)
				if refused {
					return 2
				}
				return 0
			}
		}
	}
	if name == "batch" {
		return runBatch(rest, *dir, *pattern, *dryRun)
	}
	if name == "inspect" {
		return runInspect(rest, *dir, *pattern)
	}
	if name == "check" {
		return runCheck(rest, *dir, *pattern)
	}
	if name == "undo" {
		return runUndo(*dir, *pattern)
	}
	r, ok := refactorings.Lookup(name)
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown refactoring %q (try: refactor list)\n", name)
		return 1
	}
	args, err := parseArgs(rest)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	ws, err := workspace.Load(*dir, *pattern)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	save, err := keepUndo(ws)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer save()
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
	save, err := keepUndo(ws)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer save()
	b, err := refactor.RunBatch(ws, batch.Steps, refactor.Options{Unchecked: batch.Unchecked, Declare: refactorings.Declarer(), Imports: refactorings.Importer()})
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

// runInspect reads code as the inspect tool does: argv is its arguments,
// one JSON object or key=value pairs. A refused read exits with status 2.
func runInspect(argv []string, dir, pattern string) int {
	args, err := parseArgs(argv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var q inspect.Query
	if err := refactor.DecodeArgs(args, &q); err != nil {
		fmt.Fprintln(stdout, err)
		return 2
	}
	ws, err := workspace.Load(dir, pattern)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	out, err := inspect.Run(ws, q)
	switch {
	case refactor.IsRefusal(err):
		fmt.Fprintln(stdout, err)
		return 2
	case err != nil:
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	fmt.Fprint(stdout, out)
	return 0
}

// runCheck reports, as the check tool does, whether the module
// type-checks and, with run_tests, whether its tests pass: argv is its
// arguments, one JSON object or key=value pairs. A failure exits with
// status 2.
func runCheck(argv []string, dir, pattern string) int {
	args, err := parseArgs(argv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ws, err := workspace.Load(dir, pattern)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	report, passed, err := refactor.CheckReport(ws.Errors(), ws.Root, args)
	if err != nil {
		fmt.Fprintln(stdout, err)
		return 2
	}
	fmt.Fprint(stdout, report)
	if !passed {
		return 2
	}
	return 0
}

// undoFile is where the CLI keeps the undo stack of the module at root,
// so that undo reverts what an earlier invocation applied: a file per
// module, in the user's cache directory.
func undoFile(root string) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(root))
	return filepath.Join(cache, "gorefactor", "undo", hex.EncodeToString(sum[:8])+".json"), nil
}

// keepUndo loads the undo stack earlier invocations left for ws's module,
// and returns the function that saves it once this invocation's changes
// are made.
func keepUndo(ws *workspace.Workspace) (func(), error) {
	path, err := undoFile(ws.Root)
	if err != nil {
		return nil, err
	}
	if err := ws.LoadUndo(path); err != nil {
		return nil, err
	}
	return func() {
		if err := ws.SaveUndo(path); err != nil {
			fmt.Fprintln(os.Stderr, "the undo stack was not saved:", err)
		}
	}, nil
}

// runUndo reverts the last step or batch an invocation applied to the
// module, as the undo tool does for a session. It exits with status 2
// when there is none, or it cannot be undone.
func runUndo(dir, pattern string) int {
	ws, err := workspace.Load(dir, pattern)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	save, err := keepUndo(ws)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer save()
	if !ws.CanUndo() {
		fmt.Fprintln(stdout, "Nothing to undo: no step or batch applied to this module with the CLI is left to revert.")
		return 2
	}
	c, err := ws.Undo()
	if c.Label == "" { // not undone
		fmt.Fprintln(stdout, "Cannot undo: "+err.Error())
		return 2
	}
	fmt.Fprintf(stdout, "UNDONE %s\n\n%s", strings.TrimPrefix(c.Label, "undo "), c.Diff)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
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

// stdin is what an argument "-" reads.
var stdin io.Reader = os.Stdin

// stdout is where inspect writes what it shows.
var stdout io.Writer = os.Stdout

// jsonFrom replaces an only argument "@path" with the file's contents,
// and "-" with stdin's, so that long JSON needs no shell quoting.
func jsonFrom(argv []string) ([]string, error) {
	if len(argv) != 1 {
		return argv, nil
	}
	var data []byte
	var err error
	switch a := argv[0]; {
	case a == "-":
		data, err = io.ReadAll(stdin)
	case strings.HasPrefix(a, "@"):
		data, err = os.ReadFile(a[1:])
	default:
		return argv, nil
	}
	if err != nil {
		return nil, err
	}
	return []string{string(data)}, nil
}
