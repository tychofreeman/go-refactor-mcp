package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings"
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
	if !strings.Contains(cs.InitializeResult().Instructions, "load the schema of every tool its steps use") {
		t.Error("server instructions do not say to load the schemas of a batch's tools")
	}
	if !strings.Contains(cs.InitializeResult().Instructions, "create_file for any new file") {
		t.Error("server instructions do not say create_file makes files that are not Go")
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

// The instructions' worked example of a batch runs as it is written, on
// a module like the one it names.
func TestInstructionsExampleRuns(t *testing.T) {
	_, example, ok := strings.Cut(Instructions, "Example batch")
	if !ok {
		t.Fatal("the instructions have no example batch")
	}
	start := strings.Index(example, "{")
	end := strings.Index(example, "\n\n")
	var args map[string]any
	if start < 0 || end < start || json.Unmarshal([]byte(example[start:end]), &args) != nil {
		t.Fatalf("the example is not one JSON object:\n%s", example)
	}
	cs, dir := setup(t)
	out, _ := call(t, cs, "batch", args)
	mustContain(t, "example batch", out, "APPLIED batch")
	got, _ := os.ReadFile(filepath.Join(dir, "calc", "calc.go"))
	if strings.Contains(string(got), "not implemented") {
		t.Errorf("the stub was not replaced:\n%s", got)
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

// The descriptions say what the tools do now: how a declaration can be
// addressed, what a result shows, and which way each slide goes.
func TestDescriptionsAreCurrent(t *testing.T) {
	cs, _ := setup(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]string{}
	for _, tool := range res.Tools {
		tools[tool.Name] = tool.Description
	}
	for _, name := range symbolTools {
		mustContain(t, name, tools[name], "Without line, name alone identifies")
	}
	if strings.Contains(tools["inline_variable"], "Without line") {
		t.Error("inline_variable finds only locals, but its description says it finds package-level symbols")
	}
	mustContain(t, "inline_variable", tools["inline_variable"], "function (\"Name\" or \"Type.Method\") and name")
	for name, desc := range tools {
		if strings.Contains(desc, "returns a diff") {
			t.Errorf("%s still says it returns a diff", name)
		}
	}
	mustContain(t, "rename", tools["rename"], "returns what changed")
	if strings.Contains(tools["move_statement_up"], "move_statement_down") || !strings.Contains(tools["move_statement_up"], "the one before it") {
		t.Errorf("move_statement_up describes both directions: %s", tools["move_statement_up"])
	}
	if !strings.Contains(tools["move_statement_down"], "the one after it") {
		t.Errorf("move_statement_down: %s", tools["move_statement_down"])
	}
	instr := cs.InitializeResult().Instructions
	mustContain(t, "instructions", instr, "as they were when the batch began")
	if strings.Contains(instr, "never reuse old ones") {
		t.Error("instructions still say never to reuse line numbers, which a batch now does")
	}
}

// A loaded tool's definition is sent again with every later request, so
// the tools a model loads most stay small, and the instructions say to
// load schemas alongside another call rather than in a turn of their own.
func TestLoadedToolsStaySmall(t *testing.T) {
	cs, _ := setup(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	budget := map[string]int{"batch": 2500, "replace_statements": 1700, "add_declaration": 1500}
	for _, tool := range res.Tools {
		max, ok := budget[tool.Name]
		if !ok {
			continue
		}
		b, err := json.Marshal(tool)
		if err != nil {
			t.Fatal(err)
		}
		if len(b) > max {
			t.Errorf("%s: definition is %d characters, more than %d", tool.Name, len(b), max)
		}
	}
	mustContain(t, "instructions", cs.InitializeResult().Instructions, "in the same response as another call")
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
	out, _ := call(t, cs, "replace_expression", map[string]any{"file": "calc/calc.go", "line": 6, "expression": "n * Rate", "new": "Rate * n"})
	mustContain(t, "replace_expression", out, "APPLIED replace_expression", "-func Apply(n int) int { return n * Rate }", "+func Apply(n int) int { return Rate * n }")
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

// inspect shows a file without its function bodies, and reads several
// targets in one call.
func TestInspectSkeletonAndTargets(t *testing.T) {
	cs, _ := setup(t)
	out, _ := call(t, cs, "inspect", map[string]any{"file": "calc/calc.go"})
	mustContain(t, "file", out, "function bodies elided", "6  func Apply(n int) int { return n * Rate }")
	out, _ = call(t, cs, "inspect", map[string]any{"targets": []any{
		map[string]any{"file": "calc/calc.go", "function": "Apply"},
		map[string]any{"file": "app/app.go", "start_line": 5, "end_line": 5},
	}})
	mustContain(t, "targets", out, "6  func Apply", "5  func Run() int")

	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name != "inspect" {
			continue
		}
		schema, _ := tool.InputSchema.(map[string]any)
		props, _ := schema["properties"].(map[string]any)
		if props["targets"] == nil {
			t.Errorf("inspect's schema has no targets: %v", schema)
		}
		mustContain(t, "inspect description", tool.Description, "targets", "without their bodies")
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
	mustContain(t, "batch", out, "APPLIED batch: 2 steps", "1. rename:", "2. add_parameter:", "changed lines:\n  app/app.go: 5")
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

	// A tool's argument put beside the steps is explained, not just refused.
	out, _ = call(t, cs, "batch", map[string]any{"file": "calc/calc.go", "steps": []any{
		map[string]any{"tool": "rename", "args": map[string]any{"name": "Apply", "new_name": "Scale"}},
	}})
	mustContain(t, "argument beside the steps", out, `unknown field "file"`, "steps (required)", "each step's args")
}

// A step after which the module no longer loads is refused and put back,
// alone or in a batch, whose earlier steps are rolled back with it.
func TestStepThatBreaksLoadingIsRefused(t *testing.T) {
	cs, dir := setup(t)
	gomod := filepath.Join(dir, "go.mod")
	calc := filepath.Join(dir, "calc", "calc.go")
	modBefore, _ := os.ReadFile(gomod)
	calcBefore, _ := os.ReadFile(calc)
	breakMod := map[string]any{"file": "go.mod", "old": "module example.com/demo", "new": "modul example.com/demo"}

	out, _ := call(t, cs, "replace_text", breakMod)
	mustContain(t, "replace_text", out, "REFUSED", "would no longer load")
	if got, _ := os.ReadFile(gomod); string(got) != string(modBefore) {
		t.Errorf("go.mod not put back:\n%s", got)
	}
	out, _ = call(t, cs, "undo", map[string]any{})
	mustContain(t, "undo", out, "Nothing to undo")

	out, _ = call(t, cs, "batch", map[string]any{"steps": []any{
		map[string]any{"tool": "rename", "args": map[string]any{"file": "calc/calc.go", "name": "Apply", "new_name": "Scale"}},
		map[string]any{"tool": "replace_text", "args": breakMod},
	}})
	mustContain(t, "batch", out, "REFUSED", "step 2", "would no longer load")
	if got, _ := os.ReadFile(calc); string(got) != string(calcBefore) {
		t.Errorf("step 1 not rolled back:\n%s", got)
	}
	if got, _ := os.ReadFile(gomod); string(got) != string(modBefore) {
		t.Errorf("go.mod not put back:\n%s", got)
	}
	out, _ = call(t, cs, "undo", map[string]any{})
	mustContain(t, "undo", out, "Nothing to undo")
}

// undo only needs the files, so it works while the module does not load:
// here go.mod was broken by hand after a rename.
func TestUndoWhileTheModuleDoesNotLoad(t *testing.T) {
	cs, dir := setup(t)
	calc := filepath.Join(dir, "calc", "calc.go")
	before, _ := os.ReadFile(calc)
	out, _ := call(t, cs, "rename", map[string]any{"file": "calc/calc.go", "name": "Apply", "new_name": "Scale"})
	mustContain(t, "rename", out, "APPLIED")
	gomod := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(gomod, []byte("modul example.com/demo\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ = call(t, cs, "undo", map[string]any{})
	mustContain(t, "undo", out, "UNDONE rename", "The module does not load")
	if got, _ := os.ReadFile(calc); string(got) != string(before) {
		t.Errorf("rename not undone:\n%s", got)
	}
	out, _ = call(t, cs, "undo", map[string]any{})
	mustContain(t, "undo", out, "Nothing to undo")
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

// With check "end", a declaration and its first use can be two steps;
// without, the batch runs again with check "end" (see refactor.RunBatch).
func TestBatchCheckAtEnd(t *testing.T) {
	cs, dir := setup(t)
	more := filepath.Join(dir, "calc", "more.go")
	os.WriteFile(more, []byte("package calc\n\nfunc Sum(a, b int) int {\n\treturn a + b\n}\n"), 0o644)
	steps := []any{
		map[string]any{"tool": "insert_statements", "args": map[string]any{"file": "calc/more.go", "line": 4, "where": "before", "content": "total := a + b"}},
		map[string]any{"tool": "replace_statements", "args": map[string]any{"file": "calc/more.go", "start_line": 4, "end_line": 4, "content": "return total"}},
	}
	out, _ := call(t, cs, "batch", map[string]any{"check": "end", "steps": steps})
	mustContain(t, "checked at the end", out, "APPLIED batch: 2 steps")
	got, _ := os.ReadFile(more)
	mustContain(t, "more.go", string(got), "\ttotal := a + b\n\treturn total\n")
}

// A step can address the lines an earlier step made: an id, then
// $id.end, $prev.end+1, and so on.
func TestBatchStepReferences(t *testing.T) {
	cs, dir := setup(t)
	more := filepath.Join(dir, "calc", "more.go")
	os.WriteFile(more, []byte("package calc\n\nfunc Sum(a, b int) int {\n\treturn a + b\n}\n"), 0o644)
	out, _ := call(t, cs, "batch", map[string]any{"check": "end", "steps": []any{
		map[string]any{"id": "total", "tool": "insert_statements", "args": map[string]any{"file": "calc/more.go", "line": 4, "where": "before", "content": "total := a + b"}},
		map[string]any{"tool": "insert_statements", "args": map[string]any{"file": "calc/more.go", "line": "$total.end", "where": "after", "content": "total *= 2"}},
		map[string]any{"tool": "replace_statements", "args": map[string]any{"file": "calc/more.go", "start_line": "$prev.end+1", "end_line": "$prev.end+1", "content": "return total"}},
	}})
	mustContain(t, "batch", out, "APPLIED batch: 3 steps")
	got, _ := os.ReadFile(more)
	mustContain(t, "more.go", string(got), "\ttotal := a + b\n\ttotal *= 2\n\treturn total\n}")
}

// The import a step adds is not part of the lines it made.
func TestBatchStepReferenceSkipsImports(t *testing.T) {
	cs, dir := setup(t)
	more := filepath.Join(dir, "calc", "more.go")
	os.WriteFile(more, []byte("package calc\n\nfunc Sum(a, b int) int {\n\treturn a + b\n}\n"), 0o644)
	out, _ := call(t, cs, "batch", map[string]any{"check": "end", "steps": []any{
		map[string]any{"tool": "insert_statements", "args": map[string]any{"file": "calc/more.go", "line": 4, "where": "before", "content": "s := strings.Repeat(\"x\", a)"}},
		map[string]any{"tool": "insert_statements", "args": map[string]any{"file": "calc/more.go", "line": "$prev.end", "where": "after", "content": "_ = s"}},
	}})
	mustContain(t, "batch", out, "APPLIED batch: 2 steps")
	got, _ := os.ReadFile(more)
	mustContain(t, "more.go", string(got), "import \"strings\"", "\ts := strings.Repeat(\"x\", a)\n\t_ = s\n\treturn a + b\n")
}

// A test first: write it, then declare stubs for what it calls, in one
// batch.
func TestBatchTestThenStubs(t *testing.T) {
	cs, dir := setup(t)
	test := "package calc\n\nimport \"testing\"\n\nfunc TestHalf(t *testing.T) {\n\tif got := Half(4); got != 2 {\n\t\tt.Errorf(\"Half(4) = %d\", got)\n\t}\n}\n"
	steps := []any{
		map[string]any{"tool": "create_file", "args": map[string]any{"file": "calc/half_test.go", "content": test}},
		map[string]any{"tool": "create_from_usage", "args": map[string]any{"file": "calc/half_test.go"}},
	}
	out, _ := call(t, cs, "batch", map[string]any{"check": "end", "steps": steps})
	mustContain(t, "batch", out, "APPLIED batch: 2 steps", "chose: func Half(n int) int, in calc/")
	got, _ := os.ReadFile(filepath.Join(dir, "calc", "half_test.go"))
	mustContain(t, "half_test.go", string(got), "Half(4)")
}

// A batch, or a single edit, that calls functions nothing declares gets
// stubs for them, without check "end" or a create_from_usage step.
func TestStubsForWhatEditsCall(t *testing.T) {
	cs, dir := setup(t)
	test := "package calc\n\nimport \"testing\"\n\nfunc TestHalf(t *testing.T) {\n\tif got := Half(4); got != 2 {\n\t\tt.Errorf(\"Half(4) = %d\", got)\n\t}\n}\n"
	out, _ := call(t, cs, "batch", map[string]any{"steps": []any{
		map[string]any{"tool": "create_file", "args": map[string]any{"file": "calc/half_test.go", "content": test}},
	}})
	mustContain(t, "batch", out, "APPLIED batch: 2 steps", "2. create_from_usage: ", "chose: func Half(n int) int, in calc/", "since the steps call Half and nothing declares it")

	out, _ = call(t, cs, "replace_statements", map[string]any{"file": "calc/calc.go", "function": "Apply", "content": "return Double(n) * Rate"})
	mustContain(t, "replace_statements", out, "APPLIED batch: 2 steps", "chose: func Double(n int) int")
	got, _ := os.ReadFile(filepath.Join(dir, "calc", "calc.go"))
	mustContain(t, "calc.go", string(got), "return Double(n) * Rate", "func Double(n int) int")

	// A name that is not called, such as a variable, gets no stub, and the
	// others still do; the batch is refused for it.
	out, _ = call(t, cs, "replace_statements", map[string]any{"file": "calc/calc.go", "function": "Apply", "content": "_ = factor\nreturn Triple(n) * Rate"})
	mustContain(t, "replace_statements", out, "REFUSED replace_statements", "With stubs for the functions it calls, the errors left were:\n  calc/calc.go:7:6: undefined: factor")
	if strings.Contains(out, "undefined: Triple") {
		t.Errorf("Triple should have had a stub:\n%s", out)
	}
}

// TestRefactoringLoop walks the loop the model is meant to follow.
func TestRefactoringLoop(t *testing.T) {
	cs, dir := setup(t)
	calcPath := filepath.Join(dir, "calc", "calc.go")

	out, _ := call(t, cs, "inspect", nil)
	mustContain(t, "overview", out, "calc (", "app (", "calc_test.go")

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
	broken := "package calc\n\nimport \"testing\"\n\nfunc TestHalf(t *testing.T) {\n\tif half != 2 {\n\t\tt.Fatal()\n\t}\n}\n"
	out, _ := call(t, cs, "create_file", map[string]any{"file": "calc/half_test.go", "content": broken})
	if !strings.Contains(out, "REFUSED create_file") || !strings.Contains(out, "undefined: half") {
		t.Fatalf("want a file that does not type-check refused with its error, got:\n%s", out)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("refused file was left on disk: %v", err)
	}
	ok := strings.ReplaceAll(broken, "half != 2", "Apply(2) != 2*Rate")
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

// The other edits do not repeat the code the model sent either; what they
// replaced is still shown.
func TestEditsDoNotEchoTheirCode(t *testing.T) {
	cs, dir := setup(t)
	os.WriteFile(filepath.Join(dir, "calc", "more.go"), []byte("package calc\n\ntype Pair struct {\n\tA int\n}\n\nfunc Sum(p Pair) int {\n\ts := p.A\n\treturn s\n}\n"), 0o644)

	out, _ := call(t, cs, "replace_statements", map[string]any{"file": "calc/more.go", "start_line": 8, "end_line": 9, "content": "total := p.A * 2\nreturn total"})
	mustContain(t, "replace_statements", out, "APPLIED replace_statements", "-\ts := p.A", "lines added as written")
	if strings.Contains(out, "total := p.A * 2") {
		t.Errorf("replace_statements echoed its content:\n%s", out)
	}

	out, _ = call(t, cs, "insert_statements", map[string]any{"file": "calc/more.go", "line": 9, "where": "before", "content": "check := total\n_ = check"})
	mustContain(t, "insert_statements", out, "APPLIED insert_statements", "lines added as written")
	if strings.Contains(out, "check := total") {
		t.Errorf("insert_statements echoed its content:\n%s", out)
	}

	out, _ = call(t, cs, "add_field", map[string]any{"file": "calc/more.go", "name": "Pair", "field": "B int\nC string"})
	mustContain(t, "add_field", out, "APPLIED add_field")
	if strings.Contains(out, "C string") {
		t.Errorf("add_field echoed its fields:\n%s", out)
	}
}

// A signature change edits every call the same way, so its result lists
// where, as a rename's does.
func TestSignatureChangesListLocations(t *testing.T) {
	cs, _ := setup(t)
	out, _ := call(t, cs, "add_parameter", map[string]any{"file": "calc/calc.go", "name": "Apply", "parameter": "k", "type": "int", "value": "0"})
	mustContain(t, "add_parameter", out, "APPLIED add_parameter", "changed lines:", "app/app.go: 5")
	if strings.Contains(out, "+func Apply") {
		t.Errorf("add_parameter showed the diff:\n%s", out)
	}
}

// The instructions list the tools by what the model wants to do; every
// tool must be in that list.
func TestInstructionsNameEveryTool(t *testing.T) {
	_, groups, ok := strings.Cut(Instructions, "Which tool, by what you want to do:")
	if !ok {
		t.Fatal("the instructions have no list of tools by intention")
	}
	groups, _, _ = strings.Cut(groups, "\n\n")
	names := []string{"inspect", "batch", "check", "undo"}
	for _, r := range refactorings.All() {
		names = append(names, r.Name())
	}
	for _, n := range names {
		if !regexp.MustCompile(`\b` + n + `\b`).MatchString(groups) {
			t.Errorf("%s is not in the instructions' list of tools", n)
		}
	}
}
