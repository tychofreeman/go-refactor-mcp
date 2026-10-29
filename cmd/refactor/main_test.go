package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/mcpserver"
)

// TestMain keeps the undo stacks the tests save out of the user's cache,
// and go test, which check runs, on the build cache it had.
func TestMain(m *testing.M) {
	if orig, err := os.UserCacheDir(); err == nil && os.Getenv("GOCACHE") == "" {
		os.Setenv("GOCACHE", filepath.Join(orig, "go-build"))
	}
	cache, err := os.MkdirTemp("", "refactor-cache")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CACHE_HOME", cache)
	code := m.Run()
	os.RemoveAll(cache)
	os.Exit(code)
}

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

// A call naming TypeScript files runs TypeScript's tools.
func TestTypeScript(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"tsconfig.json": `{"compilerOptions": {"strict": true, "module": "ESNext", "moduleResolution": "Bundler", "noEmit": true, "types": []}, "include": ["src"]}`,
		"src/a.ts":      "export function water(liters: number): string {\n  return liters + \" L\";\n}\n",
	} {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out strings.Builder
	stdout = &out
	defer func() { stdout = os.Stdout }()
	if code := run([]string{"-C", dir, "inspect", "file=src/a.ts"}); code != 0 || !strings.Contains(out.String(), "export function water(liters: number): string") {
		t.Fatalf("inspect: exit status %d\n%s", code, out.String())
	}
	if code := run([]string{"-C", dir, "rename", "file=src/a.ts", "name=water", "new_name=soak"}); code != 0 {
		t.Fatalf("rename: exit status %d\n%s", code, out.String())
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "src/a.ts")); !strings.Contains(string(b), "function soak(") {
		t.Errorf("a.ts:\n%s", b)
	}
	// undo, in another invocation, reverts the rename.
	if code := run([]string{"-C", dir, "undo"}); code != 0 {
		t.Fatalf("undo: exit status %d\n%s", code, out.String())
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "src/a.ts")); !strings.Contains(string(b), "function water(") {
		t.Errorf("a.ts after undo:\n%s", b)
	}
	if code := run([]string{"-C", dir, "undo"}); code != 2 {
		t.Errorf("a second undo: exit status %d, want 2 (nothing left)\n%s", code, out.String())
	}
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

func TestRunUndo(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var out strings.Builder
	stdout = &out
	defer func() { stdout = os.Stdout }()
	src := "package p\n\nfunc f() {}\n\nfunc g() { f() }\n"
	dir := module(t, src)
	if code := run([]string{"-C", dir, "rename", "file=p.go", "name=f", "new_name=h"}); code != 0 {
		t.Fatalf("rename: exit status %d", code)
	}
	steps := `{"steps":[{"tool":"rename","args":{"file":"p.go","name":"g","new_name":"k"}}]}`
	if code := run([]string{"-C", dir, "batch", steps}); code != 0 {
		t.Fatalf("batch: exit status %d", code)
	}
	if code := run([]string{"-C", dir, "undo"}); code != 0 || !strings.Contains(out.String(), "UNDONE") {
		t.Fatalf("undo of the batch: exit status %d:\n%s", code, out.String())
	}
	if got := read(t, dir); got != "package p\n\nfunc h() {}\n\nfunc g() { h() }\n" {
		t.Errorf("after the first undo:\n%s", got)
	}
	if code := run([]string{"-C", dir, "undo"}); code != 0 {
		t.Fatalf("undo of the rename: exit status %d:\n%s", code, out.String())
	}
	if got := read(t, dir); got != src {
		t.Errorf("after the second undo:\n%s", got)
	}
	out.Reset()
	if code := run([]string{"-C", dir, "undo"}); code != 2 || !strings.Contains(out.String(), "Nothing to undo") {
		t.Errorf("a third undo: exit status %d, want 2:\n%s", code, out.String())
	}
}

// list prints each tool's arguments, and with names only those tools;
// -version prints the version.
func TestListAndVersion(t *testing.T) {
	var out strings.Builder
	stdout = &out
	defer func() { stdout = os.Stdout }()
	if code := run([]string{"list", "replace_expression"}); code != 0 {
		t.Fatalf("list: exit status %d", code)
	}
	got := out.String()
	if !strings.Contains(got, "replace_expression takes:") || !strings.Contains(got, "  expression (required):") || strings.Contains(got, "\nrename\n") {
		t.Errorf("list replace_expression:\n%s", got)
	}
	out.Reset()
	if code := run([]string{"-version"}); code != 0 || !strings.HasPrefix(out.String(), "refactor "+mcpserver.Version) {
		t.Errorf("-version: exit status %d: %q", code, out.String())
	}
}
