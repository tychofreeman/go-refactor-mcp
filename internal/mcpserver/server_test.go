package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const calcSrc = `package calc

// Rate is the multiplier.
var Rate = 3

func Apply(n int) int { return n * Rate }
`

const calcTest = `package calc

import "testing"

func TestApply(t *testing.T) {
	if Apply(2) != 6 {
		t.Fatal("bad")
	}
}
`

// setup writes a small module and connects an MCP client to the server
// over in-memory transports, exactly as Claude Code would over stdio.
func setup(t *testing.T) (*mcp.ClientSession, string) {
	t.Helper()
	t.Setenv("GOWORK", "off")
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":            "module example.com/demo\n\ngo 1.22\n",
		"calc/calc.go":      calcSrc,
		"calc/calc_test.go": calcTest,
		"app/app.go":        "package app\n\nimport \"example.com/demo/calc\"\n\nfunc Run() int { return calc.Apply(calc.Rate) }\n",
	}
	for name, src := range files {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(src), 0o644)
	}

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := New(dir).MCP().Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs, dir
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String(), res.IsError
}

func mustContain(t *testing.T, label, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("%s: missing %q in:\n%s", label, w, got)
		}
	}
}

func TestToolsAndInstructions(t *testing.T) {
	cs, _ := setup(t)
	if !strings.Contains(cs.InitializeResult().Instructions, "ONE tool per call") {
		t.Error("server instructions not sent")
	}
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		names[tool.Name] = tool
	}
	for _, want := range []string{"rename", "extract_variable", "inline_variable", "move_declaration", "create_file", "inspect", "undo", "check"} {
		if names[want] == nil {
			t.Errorf("tool %q not listed", want)
		}
	}
	if tool := names["rename"]; tool != nil {
		schema, _ := tool.InputSchema.(map[string]any)
		if req, _ := schema["required"].([]any); len(req) != 4 {
			t.Errorf("rename schema not passed through: %v", tool.InputSchema)
		}
	}
	if tool := names["inspect"]; tool != nil && (tool.Annotations == nil || !tool.Annotations.ReadOnlyHint) {
		t.Error("inspect should be annotated read-only")
	}
}

// TestRefactoringLoop walks the loop the model is meant to follow.
func TestRefactoringLoop(t *testing.T) {
	cs, dir := setup(t)
	calcPath := filepath.Join(dir, "calc", "calc.go")

	out, _ := call(t, cs, "inspect", nil)
	mustContain(t, "overview", out, "calc/  (package calc)", "app/  (package app)", "calc_test.go")

	out, _ = call(t, cs, "inspect", map[string]any{"file": "calc/calc.go", "function": "Apply"})
	mustContain(t, "inspect function", out, "func Apply(n int) int   [1 line, complexity 1, nesting 0]", "6  func Apply")

	out, isErr := call(t, cs, "rename", map[string]any{"file": "calc/calc.go", "line": 4, "name": "Rate", "new_name": "Factor"})
	if isErr {
		t.Fatalf("rename errored: %s", out)
	}
	mustContain(t, "rename", out, "APPLIED rename", "3 occurrences in 2 files", "+var Factor = 3", "+func Run() int { return calc.Apply(calc.Factor) }")

	// A refusal is a normal result the model reads, not a tool error, and
	// changes nothing.
	before, _ := os.ReadFile(calcPath)
	out, isErr = call(t, cs, "rename", map[string]any{"file": "calc/calc.go", "line": 4, "name": "Factor", "new_name": "Apply"})
	if isErr || !strings.HasPrefix(out, "REFUSED rename") {
		t.Errorf("want plain REFUSED result, got isErr=%v:\n%s", isErr, out)
	}
	if after, _ := os.ReadFile(calcPath); string(after) != string(before) {
		t.Error("refused rename modified the file")
	}

	// Unknown parameters are reported, not ignored.
	out, _ = call(t, cs, "rename", map[string]any{"file": "calc/calc.go", "line": 4, "name": "Factor", "newName": "X"})
	mustContain(t, "bad args", out, "REFUSED", "invalid arguments")

	out, _ = call(t, cs, "check", map[string]any{"run_tests": true})
	mustContain(t, "check", out, "TYPE-CHECK OK", "TESTS PASSED", "ok  \texample.com/demo/calc")

	out, _ = call(t, cs, "undo", nil)
	mustContain(t, "undo", out, "UNDONE rename", "-var Factor = 3", "+var Rate = 3")
	if got, _ := os.ReadFile(calcPath); string(got) != calcSrc {
		t.Errorf("undo did not restore calc.go:\n%s", got)
	}
	out, _ = call(t, cs, "undo", nil)
	mustContain(t, "second undo", out, "Nothing to undo")
}

// TestExternalEditBetweenCalls: the model may edit a file by hand between
// tool calls. The next refactoring must use the new contents, not stale
// offsets from the previous load.
func TestExternalEditBetweenCalls(t *testing.T) {
	cs, dir := setup(t)
	calcPath := filepath.Join(dir, "calc", "calc.go")
	call(t, cs, "inspect", nil) // load the workspace

	// Insert lines above the declarations, shifting every offset.
	edited := strings.Replace(calcSrc, "package calc\n", "package calc\n\n// Package calc does arithmetic.\n// It is very small.\n", 1)
	os.WriteFile(calcPath, []byte(edited), 0o644)

	out, isErr := call(t, cs, "rename", map[string]any{"file": "calc/calc.go", "line": 7, "name": "Rate", "new_name": "Factor"})
	if isErr || !strings.HasPrefix(out, "APPLIED") {
		t.Fatalf("rename after external edit: %s", out)
	}
	got, _ := os.ReadFile(calcPath)
	// Code references change; the doc comment mentioning Rate does not.
	want := strings.NewReplacer("var Rate", "var Factor", "* Rate", "* Factor").Replace(edited)
	if string(got) != want {
		t.Errorf("edit landed at stale offsets:\n%s", got)
	}

	// Undo must refuse once the file is changed again behind its back.
	os.WriteFile(calcPath, append(got, []byte("\n// trailing\n")...), 0o644)
	out, _ = call(t, cs, "undo", nil)
	mustContain(t, "undo after edit", out, "Cannot undo", "modified since")
}

func TestBrokenCodeIsReported(t *testing.T) {
	cs, dir := setup(t)
	os.WriteFile(filepath.Join(dir, "calc", "calc.go"), []byte("package calc\n\nvar Rate int = \"x\"\n"), 0o644)
	out, _ := call(t, cs, "check", nil)
	mustContain(t, "check", out, "TYPE-CHECK FAILED", "calc/calc.go:3")
	out, _ = call(t, cs, "rename", map[string]any{"file": "calc/calc.go", "line": 3, "name": "Rate", "new_name": "Factor"})
	mustContain(t, "rename on broken code", out, "REFUSED", "does not type-check")
}

func TestCreateFileThenUndo(t *testing.T) {
	cs, dir := setup(t)
	path := filepath.Join(dir, "calc/half_test.go")
	broken := "package calc\n\nimport \"testing\"\n\nfunc TestHalf(t *testing.T) {\n\tif Half(4) != 2 {\n\t\tt.Fatal()\n\t}\n}\n"
	out, _ := call(t, cs, "create_file", map[string]any{"file": "calc/half_test.go", "content": broken})
	if !strings.Contains(out, "REFUSED create_file") || !strings.Contains(out, "undefined: Half") {
		t.Fatalf("want a file that does not type-check refused with its error, got:\n%s", out)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("refused file was left on disk: %v", err)
	}
	ok := strings.ReplaceAll(broken, "Half(4) != 2", "Apply(2) != 2*Rate")
	if out, _ := call(t, cs, "create_file", map[string]any{"file": "calc/half_test.go", "content": ok}); !strings.Contains(out, "APPLIED create_file") {
		t.Fatalf("want the file created, got:\n%s", out)
	}
	if out, _ := call(t, cs, "undo", map[string]any{}); !strings.Contains(out, "half_test.go") {
		t.Errorf("want undo to remove the file, got:\n%s", out)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file still there after undo: %v", err)
	}
}
