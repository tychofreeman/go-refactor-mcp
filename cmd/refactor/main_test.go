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

// A batch's JSON can come from a file (@path), so long content needs no
// shell quoting; escapes such as \n in Go strings stay as written.
func TestRunBatchFromFile(t *testing.T) {
	dir := module(t, "package m\n\nfunc f() {}\n")
	steps := `{"steps":[{"tool":"add_declaration","args":{"file":"p.go","content":"func g() string { return \"a\\nb\" }"}}]}`
	path := filepath.Join(t.TempDir(), "batch.json")
	if err := os.WriteFile(path, []byte(steps), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"-C", dir, "batch", "@" + path}); code != 0 {
		t.Fatalf("exit status %d, want 0", code)
	}
	if got := read(t, dir); !strings.Contains(got, `return "a\nb"`) {
		t.Errorf("batch from file not applied as written:\n%s", got)
	}
}

// A step's JSON can come from stdin (-).
func TestRunFromStdin(t *testing.T) {
	dir := module(t, "package m\n\nfunc f() {}\n")
	stdin = strings.NewReader(`{"file":"p.go","content":"func g() {}"}`)
	defer func() { stdin = os.Stdin }()
	if code := run([]string{"-C", dir, "add_declaration", "-"}); code != 0 {
		t.Fatalf("exit status %d, want 0", code)
	}
	if got := read(t, dir); !strings.Contains(got, "func g() {}") {
		t.Errorf("step from stdin not applied:\n%s", got)
	}
}

// inspect reads code as the server's inspect does, and a refused read
// exits with status 2.
func TestRunInspect(t *testing.T) {
	dir := module(t, "package p\n\n// F does nothing.\nfunc F() {}\n")
	var out strings.Builder
	stdout = &out
	defer func() { stdout = os.Stdout }()
	if code := run([]string{"-C", dir, "inspect", `{"names":["F"]}`}); code != 0 {
		t.Fatalf("exit status %d, want 0", code)
	}
	if !strings.Contains(out.String(), "4  func F() {}") {
		t.Errorf("inspect did not show F:\n%s", out.String())
	}
	if code := run([]string{"-C", dir, "inspect", "file=p.go"}); code != 0 || !strings.Contains(out.String(), "function bodies elided") {
		t.Errorf("inspect file=p.go: exit status %d:\n%s", code, out.String())
	}
	if code := run([]string{"-C", dir, "inspect", `{"names":["F"],"file":"p.go"}`}); code != 2 {
		t.Errorf("exit status %d, want 2", code)
	}
}

func TestRunCheck(t *testing.T) {
	var out strings.Builder
	stdout = &out
	defer func() { stdout = os.Stdout }()
	dir := module(t, "package p\n\nfunc F() int { return 1 }\n")
	if code := run([]string{"-C", dir, "check"}); code != 0 || !strings.Contains(out.String(), "TYPE-CHECK OK") {
		t.Errorf("check: exit status %d, want 0:\n%s", code, out.String())
	}
	test := "package p\n\nimport \"testing\"\n\nfunc TestF(t *testing.T) {\n\tif F() != 2 {\n\t\tt.Fatal(\"F is not 2\")\n\t}\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "p_test.go"), []byte(test), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := run([]string{"-C", dir, "check", "run_tests=true"}); code != 2 || !strings.Contains(out.String(), "TESTS FAILED") || !strings.Contains(out.String(), "F is not 2") {
		t.Errorf("check run_tests=true: exit status %d, want 2:\n%s", code, out.String())
	}
	broken := module(t, "package p\n\nfunc F() int { return \"one\" }\n")
	out.Reset()
	if code := run([]string{"-C", broken, "check"}); code != 2 || !strings.Contains(out.String(), "TYPE-CHECK FAILED") {
		t.Errorf("check of code that does not type-check: exit status %d, want 2:\n%s", code, out.String())
	}
}
