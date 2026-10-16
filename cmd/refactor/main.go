// Command refactor runs one refactoring step from the command line. It
// uses the same engine and arguments as the MCP server will, so it is the
// easiest way to try and debug a refactoring by hand.
//
//	refactor list
//	refactor [-C dir] [-n] rename file=main.go line=7 name=t new_name=total
//	refactor rename '{"file":"main.go","line":7,"name":"t","new_name":"total"}'
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

	"github.com/cwfreeman/gorefactor/internal/refactor"
	"github.com/cwfreeman/gorefactor/internal/refactorings"
	"github.com/cwfreeman/gorefactor/internal/workspace"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	fs := flag.NewFlagSet("refactor", flag.ContinueOnError)
	dir := fs.String("C", ".", "workspace root (module directory)")
	pattern := fs.String("p", "./...", "package pattern to load")
	dryRun := fs.Bool("n", false, "dry run: print the diff without writing")
	asJSON := fs.Bool("json", false, "print the result as JSON")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: refactor [flags] list\n       refactor [flags] <refactoring> key=value... | '<json>'\n\nflags:\n")
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
	res, err := refactor.Run(ws, r, args, refactor.Options{DryRun: *dryRun})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(res)
	} else {
		fmt.Print(res.String())
	}
	if res.Refused != "" {
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
