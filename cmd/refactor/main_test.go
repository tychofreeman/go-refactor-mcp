package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// module writes a module with one file, p.go, and returns its directory.
func module(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string]string{"go.mod": "module example.com/p\n\ngo 1.22\n", "p.go": src} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func read(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "p.go"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRunDeclaresWhatAnEditCalls(t *testing.T) {
	dir := module(t, "package p\n\nfunc f() {}\n")
	if code := run([]string{"-C", dir, "add_declaration", `{"file":"p.go","content":"func g() int { return h(1) }"}`}); code != 0 {
		t.Fatalf("exit status %d, want 0", code)
	}
	if got := read(t, dir); !strings.Contains(got, "func h(") {
		t.Errorf("no stub for h:\n%s", got)
	}
}

func TestRunBatch(t *testing.T) {
	dir := module(t, "package p\n\nfunc f() {}\n")
	steps := `{"steps":[
		{"tool":"add_declaration","args":{"file":"p.go","content":"func g() { f() }"}},
		{"tool":"rename","args":{"file":"p.go","name":"f","new_name":"run"}}]}`
	if code := run([]string{"-C", dir, "batch", steps}); code != 0 {
		t.Fatalf("exit status %d, want 0", code)
	}
	if got := read(t, dir); !strings.Contains(got, "func g() { run() }") {
		t.Errorf("batch not applied:\n%s", got)
	}
}

func TestRunBatchRefused(t *testing.T) {
	dir := module(t, "package p\n\nfunc f() {}\n")
	if code := run([]string{"-C", dir, "batch", `{"steps":[{"tool":"inspect","args":{}}]}`}); code != 2 {
		t.Errorf("exit status %d, want 2", code)
	}
	if got := read(t, dir); got != "package p\n\nfunc f() {}\n" {
		t.Errorf("file changed:\n%s", got)
	}
}
