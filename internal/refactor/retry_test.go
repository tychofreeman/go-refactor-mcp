package refactor

import (
	"encoding/json"
	"strings"
	"testing"
)

// A retry is the kept batch with only the changed steps sent, numbered as
// in the kept batch.
func TestRetry(t *testing.T) {
	var r Retry
	if _, refused := r.Args(json.RawMessage(`{"retry": true, "steps": [{"step": 1, "args": {}}]}`)); !strings.Contains(refused, "no refused batch") {
		t.Errorf("a retry with nothing kept: %q", refused)
	}
	batch := json.RawMessage(`{"steps": [{"tool": "a", "args": {"n": 1}}, {"tool": "b", "args": {"n": 2}}], "check": "end"}`)
	if hint := r.Keep(batch, true); !strings.Contains(hint, `"retry": true`) {
		t.Errorf("the refusal does not say how to retry: %q", hint)
	}
	got, refused := r.Args(json.RawMessage(`{"retry": true, "steps": [{"step": 2, "args": {"n": 3}}, {"after": 0, "tool": "c", "args": {}}, {"after": 2, "tool": "d", "args": {}}]}`))
	want := `{"check":"end","steps":[{"args":{},"tool":"c"},{"args":{"n":1},"tool":"a"},{"args":{"n":3},"tool":"b"},{"args":{},"tool":"d"}]}`
	if refused != "" || string(got) != want {
		t.Errorf("retry: %q\n got %s\nwant %s", refused, got, want)
	}
	for _, c := range []struct{ args, refusal string }{
		{`{"retry": true, "steps": [{"args": {}}]}`, "one of step and after"},
		{`{"retry": true, "steps": [{"step": 1, "after": 1}]}`, "one of step and after"},
		{`{"retry": true, "steps": [{"step": 3, "args": {}}]}`, "from 1 to 2"},
		{`{"retry": true, "steps": [{"after": 3, "tool": "x"}]}`, "from 0 to 2"},
	} {
		if _, refused := r.Args(json.RawMessage(c.args)); !strings.Contains(refused, c.refusal) {
			t.Errorf("%s: %q, want %q", c.args, refused, c.refusal)
		}
	}
	if got, _ := r.Args(json.RawMessage(`{"retry": false, "steps": [{"tool": "a", "args": {}}]}`)); strings.Contains(string(got), "retry") {
		t.Errorf("retry false is passed on: %s", got)
	}
	r.Keep(batch, false)
	if r.Kept != nil {
		t.Error("a batch that applied did not forget the kept one")
	}
}
