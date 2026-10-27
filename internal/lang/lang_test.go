package lang

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/history"
)

// fake is a language of the files with its extension; each step it runs
// is pushed on the shared stack, labelled with its name.
type fake struct {
	name, ext string
	hist      *history.Stack
}

func (f *fake) Name() string          { return f.name }
func (f *fake) Owns(path string) bool { return strings.HasSuffix(path, f.ext) }
func (f *fake) Step(tool string, args json.RawMessage) (string, bool, error) {
	f.hist.Push(history.Change{Label: f.name + " " + tool, After: map[string][]byte{"x" + f.ext: nil}})
	return f.name + " ran " + tool, false, nil
}
func (f *fake) Batch(json.RawMessage) (string, error)                  { return f.name + " batch", nil }
func (f *fake) Inspect(json.RawMessage) (string, error)                { return f.name + " inspect", nil }
func (f *fake) Check(context.Context, json.RawMessage) (string, error) { return f.name + " check", nil }
func (f *fake) Undo() (string, error)                                  { return f.name + " undo", nil }

func twoLanguages() (*Dispatcher, *fake, *fake) {
	d := &Dispatcher{History: &history.Stack{}}
	g := &fake{name: "Go", ext: ".go", hist: d.History}
	ts := &fake{name: "TypeScript", ext: ".ts", hist: d.History}
	d.Languages = []Language{g, ts}
	return d, g, ts
}

func TestForRoutesByTheFilesNamed(t *testing.T) {
	d, g, ts := twoLanguages()
	for _, tc := range []struct {
		args string
		want Language
	}{
		{`{"file": "calc/calc.go", "line": 3}`, g},
		{`{"file": "src/url.ts", "name": "getPath"}`, ts},
		{`{"targets": [{"file": "a.ts"}, {"file": "b.ts", "function": "f"}]}`, ts},
		{`{"steps": [{"tool": "rename", "args": {"file": "a.ts"}}, {"tool": "move_declaration", "args": {"file": "b.ts", "to_file": "c.ts"}}]}`, ts},
		{`{"files": ["README.md", "web/app.ts"], "old": "a", "new": "b"}`, ts},
		{`{"names": ["Apply"]}`, g},                          // no file: the first language
		{`{"file": "README.md", "old": "a", "new": "b"}`, g}, // a file no language owns: the first
	} {
		got, refused := d.For(json.RawMessage(tc.args))
		if refused != "" || got != tc.want {
			t.Errorf("For(%s) = %v, %q; want %s", tc.args, got, refused, tc.want.Name())
		}
	}
}

func TestForRefusesTwoLanguagesInOneCall(t *testing.T) {
	d, _, _ := twoLanguages()
	_, refused := d.For(json.RawMessage(`{"steps": [{"tool": "rename", "args": {"file": "a.go"}}, {"tool": "rename", "args": {"file": "b.ts"}}]}`))
	for _, want := range []string{"Go", "a.go", "TypeScript", "b.ts", "separate calls"} {
		if !strings.Contains(refused, want) {
			t.Errorf("refusal %q does not name %q", refused, want)
		}
	}
}

// Undo goes to the language whose change is on top of the shared stack.
func TestForUndoFollowsTheLastChange(t *testing.T) {
	d, g, ts := twoLanguages()
	if got := d.ForUndo(); got != g {
		t.Errorf("with nothing done, undo goes to %s; want the first language", got.Name())
	}
	ts.Step("rename", nil)
	if got := d.ForUndo(); got != ts {
		t.Errorf("after a TypeScript step, undo goes to %s", got.Name())
	}
	g.Step("rename", nil)
	if got := d.ForUndo(); got != g {
		t.Errorf("after a Go step, undo goes to %s", got.Name())
	}
}
