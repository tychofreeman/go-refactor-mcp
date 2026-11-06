package refactor

import (
	"encoding/json"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// move stands for move_declaration, move_method and move_file, chosen by
// its arguments: to_file for a declaration, to with name for a method, to
// without name for a file.
func TestMoveRoutes(t *testing.T) {
	m := NewMove[*workspace.Workspace]("")
	for args, want := range map[string]string{
		`{"file": "a.go", "name": "Water", "to_file": "b.go"}`:                    "move_declaration",
		`{"file": "a.go", "name": "Water", "to_file": "x/b.go", "also": ["dry"]}`: "move_declaration",
		`{"file": "a.go", "name": "Bed.Water", "to": "Garden"}`:                   "move_method",
		`{"file": "testdata/old", "to": "testdata/new", "copy": true}`:            "move_file",
	} {
		got, err := m.Route(json.RawMessage(args))
		if err != nil || got != want {
			t.Errorf("Route(%s) = %q, %v; want %s", args, got, err, want)
		}
	}
	for _, args := range []string{`{"file": "a.go", "name": "Water"}`, `{"file": "a.go"}`, `{"file": "a.go", "name": "Water", "to": "B", "to_file": "b.go"}`, `{"file": "a.go", "to_file": "b.go"}`, `{"file": "a", "to": "b", "also": ["x"]}`, `{"file": "a.go", "nme": "W", "to": "b"}`} {
		if _, err := m.Route(json.RawMessage(args)); !IsRefusal(err) {
			t.Errorf("Route(%s): want a refusal, got %v", args, err)
		}
	}
	if got := m.Covers(); len(got) != 3 {
		t.Errorf("Covers() = %v; want the three move tools", got)
	}
}

// A batch step of move runs the tool its arguments ask for.
func TestBatchRoutesMove(t *testing.T) {
	lookup := func(name string) (Refactoring, bool) {
		if name == "move" {
			return NewMove[*workspace.Workspace](""), true
		}
		return &fakeTool{Called[moveArgs](name)}, true
	}
	b, refused := ParseBatch(json.RawMessage(`{"steps": [{"tool": "move", "args": {"file": "a.go", "name": "Water", "to_file": "b.go"}}, {"tool": "move", "args": {"file": "notes.txt", "to": "docs/notes.txt"}}]}`), lookup)
	if refused != "" {
		t.Fatal(refused)
	}
	for i, want := range []string{"move_declaration", "move_file"} {
		if got := b.Steps[i].Refactoring.Name(); got != want {
			t.Errorf("step %d runs %s; want %s", i+1, got, want)
		}
	}
}

// fakeTool is a tool that only has a name.
type fakeTool struct{ Named[moveArgs] }

func (*fakeTool) Description() string { return "" }

func (*fakeTool) Plan(*workspace.Workspace, json.RawMessage) (*Plan, error) { return nil, nil }
