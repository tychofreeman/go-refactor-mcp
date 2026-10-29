package golang

import (
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

	if out, err := l.Undo(); err != nil || !strings.Contains(out, "UNDONE replace_text") || !strings.HasPrefix(out, "In the module at ../other") {
		t.Fatalf("undo: %v\n%s", err, out)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "other/b.go")); !strings.Contains(string(b), "return 3") {
		t.Errorf("after undo, other/b.go:\n%s", b)
	}
}
