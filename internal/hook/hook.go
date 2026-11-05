// Package hook holds what the repository's Claude Code hooks share: the
// call a PreToolUse hook is given, the decision that denies it, and how a
// Bash command line is split into the commands and files it names.
package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// decision is a PreToolUse hook's answer that denies the call, with the
// reason Claude Code shows the model.
type decision struct {
	HookSpecificOutput struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
	} `json:"hookSpecificOutput"`
}

// Deny answers the hook with a decision that refuses the call, giving
// reason to the model.
func Deny(reason string) {
	// A decision on stdout with exit status 0, since go run turns the
	// program's exit status 2, the other way to deny, into 1, which Claude
	// Code takes for the hook's own failure.
	var out decision
	out.HookSpecificOutput.HookEventName = "PreToolUse"
	out.HookSpecificOutput.PermissionDecision = "deny"
	out.HookSpecificOutput.PermissionDecisionReason = reason
	json.NewEncoder(os.Stdout).Encode(out)
}

// Call is the part of a hook's input the guards look at.
type Call struct {
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		FilePath string `json:"file_path"`
		Command  string `json:"command"`
	} `json:"tool_input"`
	Cwd string `json:"cwd"`
}

// Commands splits a shell command line into its simple commands, each
// as its words with quotes removed: at ;, &, |, newlines, parentheses,
// backquotes and $(, outside quotes. Redirections count as spaces, so
// that cat < f.go names f.go. It is not a shell parser, only enough to
// find what each command is given.
func Commands(line string) [][]string {
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

// GoSource reports whether path names a Go file of the module: a .go
// file not under testdata/.
func GoSource(path string) bool {
	return strings.HasSuffix(path, ".go") && !strings.Contains("/"+filepath.ToSlash(path), "/testdata/")
}

// Each judges the simple commands of a Bash call c, with a function that
// says where an operand points: from c's working directory after the cds
// before it, with the variables the line set expanded. It is the first
// answer of check that is not "".
func Each(c Call, check func(words []string, at func(p string) string) string) string {
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
	for _, words := range Commands(c.ToolInput.Command) {
		if len(words) == 2 && words[0] == "cd" {
			dir = at(words[1])
			continue
		}
		if name, value, ok := strings.Cut(words[0], "="); ok && len(words) == 1 && name != "" && !strings.ContainsAny(name, "-$") {
			vars[name] = value
			continue
		}
		if why := check(words, at); why != "" {
			return why
		}
	}
	return ""
}
