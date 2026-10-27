package replaceexpr

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

func garden(t *testing.T) (*workspace.Workspace, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("../../inspect/testdata/garden")); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return ws, dir
}

func run(t *testing.T, ws *workspace.Workspace, args string) *refactor.Result {
	t.Helper()
	res, err := refactor.Run(ws, New(), json.RawMessage(`{"file": "src/garden.ts", `+args[1:]), refactor.OptionsOf[*workspace.Workspace]{})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func source(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "src/garden.ts"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// new is parenthesized where its place needs it, and only there.
func TestParentheses(t *testing.T) {
	for _, tc := range []struct{ args, want string }{
		{`{"line": 33, "expression": "by", "new": "by + 1"}`, "Math.max(0, p.height - (by + 1));"},
		{`{"line": 28, "expression": "liters", "new": "liters + 1"}`, "const ml = (liters + 1) * 1000;"},
		{`{"line": 28, "expression": "liters  *  1000", "new": "liters * 1e3"}`, "const ml = liters * 1e3;"},
		{`{"line": 33, "expression": "p.height - by", "new": "p.height - by, 0"}`, "Math.max(0, (p.height - by, 0));"},
		{`{"line": 23, "expression": "n", "column": 32, "new": "this.plants.length"}`, "this.plants[this.plants.length - 1]"},
	} {
		ws, dir := garden(t)
		if res := run(t, ws, tc.args); !res.Applied || !strings.Contains(source(t, dir), tc.want) {
			t.Errorf("%s: %s\n%s", tc.args, res, source(t, dir))
		}
	}
}

// all_occurrences replaces what means the same: Bed's plants in each of
// its methods, but not a parameter p of another function.
func TestAllOccurrences(t *testing.T) {
	ws, dir := garden(t)
	res := run(t, ws, `{"line": 17, "expression": "this.plants", "new": "this.plants.slice()", "all_occurrences": true}`)
	if got := strings.Count(source(t, dir), "this.plants.slice()"); !res.Applied || got != 4 {
		t.Fatalf("%d replaced, want 4: %s\n%s", got, res, source(t, dir))
	}
	res = run(t, ws, `{"line": 17, "expression": "p", "new": "{ ...p }", "all_occurrences": true}`)
	if !res.Applied || !strings.Contains(res.String(), "replaced 1 occurrence of p") || !strings.Contains(source(t, dir), "push({ ...p })") {
		t.Fatalf("%s\n%s", res, source(t, dir))
	}
}

func TestRefusals(t *testing.T) {
	ws, _ := garden(t)
	for _, tc := range []struct{ args, want string }{
		{`{"line": 17, "expression": "nope", "new": "p"}`, "no expression nope starts on line 17: this.plants.push(p);"},
		{`{"line": 23, "expression": "n", "new": "1"}`, "n starts on line 23 2 times; give column (12, 32)"},
		{`{"line": 17, "expression": "p", "new": "a; b"}`, "new is not"},
		{`{"line": 17, "expression": "p", "new": "1"}`, "not assignable"},
	} {
		if res := run(t, ws, tc.args); res.Applied || !strings.Contains(res.Refused, tc.want) {
			t.Errorf("%s: want a refusal with %q, got %s", tc.args, tc.want, res)
		}
	}
}
