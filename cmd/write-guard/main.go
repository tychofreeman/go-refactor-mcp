// Command write-guard is a Claude Code hook (PreToolUse, on Edit, Write,
// MultiEdit and Bash) that refuses changing Go source other than through
// gorefactor's refactorings and edits: Edit, Write or MultiEdit of a .go
// file, or a Bash command that writes one (sed -i, perl -i, tee, cp, mv,
// rm, or a redirection into it). Files under testdata/ are fixtures and
// may be written so; gofmt -w and git may change Go files.
//
// It is an experiment (2026-11-05, the user): the read guard ended reading
// Go by shell in this repo's sessions; this asks whether a check ends
// writing it by hand too, and what the friction log then records.
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
	hook.Deny(why + "\n" + adviceFor(err == nil))
}

// refusal says why c changes Go source other than through gorefactor, or
// is "" when it does not.
func refusal(c hook.Call) string {
	switch c.ToolName {
	case "Edit", "Write", "MultiEdit":
		if hook.GoSource(c.ToolInput.FilePath) {
			return c.ToolName + " of " + c.ToolInput.FilePath + " refused."
		}
	case "Bash":
		return hook.Each(c, writeRefusal)
	}
	return ""
}

// writeRefusal says why the command words writes Go source, or is "".
func writeRefusal(words []string, at func(p string) string) string {
	if f := writesGo(words, at); f != "" {
		return fmt.Sprintf("%s writing %s refused.", words[0], f)
	}
	return ""
}

// writesGo is the Go source file the command words writes, or ""; at says
// where an operand points.
func writesGo(words []string, at func(string) string) string {
	for len(words) > 0 && strings.Contains(words[0], "=") && !strings.HasPrefix(words[0], "-") {
		words = words[1:] // VAR=value before the command
	}
	if len(words) == 0 {
		return ""
	}
	inPlace := false
	var operands []string
	for i := 1; i < len(words); i++ {
		w := words[i]
		switch {
		case strings.HasSuffix(w, ">") && i+1 < len(words): // a redirection into the next word
			i++
			if hook.GoSource(at(words[i])) {
				return words[i]
			}
		case strings.Contains(w, ">"): // 2>&1
		case w == "--in-place" || strings.HasPrefix(w, "-i"):
			inPlace = true
		case strings.HasPrefix(w, "-") && !strings.HasPrefix(w, "--") && strings.Contains(w, "i"):
			inPlace = inPlace || filepath.Base(words[0]) == "perl" // perl -pi
		case strings.HasPrefix(w, "-") && w != "-":
		default:
			operands = append(operands, w)
		}
	}
	switch filepath.Base(words[0]) {
	case "sed", "perl":
		if !inPlace {
			return ""
		}
	case "tee", "mv", "rm":
	case "cp", "install", "ln":
		if len(operands) == 0 {
			return ""
		}
		operands = operands[len(operands)-1:] // the destination
	default:
		return ""
	}
	for _, o := range operands {
		if hook.GoSource(at(o)) {
			return o
		}
	}
	return ""
}

// adviceFor says how to change Go instead, naming the CLI when cli says it
// is installed.
func adviceFor(cli bool) string {
	a := `Change Go through gorefactor: a refactoring, or an edit (add_declaration, replace_declaration, replace_statements, insert_statements, replace_expression, add_case, add_field, add_element; create_file for a new file, move_file and delete_file for files), several as one batch. Where no tool or sequence of tools makes the change, say so in docs/friction-log.md and ask the user.`
	if cli {
		a += ` From the CLI: refactor batch @steps.json.`
	}
	return a + ` Files under testdata/ may be written directly.`
}
