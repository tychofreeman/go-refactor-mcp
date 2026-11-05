// Command read-guard is a Claude Code hook (PreToolUse, on Read and
// Bash) that refuses reading Go source other than through gorefactor's
// inspect: Read of a .go file, or a Bash command in which sed, cat, head,
// tail, less, more, nl, awk or grep names one. Files under testdata/ are
// fixtures, not the module's source, and may be read so. A refused call
// is denied, and Claude Code shows the model what to use instead.
//
// Sessions read whole files and line ranges by habit even when told to
// read through inspect (docs/session-notes.md, 2026-10-30); a check holds
// where a resolution did not.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/hook"
)

func main() {
	var c hook.Call
	if err := json.NewDecoder(os.Stdin).Decode(&c); err != nil {
		return // not a call to judge
	}
	why := refusal(c)
	if why == "" {
		return
	}
	_, err := exec.LookPath("refactor")
	reason := why + "\n" + adviceFor(err == nil)
	hook.Deny(reason)
}

// refusal says why c reads Go source other than through inspect, or is
// "" when it does not.
func refusal(c hook.Call) string {
	switch c.ToolName {
	case "Read":
		if hook.GoSource(c.ToolInput.FilePath) {
			return "Read of " + c.ToolInput.FilePath + " refused."
		}
	case "Bash":
		if why := hook.Each(c, readRefusal); why != "" {
			return why
		}
	}
	return ""
}

// readRefusal says why the command words reads Go source, or is "".
func readRefusal(words []string, at func(p string) string) string {
	if f := readsGo(words, at); f != "" {
		return fmt.Sprintf("%s of %s refused.", words[0], f)
	}
	return ""
}

// readers are the commands that show a file's text. grep's first
// operand is its pattern, unless -e or -f gives it.
var readers = map[string]bool{"sed": true, "cat": true, "head": true, "tail": true, "less": true, "more": true, "nl": true, "awk": true, "grep": true, "egrep": true, "fgrep": true, "rg": true}

// valued are grep's and rg's flags whose value is the next word.
var valued = map[string]bool{"-e": true, "-f": true, "-A": true, "-B": true, "-C": true, "-m": true, "-g": true, "-t": true, "-T": true, "-d": true, "-D": true}

// readsGo is the Go source file the command words reads, or ""; at
// says where an operand points.
func readsGo(words []string, at func(string) string) string {
	for len(words) > 0 && strings.Contains(words[0], "=") && !strings.HasPrefix(words[0], "-") {
		words = words[1:] // VAR=value before the command
	}
	if len(words) == 0 {
		return ""
	}
	name := filepath.Base(words[0])
	if !readers[name] {
		return ""
	}
	grep := strings.HasSuffix(name, "grep") || name == "rg"
	pattern := grep
	var operands []string
	for i := 1; i < len(words); i++ {
		w := words[i]
		switch {
		case strings.Contains(w, ">"): // a redirection: what it writes is not read
			if strings.HasSuffix(w, ">") {
				i++
			}
		case grep && (w == "-e" || w == "-f"):
			pattern = false
			i++
		case grep && valued[w]:
			i++
		case strings.HasPrefix(w, "-") && w != "-":
		case pattern:
			pattern = false
		default:
			operands = append(operands, w)
		}
	}
	for _, o := range operands {
		if hook.GoSource(at(o)) {
			return o
		}
	}
	return ""
}

// adviceFor says how to read Go instead, naming the CLI when cli says it
// is installed.
func adviceFor(cli bool) string {
	a := `Read Go through inspect: a file's outline (inspect with file), declarations by name (names: ["F", "T.M"]), the lines that match a regular expression (pattern, with or without file), or lines (targets with file, start_line and end_line).`
	if cli {
		a += ` From the CLI: refactor inspect file=PATH, or refactor inspect '{"names":["F"]}'.`
	}
	return a + ` Files under testdata/ may be read directly.`
}
