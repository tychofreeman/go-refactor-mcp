package agent

import (
	"encoding/json"
	"testing"
)

func TestCoerceArgs(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"file":{"type":"string"},"line":{"type":"integer"},"keep_else":{"type":"boolean"}}}`)
	for _, tc := range []struct{ in, want string }{
		{`{"file":"a.go","line":"12"}`, `{"file":"a.go","line":12}`},
		{`{"file":"a.go","line":12}`, `{"file":"a.go","line":12}`},
		{`"{\"file\":\"a.go\",\"line\":3}"`, `{"file":"a.go","line":3}`},
		{`{"keep_else":"true"}`, `{"keep_else":true}`},
		{`{"line":"twelve"}`, `{"line":"twelve"}`}, // left for the tool to refuse
		{`{"fil":"a.go"}`, `{"fil":"a.go"}`},       // unknown names pass through
		{``, `{}`},
		{`null`, `{}`},
	} {
		got, err := coerceArgs(json.RawMessage(tc.in), schema)
		if err != nil {
			t.Errorf("%s: %v", tc.in, err)
			continue
		}
		if string(got) != tc.want {
			t.Errorf("%s: got %s, want %s", tc.in, got, tc.want)
		}
	}
	if _, err := coerceArgs(json.RawMessage(`[1,2]`), schema); err == nil {
		t.Error("an array should be an error")
	}
}
