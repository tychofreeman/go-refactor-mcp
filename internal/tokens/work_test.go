package tokens

import (
	"encoding/json"
	"strings"
	"testing"
)

// sessionLog builds the lines of a session log, as cmd/session-log writes
// them.
type sessionLog struct{ lines []string }

func (l *sessionLog) add(hook, id, tool string, input map[string]any, result string) {
	e := map[string]any{"hook_event_name": hook, "tool_use_id": id, "tool_name": tool, "tool_input": input, "cwd": "/repo", "transcript_path": "/t.jsonl"}
	if hook == "PostToolUse" {
		e["result"], e["result_chars"] = result, len(result)
	}
	b, _ := json.Marshal(e)
	l.lines = append(l.lines, string(b))
}

// call logs a call that ran: its PreToolUse and PostToolUse.
func (l *sessionLog) call(id, tool string, input map[string]any, result string) {
	l.add("PreToolUse", id, tool, input, "")
	l.add("PostToolUse", id, tool, input, result)
}

func (l *sessionLog) String() string { return strings.Join(l.lines, "\n") + "\n" }

func TestAnalyzeWork(t *testing.T) {
	var l sessionLog
	l.add("PreToolUse", "t1", "Read", map[string]any{"file_path": "/repo/a.go"}, "")
	l.add("PostToolUse", "t2", "mcp__gorefactor__inspect", map[string]any{"file": "a.go"}, "a.go: 3 functions")
	l.add("PostToolUse", "t3", "mcp__gorefactor__extract_function", map[string]any{"file": "a.go", "start_line": 3, "end_line": 9, "name": "g"}, "REFUSED extract_function: the range cuts a statement")
	l.call("t4", "Edit", map[string]any{"file_path": "/repo/a.go", "old_string": "x", "new_string": "y"}, "ok")
	l.add("PostToolUse", "t5", "mcp__gorefactor__rename", map[string]any{"file": "a.go", "name": "X", "new_name": "Y"}, "APPLIED rename: renamed \"X\" to \"Y\"\n\nchanged lines:\n  a.go: 3-4\n")
	l.call("t6", "Bash", map[string]any{"command": "git commit -m x"}, "")
	l.call("t7", "Bash", map[string]any{"command": "sed -i 's/a/b/' b.go"}, "")
	l.call("t8", "Bash", map[string]any{"command": "go run ./cmd/refactor batch @steps.json"}, "1. rename: renamed")

	w, err := AnalyzeWork(strings.NewReader(l.String()))
	if err != nil {
		t.Fatal(err)
	}
	if w.Calls != 8 {
		t.Errorf("Calls = %d, want 8", w.Calls)
	}
	if want := []string{"Read /repo/a.go"}; !equalStrings(w.NoResult, want) {
		t.Errorf("NoResult = %q, want %q", w.NoResult, want)
	}
	if w.Reads["inspect: file"] != 1 {
		t.Errorf("Reads = %v, want one inspect of a file", w.Reads)
	}
	if w.Refused[RefusedTool] != 1 {
		t.Errorf("Refused = %v, want one gorefactor refusal", w.Refused)
	}
	if want := []string{"Edit /repo/a.go", "Bash: sed -i 's/a/b/' b.go"}; !equalStrings(w.HandWrites, want) {
		t.Errorf("HandWrites = %q, want %q", w.HandWrites, want)
	}
	if want := []string{"extract_function (REFUSED extract_function: the range cuts a statement), then Edit /repo/a.go"}; !equalStrings(w.AfterRefusal, want) {
		t.Errorf("AfterRefusal = %q, want %q", w.AfterRefusal, want)
	}
	if w.RefactoringSteps != 1 || w.CLI != 1 {
		t.Errorf("RefactoringSteps = %d, CLI = %d; want 1 and 1", w.RefactoringSteps, w.CLI)
	}
	md := w.Markdown()
	for _, s := range []string{"- tool calls: 8", "- by hand: 2", "extract_function (REFUSED", "| refactorings | 1 | 0 | 2 | 2 |"} {
		if !strings.Contains(md, s) {
			t.Errorf("Markdown lacks %q:\n%s", s, md)
		}
	}
}

// Go written as text is counted apart from structured edits.
func TestAnalyzeWorkTextEdits(t *testing.T) {
	var l sessionLog
	l.add("PostToolUse", "t1", "mcp__gorefactor__replace_text", map[string]any{"file": "a.go", "old": "x", "new": "y"}, "APPLIED replace_text: replaced 1 occurrence\n")
	l.add("PostToolUse", "t2", "mcp__gorefactor__add_element", map[string]any{"file": "a.go", "name": "m", "element": "1: 2"}, "APPLIED add_element: added an element\n")

	w, err := AnalyzeWork(strings.NewReader(l.String()))
	if err != nil {
		t.Fatal(err)
	}
	if md := w.Markdown(); !strings.Contains(md, "- through gorefactor: 0 refactoring steps, 2 edit steps (1 of them text edits of Go)") {
		t.Errorf("Markdown lacks the text edits:\n%s", md)
	}
}

// A refusal followed by another gorefactor call that applies is not a
// workaround, even if a hand edit comes later.
func TestAnalyzeWorkRefusalThenTool(t *testing.T) {
	var l sessionLog
	l.add("PostToolUse", "t1", "mcp__gorefactor__inline_variable", map[string]any{"file": "a.go"}, "REFUSED inline_variable: assigned later")
	l.add("PostToolUse", "t2", "mcp__gorefactor__inspect", map[string]any{"file": "a.go"}, "a.go")
	l.add("PostToolUse", "t3", "mcp__gorefactor__inline_variable", map[string]any{"file": "a.go", "function": "F"}, "APPLIED inline_variable: inlined\n\nchanged lines:\n  a.go: 4\n")
	l.call("t4", "Write", map[string]any{"file_path": "/repo/notes.go"}, "ok")

	w, err := AnalyzeWork(strings.NewReader(l.String()))
	if err != nil {
		t.Fatal(err)
	}
	if len(w.AfterRefusal) != 0 {
		t.Errorf("AfterRefusal = %q, want none", w.AfterRefusal)
	}
	if len(w.HandWrites) != 1 {
		t.Errorf("HandWrites = %q, want the Write", w.HandWrites)
	}
}

func equalStrings(a, b []string) bool {
	return strings.Join(a, "\x00") == strings.Join(b, "\x00") && len(a) == len(b)
}

func TestAnalyzeWorkLargeWrites(t *testing.T) {
	lines := func(n int) string { return strings.Repeat("x\n", n) }
	var l sessionLog
	l.add("PostToolUse", "t1", "mcp__gorefactor__create_file", map[string]any{"file": "internal/x.go", "content": lines(30), "overwrite": true}, "APPLIED create_file: replaced internal/x.go (30 lines)")
	l.add("PostToolUse", "t2", "mcp__gorefactor__replace_statements", map[string]any{"file": "a.go", "function": "F", "content": lines(25)}, "APPLIED replace_statements: replaced the body of F in a.go")
	l.add("PostToolUse", "t3", "mcp__gorefactor__replace_text", map[string]any{"file": "a.go", "old": "a", "new": "b\nc"}, "APPLIED replace_text: replaced 1 occurrence")
	l.call("t4", "Write", map[string]any{"file_path": "/repo/b.go", "content": lines(40)}, "ok")
	l.call("t5", "Write", map[string]any{"file_path": "/tmp/steps.json", "content": lines(3)}, "ok")
	l.call("t6", "Bash", map[string]any{"command": "cat > c.go <<'EOF'\n" + lines(24) + "EOF"}, "")
	w, err := AnalyzeWork(strings.NewReader(l.String()))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"create_file internal/x.go: 30 lines, replacing the file",
		"replace_statements a.go: 25 lines",
		"Write /repo/b.go: 40 lines",
		"Bash: cat > c.go <<'EOF': 25 lines",
	}
	if !equalStrings(w.LargeWrites, want) {
		t.Errorf("LargeWrites = %q, want %q", w.LargeWrites, want)
	}
	if want := []string{"Write /tmp/steps.json"}; !equalStrings(w.ScratchWrites, want) {
		t.Errorf("ScratchWrites = %q, want %q", w.ScratchWrites, want)
	}
	if want := []string{"Write /repo/b.go", "Bash: cat > c.go <<'EOF'"}; !equalStrings(w.HandWrites, want) {
		t.Errorf("HandWrites = %q, want %q", w.HandWrites, want)
	}
	if !w.ToolUseIDs["t1"] || len(w.ToolUseIDs) != 6 {
		t.Errorf("ToolUseIDs = %v", w.ToolUseIDs)
	}
	if want := []string{"/t.jsonl"}; !equalStrings(w.Transcripts, want) {
		t.Errorf("Transcripts = %q, want %q", w.Transcripts, want)
	}
	if md := w.Markdown(); !strings.Contains(md, "- large writes (20 lines or more in one call): 4") || !strings.Contains(md, "- written to scratch files: 1") {
		t.Errorf("Markdown lacks the large or scratch writes:\n%s", md)
	}
}
