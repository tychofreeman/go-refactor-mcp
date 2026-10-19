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
	if !strings.Contains(cs.InitializeResult().Instructions, "without line") {
		t.Error("server instructions do not describe addressing a declaration by name")
	}
	if !strings.Contains(cs.InitializeResult().Instructions, "no need to search for them first") {
		t.Error("server instructions do not say the refactorings find every reference themselves")
	}
	if !strings.Contains(cs.InitializeResult().Instructions, "send them together with batch") {
		t.Error("server instructions do not suggest batching a planned sequence")
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
		if req, _ := schema["required"].([]any); len(req) != 3 {
			t.Errorf("rename schema not passed through: %v", tool.InputSchema)
		}
	}
	if tool := names["inspect"]; tool != nil && (tool.Annotations == nil || !tool.Annotations.ReadOnlyHint) {
		t.Error("inspect should be annotated read-only")
	}
}

// symbolTools target a declaration, so they accept name alone, as a
// package-level symbol, in place of file + line + name.
var symbolTools = []string{
	"rename", "remove_dead_code", "merge_duplicate_function", "move_declaration",
	"extract_struct", "add_parameter", "remove_parameter", "move_parameter",
	"function_to_method", "method_to_function", "add_field",
}

func TestSymbolToolsDoNotRequireLine(t *testing.T) {
	cs, _ := setup(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		tools[tool.Name] = tool
	}
	for _, name := range symbolTools {
		tool := tools[name]
		if tool == nil {
			t.Errorf("tool %q not listed", name)
			continue
		}
		schema, _ := tool.InputSchema.(map[string]any)
		req, _ := schema["required"].([]any)
		for _, r := range req {
			if r == "line" {
				t.Errorf("%s: line is required", name)
			}
		}
		props, _ := schema["properties"].(map[string]any)
		nameProp, _ := props["name"].(map[string]any)
		if desc, _ := nameProp["description"].(string); !strings.Contains(desc, "without line") {
			t.Errorf("%s: name description does not mention the symbol form: %q", name, desc)
		}
	}
}

func TestRenameBySymbol(t *testing.T) {
	cs, dir := setup(t)
	out, _ := call(t, cs, "rename", map[string]any{"file": "calc/calc.go", "name": "Rate", "new_name": "Factor"})
	mustContain(t, "rename", out, "APPLIED rename")
	app, _ := os.ReadFile(filepath.Join(dir, "app/app.go"))
	mustContain(t, "app.go", string(app), "calc.Factor")
}

// Results show what changed and where, without context lines: the model
// reads every result again on each later turn.
func TestDiffsAreCompact(t *testing.T) {
	cs, _ := setup(t)
	out, _ := call(t, cs, "add_parameter", map[string]any{"file": "calc/calc.go", "name": "Apply", "parameter": "k", "type": "int", "value": "0"})
	mustContain(t, "add_parameter", out, "APPLIED add_parameter", "-func Apply(n int) int", "+func Apply(n, k int) int")
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, " ") {
			t.Errorf("context line in the result: %q\n%s", l, out)
		}
	}
}

// A rename changes the same name everywhere, so its result lists where
// instead of showing each line twice.
func TestRenameListsLocations(t *testing.T) {
	cs, _ := setup(t)
	out, _ := call(t, cs, "rename", map[string]any{"file": "calc/calc.go", "name": "Apply", "new_name": "Scale"})
	mustContain(t, "rename", out, "APPLIED rename", "changed lines:\n  app/app.go: 5\n  calc/calc.go: 6\n")
	if strings.Contains(out, "func Scale") {
		t.Errorf("rename showed the changed lines:\n%s", out)
	}
}

// A batch runs several steps in one call, addressed by name so that a
// later step can use what an earlier one made, and undoes as one.
func TestBatch(t *testing.T) {
	cs, dir := setup(t)
	calc := filepath.Join(dir, "calc", "calc.go")
	before, _ := os.ReadFile(calc)
	out, _ := call(t, cs, "batch", map[string]any{"steps": []any{
		map[string]any{"tool": "rename", "args": map[string]any{"file": "calc/calc.go", "name": "Apply", "new_name": "Scale"}},
		map[string]any{"tool": "add_parameter", "args": map[string]any{"file": "calc/calc.go", "name": "Scale", "parameter": "k", "type": "int", "value": "0"}},
	}})
	mustContain(t, "batch", out, "APPLIED batch: 2 steps", "1. rename:", "2. add_parameter:", "+func Scale(n, k int) int")
	app, _ := os.ReadFile(filepath.Join(dir, "app", "app.go"))
	mustContain(t, "app.go", string(app), "calc.Scale(calc.Rate, 0)")

	out, _ = call(t, cs, "undo", map[string]any{})
	mustContain(t, "undo", out, "UNDONE batch: rename, add_parameter")
	if got, _ := os.ReadFile(calc); string(got) != string(before) {
		t.Errorf("one undo did not undo the batch:\n%s", got)
	}

	out, _ = call(t, cs, "batch", map[string]any{"report": "summary", "steps": []any{
		map[string]any{"tool": "rename", "args": map[string]any{"file": "calc/calc.go", "name": "Apply", "new_name": "Scale"}},
	}})
	mustContain(t, "summary", out, "APPLIED batch: 1 step\n1. rename:")
	if strings.Contains(out, "changed lines") {
		t.Errorf("report summary showed the change:\n%s", out)
	}
}

func TestBatchRefusals(t *testing.T) {
	cs, dir := setup(t)
	calc := filepath.Join(dir, "calc", "calc.go")
	before, _ := os.ReadFile(calc)

	out, _ := call(t, cs, "batch", map[string]any{"steps": []any{
		map[string]any{"tool": "rename", "args": map[string]any{"file": "calc/calc.go", "name": "Apply", "new_name": "Scale"}},
		map[string]any{"tool": "inspect", "args": map[string]any{}},
	}})
	mustContain(t, "unknown tool", out, `REFUSED batch: step 2: "inspect" is not a refactoring or edit`)

	out, _ = call(t, cs, "batch", map[string]any{"steps": []any{
		map[string]any{"tool": "rename", "args": map[string]any{"file": "calc/calc.go", "name": "Apply", "new_name": "Scale"}},
		map[string]any{"tool": "rename", "args": map[string]any{"file": "calc/calc.go", "name": "Apply", "new_name": "Other"}},
	}})
	mustContain(t, "refused step", out, "REFUSED batch at step 2 of 2 (rename)", "rolled back")
	if got, _ := os.ReadFile(calc); string(got) != string(before) {
		t.Errorf("a refused batch changed the code:\n%s", got)
	}

	out, _ = call(t, cs, "batch", map[string]any{"steps": []any{}})
	mustContain(t, "empty", out, "REFUSED batch: steps is empty")
}

// replace_text edits text no other tool reaches, such as a string
// literal, and, addressing text rather than lines, works after an earlier
// step of a batch changed the file.
func TestReplaceText(t *testing.T) {
	cs, dir := setup(t)
	os.WriteFile(filepath.Join(dir, "calc", "doc.go"), []byte("package calc\n\n// Usage is shown to callers.\nconst Usage = `scale n by the rate`\n"), 0o644)
	out, _ := call(t, cs, "batch", map[string]any{"steps": []any{
		map[string]any{"tool": "rename", "args": map[string]any{"file": "calc/calc.go", "name": "Rate", "new_name": "Factor"}},
		map[string]any{"tool": "replace_text", "args": map[string]any{"file": "calc/doc.go", "old": "by the rate", "new": "by the factor"}},
		map[string]any{"tool": "replace_text", "args": map[string]any{"file": "calc/calc.go", "old": "// Factor is the multiplier.", "new": "// Factor multiplies n in Apply."}},
	}})
	mustContain(t, "batch", out, "APPLIED batch: 3 steps", "3. replace_text:")
	doc, _ := os.ReadFile(filepath.Join(dir, "calc", "doc.go"))
	calc, _ := os.ReadFile(filepath.Join(dir, "calc", "calc.go"))
	mustContain(t, "files", string(doc)+string(calc), "`scale n by the factor`", "// Factor multiplies n in Apply.")
}

// A batch takes line numbers as they were when it began: after a step
// adds code above Apply, a later step still finds it at its old line.
func TestBatchLinesFromBeforeTheBatch(t *testing.T) {
	cs, dir := setup(t)
	out, _ := call(t, cs, "batch", map[string]any{"steps": []any{
		map[string]any{"tool": "add_declaration", "args": map[string]any{"file": "calc/calc.go", "before": "Rate", "content": "// Base is where counting starts.\nconst Base = 0"}},
		map[string]any{"tool": "rename", "args": map[string]any{"file": "calc/calc.go", "line": 6, "name": "Apply", "new_name": "Scale"}},
	}})
	mustContain(t, "batch", out, "APPLIED batch: 2 steps")
	app, _ := os.ReadFile(filepath.Join(dir, "app", "app.go"))
	mustContain(t, "app.go", string(app), "calc.Scale(")
}

// TestRefactoringLoop walks the loop the model is meant to follow.
func TestRefactoringLoop(t *testing.T) {
	cs, dir := setup(t)
	calcPath := filepath.Join(dir, "calc", "calc.go")

	out, _ := call(t, cs, "inspect", nil)
	mustContain(t, "overview", out, "calc/  (package calc)", "app/  (package app)", "calc_test.go")

	out, _ = call(t, cs, "inspect", map[string]any{"file": "calc/calc.go", "function": "Apply"})
	mustContain(t, "inspect function", out, "func Apply(n int) int   [1 line, complexity 1, nesting 0, no side effects]", "6  func Apply")

	out, isErr := call(t, cs, "rename", map[string]any{"file": "calc/calc.go", "line": 4, "name": "Rate", "new_name": "Factor"})
	if isErr {
		t.Fatalf("rename errored: %s", out)
	}
	mustContain(t, "rename", out, "APPLIED rename", "3 occurrences in 2 files", "app/app.go: 5", "calc/calc.go: 3-4, 6")

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
	// Code references change, and so does the doc comment that starts with
	// the name.
	want := strings.NewReplacer("var Rate", "var Factor", "* Rate", "* Factor", "// Rate is", "// Factor is").Replace(edited)
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

// The model wrote the code it adds, so the result says where it went
// instead of repeating it; imports the tool added are still shown.
func TestAddedCodeIsNotEchoed(t *testing.T) {
	cs, _ := setup(t)
	content := "package calc\n\n// Twice doubles n.\nfunc Twice(n int) int {\n\treturn 2 * n\n}\n"
	out, _ := call(t, cs, "create_file", map[string]any{"file": "calc/twice.go", "content": content})
	mustContain(t, "create_file", out, "APPLIED create_file", "[6 lines added as written: lines 1-6]")
	if strings.Contains(out, "return 2 * n") {
		t.Errorf("create_file echoed the content:\n%s", out)
	}

	decl := "// Label names n.\nfunc Label(n int) string {\n\treturn strconv.Itoa(n)\n}"
	out, _ = call(t, cs, "add_declaration", map[string]any{"file": "calc/twice.go", "content": decl})
	mustContain(t, "add_declaration", out, "APPLIED add_declaration", "lines added as written", `+import "strconv"`)
	if strings.Contains(out, "strconv.Itoa(n)") {
		t.Errorf("add_declaration echoed the content:\n%s", out)
	}
}
