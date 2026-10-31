package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/tychofreeman/go-refactor-mcp/internal/tokens"
)

func TestEntry(t *testing.T) {
	in := `{"session_id":"s1","transcript_path":"/t.jsonl","cwd":"/repo","hook_event_name":"PostToolUse","tool_name":"mcp__gorefactor__rename","tool_use_id":"u1","tool_input":{"file":"a.go"},"tool_response":[{"type":"text","text":"APPLIED rename"}]}`
	line, err := entry([]byte(in), time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	var e tokens.Event
	if err := json.Unmarshal(line, &e); err != nil {
		t.Fatal(err)
	}
	if e.Hook != "PostToolUse" || e.ToolUseID != "u1" || e.Tool != "mcp__gorefactor__rename" || e.Session != "s1" {
		t.Errorf("entry = %+v", e)
	}
	if e.Result != "APPLIED rename" || e.ResultChars != len("APPLIED rename") || e.Time != "2026-10-31T12:00:00Z" {
		t.Errorf("result %q (%d chars) at %s", e.Result, e.ResultChars, e.Time)
	}
	if string(e.Input) != `{"file":"a.go"}` {
		t.Errorf("Input = %s", e.Input)
	}
	if e.Transcript != "/t.jsonl" || e.Cwd != "/repo" {
		t.Errorf("Transcript = %q, Cwd = %q", e.Transcript, e.Cwd)
	}
	if strings.Contains(string(line), "tool_response") {
		t.Errorf("line keeps the response: %s", line)
	}
}

func TestResponseText(t *testing.T) {
	for _, c := range []struct{ raw, want string }{
		{`"plain"`, "plain"},
		{`[{"type":"text","text":"APPLIED x"},{"type":"text","text":"more"}]`, "APPLIED x\nmore"},
		{`{"content":[{"type":"text","text":"a"}]}`, "a"},
		{`{"stdout":"out","stderr":"err","interrupted":false}`, "out\nerr"},
		{`{"filePath":"x.md"}`, `{"filePath":"x.md"}`},
		{``, ""},
	} {
		if got := responseText([]byte(c.raw)); got != c.want {
			t.Errorf("responseText(%s) = %q, want %q", c.raw, got, c.want)
		}
	}
}

func TestResponseTextCut(t *testing.T) {
	long := strings.Repeat("x", maxResult+10)
	b, _ := json.Marshal(long)
	if got := responseText(b); len(got) != maxResult {
		t.Errorf("len = %d, want %d", len(got), maxResult)
	}
}

func TestLogName(t *testing.T) {
	for branch, want := range map[string]string{"whats-next": "whats-next.jsonl", "a/b": "a-b.jsonl", "HEAD": "detached.jsonl", "": "detached.jsonl"} {
		if got := logName(branch); got != want {
			t.Errorf("logName(%q) = %q, want %q", branch, got, want)
		}
	}
}
