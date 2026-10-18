package tokens

import (
	"encoding/json"
	"strings"
	"testing"
)

// transcript builds Claude Code transcript lines.
type transcript struct{ lines []string }

func (t *transcript) add(v map[string]any) {
	b, _ := json.Marshal(v)
	t.lines = append(t.lines, string(b))
}

// request adds one API response; Claude Code writes a line per content
// block, each repeating the request's usage.
func (t *transcript) request(id string, context, output int, blocks ...map[string]any) {
	usage := map[string]any{"input_tokens": 2, "cache_creation_input_tokens": 100, "cache_read_input_tokens": context - 102, "output_tokens": output}
	for _, b := range blocks {
		t.add(map[string]any{"type": "assistant", "requestId": id, "message": map[string]any{"usage": usage, "content": []any{b}}})
	}
}

func toolUse(id, name string, inputChars int) map[string]any {
	// {"x":"..."} is 8 characters around the padding.
	return map[string]any{"type": "tool_use", "id": id, "name": name, "input": map[string]any{"x": strings.Repeat("a", inputChars-8)}}
}

func (t *transcript) result(id string, chars int) {
	t.add(map[string]any{"type": "user", "message": map[string]any{"content": []any{
		map[string]any{"type": "tool_result", "tool_use_id": id, "content": strings.Repeat("r", chars)},
	}}})
}

func (t *transcript) String() string { return strings.Join(t.lines, "\n") + "\n" }

func stats(t *testing.T, r *Report, name string) ToolStats {
	t.Helper()
	for _, s := range r.Tools {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no stats for %s in %+v", name, r.Tools)
	return ToolStats{}
}

func TestAnalyze(t *testing.T) {
	var tr transcript
	tr.request("r1", 1000, 50, map[string]any{"type": "thinking"}, toolUse("t1", "Read", 20))
	tr.result("t1", 400)
	tr.request("r2", 1250, 30, toolUse("t2", "mcp__gorefactor__rename", 100))
	tr.result("t2", 40)
	tr.request("r3", 1300, 10, map[string]any{"type": "text", "text": "done"})

	r, err := Analyze(strings.NewReader(tr.String()))
	if err != nil {
		t.Fatal(err)
	}
	if r.Requests != 3 || r.BaseContext != 1000 || r.OutputTokens != 90 || r.InputTokens != 3550 {
		t.Errorf("totals: %+v", r)
	}
	// Results: 1250-1000-50 = 200 tokens for 400 chars, 1300-1250-30 = 20
	// for 40, so 0.5 tokens per char, which also sizes the inputs.
	read := stats(t, r, "Read")
	if read != (ToolStats{Name: "Read", Calls: 1, InputTokens: 10, ResultTokens: 200, ContextTokens: 2 * 210}) {
		t.Errorf("Read: %+v", read)
	}
	rename := stats(t, r, "mcp__gorefactor__rename")
	if rename != (ToolStats{Name: "mcp__gorefactor__rename", Calls: 1, InputTokens: 50, ResultTokens: 20, ContextTokens: 70}) {
		t.Errorf("rename: %+v", rename)
	}
	if r.Tools[0].Name != "Read" {
		t.Errorf("tools not sorted by context tokens: %+v", r.Tools)
	}
}

// Results of one turn share its context growth by size.
func TestAnalyzeSplitsATurn(t *testing.T) {
	var tr transcript
	tr.request("r1", 1000, 0, toolUse("a", "Read", 8), toolUse("b", "Bash", 8))
	tr.result("a", 300)
	tr.result("b", 100)
	tr.request("r2", 1200, 0, map[string]any{"type": "text", "text": "ok"})

	r, err := Analyze(strings.NewReader(tr.String()))
	if err != nil {
		t.Fatal(err)
	}
	if got := stats(t, r, "Read").ResultTokens; got != 150 {
		t.Errorf("Read result tokens = %d, want 150", got)
	}
	if got := stats(t, r, "Bash").ResultTokens; got != 50 {
		t.Errorf("Bash result tokens = %d, want 50", got)
	}
}

// A context that shrinks (/clear, compaction) no longer carries what came
// before.
func TestAnalyzeContextReset(t *testing.T) {
	var tr transcript
	tr.request("r1", 1000, 0, toolUse("a", "Read", 8))
	tr.result("a", 200)
	tr.request("r2", 1100, 0, map[string]any{"type": "text", "text": "ok"})
	tr.request("r3", 500, 0, map[string]any{"type": "text", "text": "after clear"})
	tr.request("r4", 600, 0, map[string]any{"type": "text", "text": "more"})

	r, err := Analyze(strings.NewReader(tr.String()))
	if err != nil {
		t.Fatal(err)
	}
	if got := stats(t, r, "Read"); got.ResultTokens != 100 || got.ContextTokens != 104 {
		t.Errorf("Read: %+v, want 100 result tokens seen by one request", got)
	}
}

// Subagent turns are in the same file but are not the session's context.
func TestAnalyzeSkipsSidechains(t *testing.T) {
	var tr transcript
	tr.request("r1", 1000, 0, map[string]any{"type": "text", "text": "hi"})
	tr.add(map[string]any{"type": "assistant", "isSidechain": true, "requestId": "s1", "message": map[string]any{
		"usage": map[string]any{"input_tokens": 5}, "content": []any{toolUse("x", "Read", 8)}}})
	r, err := Analyze(strings.NewReader(tr.String()))
	if err != nil {
		t.Fatal(err)
	}
	if r.Requests != 1 || len(r.Tools) != 0 {
		t.Errorf("sidechain counted: %+v", r)
	}
}

func TestMarkdownGroupsTools(t *testing.T) {
	r := &Report{Requests: 3, Tools: []ToolStats{
		{Name: "Read", Calls: 2, ResultTokens: 300, ContextTokens: 900},
		{Name: "Edit", Calls: 1, InputTokens: 40, ResultTokens: 10, ContextTokens: 50},
		{Name: "mcp__gorefactor__rename", Calls: 1, InputTokens: 20, ResultTokens: 60, ContextTokens: 80},
		{Name: "ToolSearch", Calls: 1, ResultTokens: 5, ContextTokens: 5},
	}}
	md := r.Markdown()
	for _, want := range []string{
		"| gorefactor | 1 | 20 | 60 | 80 |",
		"| by hand (Read, Edit, Write, Bash, Grep, Glob) | 3 | 40 | 310 | 950 |",
		"| other | 1 | 0 | 5 | 5 |",
		"| Read | 2 | 0 | 300 | 900 |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
}
