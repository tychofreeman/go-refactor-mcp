package typescript

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/history"
)

// A Go tool's name runs the TypeScript tool that does its operation.
func TestGoNames(t *testing.T) {
	for goName, want := range goNames {
		r, ok := lookup(goName)
		if !ok || r.Name() != want {
			t.Errorf("lookup(%q) = %v, %v; want %s", goName, r, ok, want)
		}
	}
	if _, ok := lookup("no_such_tool"); ok {
		t.Errorf("lookup(no_such_tool) found a tool")
	}
}

// The tests are package.json's test script when it has one, else
// node --test; a pattern other than ./... names the files.
func TestTestCommand(t *testing.T) {
	plain, scripted := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(scripted, "package.json"), []byte(`{"scripts": {"test": "node --test"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		dir, pattern string
		want         []string
	}{
		{plain, "./...", []string{"node", "--test"}},
		{plain, "", []string{"node", "--test"}},
		{plain, "src/a.test.ts", []string{"node", "--test", "src/a.test.ts"}},
		{scripted, "./...", []string{"npm", "test", "--silent"}},
		{scripted, "src/a.test.ts", []string{"npm", "test", "--silent", "--", "src/a.test.ts"}},
	} {
		if got := testCommand(c.dir, c.pattern); !slices.Equal(got, c.want) {
			t.Errorf("testCommand(%s, %q) = %q; want %q", filepath.Base(c.dir), c.pattern, got, c.want)
		}
	}
}

// A batch's checkpoint and check run the project's tests, not go test.
func TestCheckpointRunsTheProjectsTests(t *testing.T) {
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm is not installed")
	}
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("tsconfig.json", `{"compilerOptions": {"strict": true, "types": []}, "include": ["a.ts"]}`)
	write("a.ts", "export const a = 1;\n")
	write("package.json", `{"scripts": {"test": "node -e \"process.exit(require('fs').existsSync('fail') ? 1 : 0)\""}}`)
	l := New(dir, &history.Stack{})
	batch := json.RawMessage(`{"steps": [{"tool": "check", "args": {"run_tests": true}}]}`)
	if out, err := l.Batch(batch); err != nil || !strings.Contains(out, "tests pass") {
		t.Fatalf("passing tests: %v\n%s", err, out)
	}
	if out, err := l.Check(context.Background(), json.RawMessage(`{"run_tests": true}`)); err != nil || !strings.Contains(out, "TESTS PASSED") {
		t.Fatalf("check, passing tests: %v\n%s", err, out)
	}
	write("fail", "")
	if out, err := l.Batch(batch); err != nil || !strings.Contains(out, "tests of ./... failed") {
		t.Fatalf("failing tests: %v\n%s", err, out)
	}
	if out, err := l.Check(context.Background(), json.RawMessage(`{"run_tests": true}`)); err != nil || !strings.Contains(out, "TESTS FAILED") {
		t.Fatalf("check, failing tests: %v\n%s", err, out)
	}
}

// A call that names a file of another solution, beside the directory's or
// inside it but in no project of its own, works in that solution, and undo
// reverts it; a call that names files of two solutions is refused.
func TestOtherSolutions(t *testing.T) {
	root := t.TempDir()
	config := `{"compilerOptions": {"strict": true, "noEmit": true, "types": []}, "include": ["*.ts", "src"]}`
	for name, text := range map[string]string{
		"home/tsconfig.json":       `{"compilerOptions": {"strict": true, "noEmit": true, "types": []}, "include": ["src"]}`,
		"home/src/a.ts":            "export function a(): number {\n  return 1;\n}\n",
		"home/tools/tsconfig.json": config,
		"home/tools/t.ts":          "export function t(): number {\n  return 2;\n}\n",
		"other/tsconfig.json":      config,
		"other/b.ts":               "export function b(): number {\n  return 3;\n}\n",
	} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	l := New(filepath.Join(root, "home"), &history.Stack{})

	text, refused, err := l.Step("replace_text", []byte(`{"file": "../other/b.ts", "old": "return 3", "new": "return 4"}`))
	if err != nil || refused {
		t.Fatalf("step in a sibling solution: %v %v\n%s", err, refused, text)
	}
	if !strings.HasPrefix(text, "In the solution at ../other") {
		t.Errorf("result does not say which solution:\n%s", text)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "other/b.ts")); !strings.Contains(string(b), "return 4") {
		t.Errorf("other/b.ts:\n%s", b)
	}

	out, err := l.Inspect([]byte(`{"file": "tools/t.ts", "function": "t"}`))
	if err != nil || !strings.Contains(out, "return 2") {
		t.Errorf("inspect in a nested solution: %v\n%s", err, out)
	}

	out, err = l.Batch([]byte(`{"steps": [{"tool": "replace_text", "args": {"file": "src/a.ts", "old": "return 1", "new": "return 5"}}, {"tool": "replace_text", "args": {"file": "tools/t.ts", "old": "return 2", "new": "return 6"}}]}`))
	if err != nil || !strings.Contains(out, "more than one solution") {
		t.Errorf("batch across solutions: %v\n%s", err, out)
	}

	if out, err := l.Undo(); err != nil || !strings.Contains(out, "UNDONE replace_text") || !strings.HasPrefix(out, "In the solution at ../other") {
		t.Fatalf("undo: %v\n%s", err, out)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "other/b.ts")); !strings.Contains(string(b), "return 3") {
		t.Errorf("after undo, other/b.ts:\n%s", b)
	}
}
