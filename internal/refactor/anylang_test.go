package refactor_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/history"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
)

// textWorkspace is a workspace of plain text files, for running the engine
// on a language other than Go: a file is in error while it holds the word
// "broken".
type textWorkspace struct {
	dir  string
	hist history.Stack
}

func (w *textWorkspace) Dir() string         { return w.dir }
func (w *textWorkspace) Rel(p string) string { return history.Rel(w.dir, p) }
func (w *textWorkspace) Abs(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(w.dir, p)
}

func (w *textWorkspace) Errors() []string {
	var errs []string
	entries, _ := os.ReadDir(w.dir)
	for _, e := range entries {
		b, _ := os.ReadFile(filepath.Join(w.dir, e.Name()))
		if bytes.Contains(b, []byte("broken")) {
			errs = append(errs, e.Name()+": broken")
		}
	}
	return errs
}

func (w *textWorkspace) Preview(label string, fes []edit.FileEdit) (history.Change, error) {
	c := history.Change{Label: label, Before: map[string][]byte{}, After: map[string][]byte{}, Created: map[string]bool{}, Deleted: map[string]bool{}}
	var diffs []string
	for _, fe := range edit.Merge(fes) {
		before, err := os.ReadFile(fe.Path)
		if err != nil {
			return history.Change{}, err
		}
		after, err := edit.Apply(before, fe.Edits)
		if err != nil {
			return history.Change{}, err
		}
		c.Before[fe.Path], c.After[fe.Path] = before, after
		diffs = append(diffs, edit.Unified(w.Rel(fe.Path), before, after))
	}
	sort.Strings(diffs)
	c.Diff = strings.Join(diffs, "")
	return c, nil
}

func (w *textWorkspace) Apply(label string, fes []edit.FileEdit) (history.Change, error) {
	c, err := w.Preview(label, fes)
	if err != nil {
		return history.Change{}, err
	}
	if err := c.Write(); err != nil {
		return history.Change{}, err
	}
	w.hist.Push(c)
	return c, nil
}

func (w *textWorkspace) Undo() (history.Change, error) { return w.hist.Undo(w.dir) }
func (w *textWorkspace) Squash(n int, label string) (history.Change, error) {
	return w.hist.Squash(n, label, w.dir)
}
func (w *textWorkspace) LastChange() (history.Change, bool) { return w.hist.Last() }

// replaceWord replaces the first occurrence of old in file with new.
type replaceWord struct{}

func (replaceWord) Name() string        { return "replace_word" }
func (replaceWord) Description() string { return "" }
func (replaceWord) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file":{"type":"string"},"old":{"type":"string"},"new":{"type":"string"}},"required":["file","old","new"]}`)
}
func (replaceWord) Plan(ws *textWorkspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct{ File, Old, New string }
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, refactor.Refusef("invalid arguments: %v", err)
	}
	path := ws.Abs(a.File)
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	i := bytes.Index(src, []byte(a.Old))
	if i < 0 {
		return nil, refactor.Refusef("%q is not in %s", a.Old, a.File)
	}
	return &refactor.Plan{
		Summary: "replaced " + a.Old,
		Edits:   []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{Start: i, End: i + len(a.Old), NewText: a.New}}}},
	}, nil
}

func newTextWorkspace(t *testing.T, files map[string]string) *textWorkspace {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &textWorkspace{dir: dir}
}

func read(t *testing.T, ws *textWorkspace, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(ws.dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The engine runs on any workspace: a step applies with its diff, and one
// that leaves an error is refused and undone, as for Go.
func TestRunOnAnotherLanguage(t *testing.T) {
	ws := newTextWorkspace(t, map[string]string{"a.txt": "hello world\n"})

	res, err := refactor.Run(ws, replaceWord{}, json.RawMessage(`{"file":"a.txt","old":"world","new":"there"}`), refactor.OptionsOf[*textWorkspace]{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Applied || !strings.Contains(res.Diff, "+hello there") {
		t.Fatalf("not applied as expected: %s", res)
	}

	res, err = refactor.Run(ws, replaceWord{}, json.RawMessage(`{"file":"a.txt","old":"there","new":"broken"}`), refactor.OptionsOf[*textWorkspace]{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied || !strings.Contains(res.Refused, "a.txt: broken") {
		t.Fatalf("a step that leaves an error was not refused: %s", res)
	}
	if got := read(t, ws, "a.txt"); got != "hello there\n" {
		t.Fatalf("the refused step was not undone: %q", got)
	}
}

// A batch on any workspace applies its steps as one change, and rolls them
// all back when one is refused.
func TestBatchOnAnotherLanguage(t *testing.T) {
	ws := newTextWorkspace(t, map[string]string{"a.txt": "one two\n", "b.txt": "three\n"})
	step := func(file, old, new string) refactor.StepOf[*textWorkspace] {
		args, _ := json.Marshal(map[string]string{"file": file, "old": old, "new": new})
		return refactor.StepOf[*textWorkspace]{Refactoring: replaceWord{}, Args: args}
	}

	b, err := refactor.RunBatch(ws, []refactor.StepOf[*textWorkspace]{step("a.txt", "one", "1"), step("b.txt", "three", "3")}, refactor.OptionsOf[*textWorkspace]{})
	if err != nil {
		t.Fatal(err)
	}
	if !b.Applied || read(t, ws, "a.txt") != "1 two\n" || read(t, ws, "b.txt") != "3\n" {
		t.Fatalf("batch not applied: %s", b)
	}
	if c, _ := ws.LastChange(); len(c.Files(ws.dir)) != 2 {
		t.Fatalf("the batch is not one change: %v", c.Files(ws.dir))
	}

	b, err = refactor.RunBatch(ws, []refactor.StepOf[*textWorkspace]{step("a.txt", "two", "2"), step("b.txt", "3", "broken")}, refactor.OptionsOf[*textWorkspace]{})
	if err != nil {
		t.Fatal(err)
	}
	if b.Applied || b.FailedStep != 2 {
		t.Fatalf("second step not refused: %s", b)
	}
	if read(t, ws, "a.txt") != "1 two\n" {
		t.Fatalf("the first step was not rolled back: %q", read(t, ws, "a.txt"))
	}
}
