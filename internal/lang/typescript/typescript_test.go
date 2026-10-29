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
