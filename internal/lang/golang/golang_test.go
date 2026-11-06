package golang

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/history"
)

// write creates the files, by path relative to dir.
func write(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, text := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// A call that names a file of another module, beside or inside the
// directory's own, works in that module, and undo reverts it; a call
// that names files of two modules is refused.
func TestOtherModules(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"home/go.mod":       "module example.com/home\n\ngo 1.22\n",
		"home/a.go":         "package home\n\nfunc A() int { return 1 }\n",
		"home/tools/go.mod": "module example.com/tools\n\ngo 1.22\n",
		"home/tools/t.go":   "package tools\n\nfunc T() int { return 2 }\n",
		"other/go.mod":      "module example.com/other\n\ngo 1.22\n",
		"other/b.go":        "package other\n\nfunc B() int { return 3 }\n",
	})
	l := New(filepath.Join(root, "home"), []string{"./..."}, &history.Stack{})

	text, refused, err := l.Step("replace_text", []byte(`{"file": "../other/b.go", "old": "return 3", "new": "return 4"}`))
	if err != nil || refused {
		t.Fatalf("step in a sibling module: %v %v\n%s", err, refused, text)
	}
	if !strings.HasPrefix(text, "In the module at ../other") {
		t.Errorf("result does not say which module:\n%s", text)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "other/b.go")); !strings.Contains(string(b), "return 4") {
		t.Errorf("other/b.go:\n%s", b)
	}

	out, err := l.Inspect([]byte(`{"file": "tools/t.go", "function": "T"}`))
	if err != nil || !strings.Contains(out, "return 2") {
		t.Errorf("inspect in a nested module: %v\n%s", err, out)
	}

	out, err = l.Batch([]byte(`{"steps": [{"tool": "replace_text", "args": {"file": "a.go", "old": "return 1", "new": "return 5"}}, {"tool": "replace_text", "args": {"file": "tools/t.go", "old": "return 2", "new": "return 6"}}]}`))
	if err != nil || !strings.Contains(out, "more than one module") {
		t.Errorf("batch across modules: %v\n%s", err, out)
	}

	if out, err := l.Undo(nil); err != nil || !strings.Contains(out, "UNDONE replace_text") || !strings.HasPrefix(out, "In the module at ../other") {
		t.Fatalf("undo: %v\n%s", err, out)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "other/b.go")); !strings.Contains(string(b), "return 3") {
		t.Errorf("after undo, other/b.go:\n%s", b)
	}
}

// move_file's to, a path like file, is taken in the module the call works
// in; to in another module than file's is refused, as two modules are.
func TestMoveFileInOtherModule(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"home/go.mod":       "module example.com/home\n\ngo 1.22\n",
		"home/a.go":         "package home\n\nfunc A() int { return 1 }\n",
		"home/tools/go.mod": "module example.com/tools\n\ngo 1.22\n",
		"home/tools/t.go":   "package tools\n\nfunc T() int { return 2 }\n",
	})
	l := New(filepath.Join(root, "home"), []string{"./..."}, &history.Stack{})
	text, refused, err := l.Step("move_file", []byte(`{"file": "tools/t.go", "to": "tools/u.go"}`))
	if err != nil || refused {
		t.Fatalf("move_file in a nested module: %v %v\n%s", err, refused, text)
	}
	if _, err := os.Stat(filepath.Join(root, "home/tools/u.go")); err != nil {
		t.Errorf("home/tools/u.go: %v\n%s", err, text)
	}
	if text, refused, _ := l.Step("move_file", []byte(`{"file": "a.go", "to": "tools/a.go"}`)); !refused || !strings.Contains(text, "more than one module") {
		t.Errorf("move_file into another module is not refused:\n%s", text)
	}
}

// A call from a worktree that names no file (inspect's overview, check,
// undo) carries the worktree, as cmd/worktree-paths adds it: inspect and
// check work in its module, and undo refuses a change made elsewhere.
func TestWorktreeCalls(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"go.mod":                     "module example.com/home\n\ngo 1.22\n",
		"a.go":                       "package home\n\nfunc A() int { return 1 }\n",
		".claude/worktrees/w/go.mod": "module example.com/home\n\ngo 1.22\n",
		".claude/worktrees/w/a.go":   "package home\n\nfunc A() int { return 1 }\n\nfunc W() int { return 2 }\n",
	})
	tree := filepath.Join(root, ".claude/worktrees/w")
	from := []byte(`{"worktree": "` + tree + `"}`)
	l := New(root, []string{"./..."}, &history.Stack{})

	out, err := l.Inspect(from)
	if err != nil || !strings.HasPrefix(out, "In the module at .claude/worktrees/w") {
		t.Errorf("inspect from the worktree: %v\n%s", err, out)
	}
	out, err = l.Check(context.Background(), from)
	if err != nil || !strings.HasPrefix(out, "In the module at .claude/worktrees/w") {
		t.Errorf("check from the worktree: %v\n%s", err, out)
	}

	if text, refused, err := l.Step("replace_text", []byte(`{"file": "a.go", "old": "return 1", "new": "return 3"}`)); err != nil || refused {
		t.Fatalf("step in the home module: %v %v\n%s", err, refused, text)
	}
	out, err = l.Undo(from)
	if err != nil || !strings.Contains(out, "not in this worktree") {
		t.Errorf("undo from the worktree of a change in the home module: %v\n%s", err, out)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "a.go")); !strings.Contains(string(b), "return 3") {
		t.Errorf("a.go was undone from the worktree:\n%s", b)
	}
	// A relative path with the worktree is the worktree's file, as when the
	// server takes the worktree from the client's roots.
	if text, refused, err := l.Step("replace_text", []byte(`{"file": "a.go", "old": "return 2", "new": "return 4", "worktree": "`+tree+`"}`)); err != nil || refused {
		t.Fatalf("step by a relative path from the worktree: %v %v\n%s", err, refused, text)
	}
	if b, _ := os.ReadFile(filepath.Join(tree, "a.go")); !strings.Contains(string(b), "return 4") {
		t.Errorf("the worktree's a.go:\n%s", b)
	}
}

// move is published in place of move_declaration, move_method and
// move_file, and runs the one its arguments ask for; the three still run
// by name.
func TestMove(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"go.mod":    "module example.com/home\n\ngo 1.22\n",
		"a.go":      "package home\n\ntype Soil struct{ depth int }\n\nfunc (s Soil) Deep() bool { return s.depth > 2 }\n\ntype Bed struct{ Soil }\n\nconst MaxBeds = 4\n\nfunc Count() int { return MaxBeds }\n",
		"notes.txt": "beds\n",
	})
	l := New(root, []string{"./..."}, &history.Stack{})
	for _, args := range []string{
		`{"file": "a.go", "name": "MaxBeds", "to_file": "limits.go"}`,
		`{"file": "a.go", "name": "Soil.Deep", "to": "Bed"}`,
		`{"file": "notes.txt", "to": "docs/notes.txt"}`,
	} {
		if text, refused, err := l.Step("move", []byte(args)); err != nil || refused {
			t.Fatalf("move %s: %v %v\n%s", args, err, refused, text)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(root, "limits.go")); !strings.Contains(string(b), "const MaxBeds = 4") {
		t.Errorf("limits.go:\n%s", b)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "a.go")); !strings.Contains(string(b), "func (b Bed) Deep() bool") && !strings.Contains(string(b), "Bed) Deep()") {
		t.Errorf("Deep is not Bed's:\n%s", b)
	}
	if _, err := os.Stat(filepath.Join(root, "docs/notes.txt")); err != nil {
		t.Errorf("notes.txt was not moved: %v", err)
	}
	if text, refused, _ := l.Step("move", []byte(`{"file": "a.go", "name": "Count"}`)); !refused {
		t.Errorf("move without a destination is not refused:\n%s", text)
	}
	if text, refused, err := l.Step("move_declaration", []byte(`{"file": "a.go", "name": "Count", "to_file": "count.go"}`)); err != nil || refused {
		t.Errorf("move_declaration by its own name: %v %v\n%s", err, refused, text)
	}
	tools := map[string]bool{}
	for _, tool := range l.Tools() {
		tools[tool.Name] = true
	}
	if !tools["move"] || tools["move_declaration"] || tools["move_method"] || tools["move_file"] || tools["delete_file"] {
		t.Errorf("published: move %v, move_declaration %v, move_method %v, move_file %v, delete_file %v", tools["move"], tools["move_declaration"], tools["move_method"], tools["move_file"], tools["delete_file"])
	}
	if text, refused, err := l.Step("move", []byte(`{"file": "docs/notes.txt", "delete": true}`)); err != nil || refused {
		t.Fatalf("move with delete: %v %v\n%s", err, refused, text)
	}
	if _, err := os.Stat(filepath.Join(root, "docs/notes.txt")); err == nil {
		t.Error("docs/notes.txt was not deleted")
	}
	if text, refused, _ := l.Step("move", []byte(`{"file": "a.go", "delete": true, "to": "b.go"}`)); !refused {
		t.Errorf("delete with a destination is not refused:\n%s", text)
	}
}

// move_statement is published in place of move_statement_up and
// move_statement_down, and runs the one its direction names.
func TestMoveStatement(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"go.mod": "module example.com/home\n\ngo 1.22\n",
		"a.go":   "package home\n\nfunc F() int {\n\ta := 1\n\tb := 2\n\treturn a + b\n}\n",
	})
	l := New(root, []string{"./..."}, &history.Stack{})
	for _, s := range []struct{ args, want string }{
		{`{"file": "a.go", "line": 4, "direction": "down"}`, "b := 2\n\ta := 1"},
		{`{"file": "a.go", "line": 5, "direction": "up"}`, "a := 1\n\tb := 2"},
	} {
		if text, refused, err := l.Step("move_statement", []byte(s.args)); err != nil || refused {
			t.Fatalf("move_statement %s: %v %v\n%s", s.args, err, refused, text)
		}
		if b, _ := os.ReadFile(filepath.Join(root, "a.go")); !strings.Contains(string(b), s.want) {
			t.Errorf("after %s:\n%s", s.args, b)
		}
	}
	if text, refused, _ := l.Step("move_statement", []byte(`{"file": "a.go", "line": 4, "direction": "left"}`)); !refused || !strings.Contains(text, "up or down") {
		t.Errorf("a direction that is neither up nor down is not refused:\n%s", text)
	}
	tools := map[string]bool{}
	for _, tool := range l.Tools() {
		tools[tool.Name] = true
	}
	if !tools["move_statement"] || tools["move_statement_up"] || tools["move_statement_down"] {
		t.Errorf("published: %v", tools)
	}
}

// With SkeletonOnly, create_file refuses Go content with function bodies,
// and takes types, fields, constants and variables, and declaration.
func TestSkeletonOnly(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{"go.mod": "module example.com/home\n\ngo 1.22\n", "a.go": "package home\n"})
	l := New(root, []string{"./..."}, &history.Stack{})
	l.SkeletonOnly = true
	if text, refused, _ := l.Step("create_file", []byte(`{"file": "bed.go", "content": "package home\n\ntype Bed struct{ depth int }\n\nfunc (b *Bed) Dig() {}\n"}`)); !refused || !strings.Contains(text, "declaration") {
		t.Errorf("a function body is not refused, naming declaration:\n%s", text)
	}
	for _, args := range []string{`{"file": "bed.go", "content": "package home\n\ntype Bed struct{ depth int }\n\nconst rows = 3\n"}`, `{"file": "pot.go", "declaration": "type Pot struct", "implements": ["fmt.Stringer"]}`} {
		if text, refused, err := l.Step("create_file", []byte(args)); err != nil || refused {
			t.Errorf("%s: %v %v\n%s", args, err, refused, text)
		}
	}
}

// inline is published in place of inline_function, inline_variable and
// inline_struct, and runs the one that what it names asks for.
func TestInline(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"go.mod": "module example.com/home\n\ngo 1.22\n",
		"a.go":   "package home\n\nfunc double(n int) int { return n * 2 }\n\nfunc Use() int {\n\tx := 3\n\treturn double(x)\n}\n\ntype Soil struct {\n\tdepth int\n}\n\ntype Bed struct {\n\tSoil\n}\n\nfunc (b Bed) Deep() bool { return b.depth > 2 }\n",
	})
	l := New(root, []string{"./..."}, &history.Stack{})
	if text, refused, _ := l.Step("inline", []byte(`{"file": "a.go", "name": "double"}`)); !refused {
		t.Errorf("inline without a line, all_calls or field is not refused:\n%s", text)
	}
	for _, args := range []string{
		`{"file": "a.go", "line": 6, "name": "x"}`,
		`{"file": "a.go", "line": 6, "name": "double"}`,
		`{"file": "a.go", "name": "Bed", "field": "Soil"}`,
	} {
		if text, refused, err := l.Step("inline", []byte(args)); err != nil || refused {
			t.Fatalf("inline %s: %v %v\n%s", args, err, refused, text)
		}
	}
	b, _ := os.ReadFile(filepath.Join(root, "a.go"))
	for _, want := range []string{"n := 3\n\treturn n * 2", "type Bed struct {\n\tdepth int\n}"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("a.go lacks %q:\n%s", want, b)
		}
	}
	tools := map[string]bool{}
	for _, tool := range l.Tools() {
		tools[tool.Name] = true
	}
	if !tools["inline"] || tools["inline_function"] || tools["inline_variable"] || tools["inline_struct"] {
		t.Errorf("published: inline %v, inline_function %v, inline_variable %v, inline_struct %v", tools["inline"], tools["inline_function"], tools["inline_variable"], tools["inline_struct"])
	}
}

// rename with a file and no name renames the file in its directory.
func TestRenameFile(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{"go.mod": "module example.com/home\n\ngo 1.22\n", "a.go": "package home\n", "testdata/old.txt": "beds\n"})
	l := New(root, []string{"./..."}, &history.Stack{})
	if text, refused, err := l.Step("rename", []byte(`{"file": "testdata/old.txt", "new_name": "new.txt"}`)); err != nil || refused {
		t.Fatalf("rename of a file: %v %v\n%s", err, refused, text)
	}
	if _, err := os.Stat(filepath.Join(root, "testdata/new.txt")); err != nil {
		t.Errorf("testdata/new.txt: %v", err)
	}
	if text, refused, _ := l.Step("rename", []byte(`{"file": "a.go", "new_name": "other/b.go"}`)); !refused || !strings.Contains(text, "move") {
		t.Errorf("a path as new_name is not refused, naming move:\n%s", text)
	}
}

// change_type is published in place of change_field_type and
// change_parameter_type, and runs the one its arguments ask for.
func TestChangeTypeCovers(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"go.mod": "module example.com/home\n\ngo 1.22\n",
		"a.go":   "package home\n\ntype Plant struct{ Height int }\n\nfunc grow(n int) int { return n + 1 }\n\nfunc Use(p Plant) int { return grow(p.Height) }\n",
	})
	l := New(root, []string{"./..."}, &history.Stack{})
	for _, args := range []string{
		`{"file": "a.go", "name": "Plant.Height", "type": "int64"}`,
		`{"file": "a.go", "name": "grow", "parameter": "n", "type": "int64"}`,
	} {
		if text, refused, err := l.Step("change_type", []byte(args)); err != nil || refused {
			t.Fatalf("change_type %s: %v %v\n%s", args, err, refused, text)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(root, "a.go")); !strings.Contains(string(b), "Height int64") || !strings.Contains(string(b), "func grow(n int64)") {
		t.Errorf("a.go:\n%s", b)
	}
	tools := map[string]bool{}
	for _, tool := range l.Tools() {
		tools[tool.Name] = true
	}
	if !tools["change_type"] || tools["change_field_type"] || tools["change_parameter_type"] {
		t.Errorf("published: %v", tools)
	}
}
