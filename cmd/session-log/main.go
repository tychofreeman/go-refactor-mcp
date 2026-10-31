// Command session-log is a Claude Code hook (PreToolUse and PostToolUse)
// that appends each tool call, with the session's transcript and working
// directory, to a log of the branch being worked on,
// .claude/session-log/<branch>.jsonl, gitignored. A call logged before it
// ran and never after was denied (by read-guard or the user) or failed.
// `refactor-tokens -log` reports on a log: what went through gorefactor,
// what was done by hand, by hand after a refusal, or in large writes, and
// the tokens of those calls from the transcript. The
// whats-next skill (.claude/skills/whats-next) writes that report into
// the item's session record in docs/sessions/.
//
// It never blocks a call: whatever goes wrong, it exits 0 and prints
// nothing.
package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tychofreeman/go-refactor-mcp/internal/tokens"
)

// maxResult is how much of a result's text a line keeps.
const maxResult = 50000

func main() {
	in, err := io.ReadAll(os.Stdin)
	if err != nil {
		return
	}
	line, err := entry(in, time.Now())
	if err != nil {
		return
	}
	var h struct {
		Cwd string `json:"cwd"`
	}
	json.Unmarshal(in, &h)
	root := os.Getenv("CLAUDE_PROJECT_DIR")
	if root == "" {
		root = h.Cwd
	}
	dir := filepath.Join(root, ".claude", "session-log")
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, logName(branch(root))), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(append(line, '\n'))
}

// entry is the log line for a hook's input in: the call, with the text of
// its result in place of the response, and the time.
func entry(in []byte, now time.Time) ([]byte, error) {
	var h struct {
		tokens.Event
		Response json.RawMessage `json:"tool_response"`
	}
	if err := json.Unmarshal(in, &h); err != nil {
		return nil, err
	}
	e := h.Event
	e.Time = now.UTC().Format(time.RFC3339)
	if len(h.Response) > 0 {
		e.ResultChars = len(fullText(h.Response))
		e.Result = responseText(h.Response)
	}
	return json.Marshal(e)
}

// responseText is the text of a tool's response, cut to maxResult.
func responseText(raw []byte) string {
	s := fullText(raw)
	if len(s) > maxResult {
		s = s[:maxResult]
	}
	return s
}

// fullText is the text of a tool's response: a string, the text blocks
// of an MCP result, a command's output, or else the JSON itself.
func fullText(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var texts []string
		for _, b := range blocks {
			texts = append(texts, b.Text)
		}
		return strings.Join(texts, "\n")
	}
	var o struct {
		Content json.RawMessage `json:"content"`
		Stdout  *string         `json:"stdout"`
		Stderr  string          `json:"stderr"`
	}
	if json.Unmarshal(raw, &o) == nil {
		switch {
		case len(o.Content) > 0:
			return fullText(o.Content)
		case o.Stdout != nil:
			return strings.TrimRight(*o.Stdout+"\n"+o.Stderr, "\n")
		}
	}
	return string(raw)
}

// branch is the branch checked out in dir, or "" if none.
func branch(dir string) string {
	out, err := exec.Command("git", "-C", dir, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// logName is the log file of a branch: "detached" when no branch is
// checked out.
func logName(branch string) string {
	if branch == "" || branch == "HEAD" {
		branch = "detached"
	}
	return strings.ReplaceAll(branch, "/", "-") + ".jsonl"
}
