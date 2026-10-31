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
)

// call is the part of a hook's input read-guard looks at.
type call struct {
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		FilePath string `json:"file_path"`
		Command  string `json:"command"`
	} `json:"tool_input"`
	Cwd string `json:"cwd"`
}

// decision is a PreToolUse hook's answer that denies the call, with the
// reason Claude Code shows the model.
type decision struct {
	HookSpecificOutput struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
	} `json:"hookSpecificOutput"`
}

func main() {
	var c call
	if err := json.NewDecoder(os.Stdin).Decode(&c); err != nil {
		return // not a call to judge
	}
	why := refusal(c)
	if why == "" {
		return
	}
	// A decision on stdout with exit status 0, since go run turns the
	// program's exit status 2, the other way to deny, into 1, which Claude
	// Code takes for the hook's own failure.
	var out decision
	out.HookSpecificOutput.HookEventName = "PreToolUse"
	out.HookSpecificOutput.PermissionDecision = "deny"
	_, err := exec.LookPath("refactor")
	out.HookSpecificOutput.PermissionDecisionReason = why + "\n" + adviceFor(err == nil)
	json.NewEncoder(os.Stdout).Encode(out)
}

// refusal says why c reads Go source other than through inspect, or is
// "" when it does not.
func refusal(c call) string {
	switch c.ToolName {
	case "Read":
		if goSource(c.ToolInput.FilePath) {
			return "Read of " + c.ToolInput.FilePath + " refused."
		}
	case "Bash":
		dir, vars := c.Cwd, map[string]string{}
		at := func(p string) string { // where p points, from dir
			p = os.Expand(p, func(k string) string {
				if v, ok := vars[k]; ok {
					return v
				}
				return "$" + k
			})
			if dir != "" && !filepath.IsAbs(p) {
				p = filepath.Join(dir, p)
			}
			return p
		}
		for _, words := range commands(c.ToolInput.Command) {
			if len(words) == 2 && words[0] == "cd" {
				dir = at(words[1])
				continue
			}
			if name, value, ok := strings.Cut(words[0], "="); ok && len(words) == 1 && name != "" && !strings.ContainsAny(name, "-$") {
				vars[name] = value
				continue
			}
			if f := readsGo(words, at); f != "" {
				return fmt.Sprintf("%s of %s refused.", words[0], f)
			}
		}
	}
	return ""
}

// goSource reports whether path names a Go file of the module: a .go
// file not under testdata/.
func goSource(path string) bool {
	return strings.HasSuffix(path, ".go") && !strings.Contains("/"+filepath.ToSlash(path), "/testdata/")
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
		if goSource(at(o)) {
			return o
		}
	}
	return ""
}

// commands splits a shell command line into its simple commands, each
// as its words with quotes removed: at ;, &, |, newlines, parentheses,
// backquotes and $(, outside quotes. Redirections count as spaces, so
// that cat < f.go names f.go. It is not a shell parser, only enough to
// find what each command is given.
func commands(line string) [][]string {
	var out [][]string
	var words []string
	var word strings.Builder
	inWord := false
	endWord := func() {
		if inWord {
			words = append(words, word.String())
			word.Reset()
			inWord = false
		}
	}
	endCommand := func() {
		endWord()
		if len(words) > 0 {
			out = append(out, words)
			words = nil
		}
	}
	var quote rune
	rs := []rune(line)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				word.WriteRune(r)
			}
		case quote == '"':
			switch {
			case r == '"':
				quote = 0
			case r == '\\' && i+1 < len(rs):
				i++
				word.WriteRune(rs[i])
			default:
				word.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == '\\' && i+1 < len(rs):
			i++
			word.WriteRune(rs[i])
			inWord = true
		case strings.ContainsRune(";&|()`\n", r) || r == '$' && i+1 < len(rs) && rs[i+1] == '(':
			endCommand()
		case r == ' ' || r == '\t' || r == '<' || r == '>':
			endWord()
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	endCommand()
	return out
}

// adviceFor says how to read Go instead, naming the CLI when cli says it
// is installed.
func adviceFor(cli bool) string {
	a := `Read Go through inspect: a file's outline (inspect with file), declarations by name (names: ["F", "T.M"]), or lines (targets with file, start_line and end_line).`
	if cli {
		a += ` From the CLI: refactor inspect file=PATH, or refactor inspect '{"names":["F"]}'.`
	}
	return a + ` Files under testdata/ may be read directly.`
}
