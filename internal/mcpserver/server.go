// Package mcpserver exposes the refactoring engine as an MCP server.
//
// Tools:
//   - one tool per registered refactoring (rename, ...), which applies a
//     single step and returns the diff or the reason it was refused;
//   - inspect: module overview, a file without its function bodies, numbered
//     source of functions or lines, several in one call;
//   - undo: revert the last applied refactoring;
//   - check: report type errors and optionally run go test.
//
// The server holds one workspace for the session so that undo works across
// calls, and refreshes it before every call so direct edits made between
// calls (by the model or a person) are never overwritten with stale offsets.
package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tychofreeman/go-refactor-mcp/internal/inspect"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// Version is reported to MCP clients.
const Version = "0.2.0"

// Instructions are sent to the client at initialization; MCP clients such
// as Claude Code show them to the model. They set the pace of the loop.
const Instructions = `Refactoring and editing tools for the Go module in this project.

Two kinds of tool, by what they guarantee:
- Refactorings preserve behavior. They find every reference with the type checker (every use, call and comment that names it), so there is no need to search for them first; they refuse changes that would alter behavior.
- Edits change behavior on purpose: the step that adds or changes a feature.
Every step, refactoring or edit, starts from code that type-checks and must leave code that type-checks; a step that would not is refused with the errors, and nothing changes. An edit may also start from code that does not type-check if it fixes some of the errors and adds none (add_field for a field a test written first uses). And an edit that calls functions or methods nothing declares yet gets stubs for them (create_from_usage), so to write a failing test first, just write the test.
Some tools choose for you (create_from_usage chooses a stub's signature and file); their result lists each choice on a "chose:" line. Check them: give the choice explicitly, or undo, to change it.

Which tool, by what you want to do:
- Read the code: inspect.
- Rename or move: rename, move_declaration.
- Split or join code (refactorings): extract_function, inline_function, extract_variable, inline_variable, extract_struct, merge_duplicate_function, function_to_method, method_to_function; fold replaces code that does what an existing function does with calls to it; parameterize makes functions that differ only in constants calls of one that takes them.
- Change a signature and every call (refactorings): add_parameter, remove_parameter, move_parameter, introduce_parameter, change_parameter_type, add_result, change_field_type; an interface method changes with its implementations.
- Simplify control flow (refactorings): invert_if, remove_else, move_statement_up, move_statement_down, remove_dead_code.
- Add code (edits): create_file for any new file, Go or not (test fixtures, JSON, go.mod; several at once in one batch), add_declaration, add_field, add_case, add_element, insert_statements; create_from_usage declares the functions, methods, types and fields code uses before they exist, and the methods a type lacks to implement an interface it is used as.
- Change code (edits): replace_statements, replace_expression, replace_declaration (a whole declaration, by name); change_type for what a type (an alias) is, with the fixes it lists in the same batch; fields_to_map moves fields into a map field (literals and reads too); replace_text for string literals, comments and files that are not Go.
- Move or delete files and directories (fixtures, docs, data): move_file, delete_file.
- Several steps in one call: batch. Then: check, and undo to revert a step.

How to work with them:
1. Call inspect (no arguments) for an overview. Inspect a file to see its declarations without their bodies, with line numbers: a third to a half of reading the whole file. Then read what you will change: inspect with function, or start_line/end_line, or several at once with targets. Each function shows its complexity, nesting, and whether calling it has side effects (calls of a function without them are easier to move or reorder).
2. To change a feature, first refactor so the change is easy, then make the change with an edit: "make the change easy, then make the easy change".
3. Apply ONE tool per call, or, when you can plan several steps from what you have read, send them together with batch: one call, rolled back as a whole if any step is refused. A step's args are that tool's own arguments, so if your client loads tool schemas on demand, first load the schema of every tool its steps use, all in one request, rather than guessing their names. Load schemas in the same response as another call, never in a turn of their own: when you read the code you will change, load the tools you will change it with in that response. Each call returns APPLIED with what changed, or REFUSED with the reason; a refusal changes nothing, so read it and choose differently.
4. Line numbers change after edits. Before a single step that needs them, re-inspect the code an earlier step changed. In a batch, give line numbers as they were when the batch began (from the inspect you planned it with): the batch moves them past the earlier steps. To address what an earlier step of the batch made, give a reference instead of a number: "$prev.end", "$2.start", "$helper.end+1" (see batch). Tools that target a declaration (rename, add_parameter, move_declaration, ...) also accept file and name without line: name is then a package-level symbol of file's package (Name, Type.Method or Type.Field), which stays valid after edits.
5. Use check (with run_tests) at the end, and undo if a step made the code worse. Every step can be undone.

Example batch, for a feature added test first: the test calls Discount, which nothing declares, so it gets a stub; the next step replaces the stub with the real function; the last renames by name. One call, one turn:
{"report": "summary", "steps": [
  {"tool": "add_declaration", "args": {"file": "calc/calc_test.go", "content": "func TestDiscount(t *testing.T) {\n\tif Discount(200, 10) != 180 {\n\t\tt.Fatal(\"wrong\")\n\t}\n}"}},
  {"tool": "add_declaration", "args": {"file": "calc/calc.go", "content": "// Discount is price less pct percent of it.\nfunc Discount(price, pct int) int { return price - price*pct/100 }"}},
  {"tool": "rename", "args": {"file": "calc/calc.go", "name": "Apply", "new_name": "Scale"}}
]}

Prefer these tools over editing files by hand for everything they cover.`

// Server is the refactoring MCP server for one module.
type Server struct {
	// FullDiffs shows results with three lines of context around each
	// change, as diff -u does; by default they have none.
	FullDiffs bool
	dir       string
	patterns  []string

	mu sync.Mutex
	ws *workspace.Workspace // loaded lazily, refreshed before each call
}

// New returns a server for the module containing dir.
func New(dir string, patterns ...string) *Server {
	return &Server{dir: dir, patterns: patterns}
}

// MCP builds the MCP server with all tools registered.
func (s *Server) MCP() *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "gorefactor", Version: Version}, &mcp.ServerOptions{
		Instructions: Instructions,
	})

	notDestructive := false
	for _, r := range refactorings.All() {
		r := r
		srv.AddTool(&mcp.Tool{
			Name:        r.Name(),
			Description: r.Description() + symbolNote(r) + "\nApplies the change and returns what changed; undo reverts it.",
			InputSchema: r.InputSchema(),
			Annotations: &mcp.ToolAnnotations{DestructiveHint: &notDestructive},
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return s.refactor(r, req.Params.Arguments), nil
		})
	}

	srv.AddTool(&mcp.Tool{
		Name: "inspect",
		Description: `Read Go code with line numbers. Use it before every refactoring to get exact line numbers, and again after each change.
- No arguments: overview of the module's packages and files.
- file: the file with its functions without their bodies, a third to a half of reading it whole: declarations, doc comments and types in full, and in place of each body the lines it takes, with the function's length, cyclomatic complexity, nesting depth, and whether calling it has side effects (with the first reason).
- file + function ("Name" or "Type.Method"): that function's source.
- file + start_line/end_line: those lines.
- targets: several of these in one call, e.g. [{"file": "a.go", "function": "F"}, {"file": "b.go", "start_line": 10, "end_line": 30}].`,
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":       {"type": "string",  "description": "File path relative to the module root."},
    "function":   {"type": "string",  "description": "Function name, or Type.Method for a method."},
    "start_line": {"type": "integer", "description": "First line to show (1-based)."},
    "end_line":   {"type": "integer", "description": "Last line to show (inclusive)."},
    "targets":    {"type": "array", "description": "Several views in one call, in place of the arguments above: each a file, with function or start_line/end_line to read part of it.", "items": {"type": "object", "properties": {"file": {"type": "string"}, "function": {"type": "string"}, "start_line": {"type": "integer"}, "end_line": {"type": "integer"}}, "required": ["file"], "additionalProperties": false}}
  },
  "additionalProperties": false
}`),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return s.inspect(req.Params.Arguments), nil
	})

	srv.AddTool(&mcp.Tool{
		Name:        "undo",
		Description: "Revert the most recent refactoring or edit applied in this session and return the reverting diff. Can be called repeatedly to step further back. Refuses if a changed file was edited since. Works while the module does not load.",
		InputSchema: json.RawMessage(`{"type": "object", "properties": {}, "additionalProperties": false}`),
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &notDestructive},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return s.undo(), nil
	})

	srv.AddTool(&mcp.Tool{
		Name:        "check",
		Description: "Report whether the module type-checks. With run_tests, also run `go test` (on packages, default ./...) to confirm behavior is unchanged; use it after a series of refactorings.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "run_tests": {"type": "boolean", "description": "Also run go test."},
    "packages":  {"type": "string",  "description": "Package pattern for go test, relative to the module root (default ./...)."}
  },
  "additionalProperties": false
}`),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return s.check(ctx, req.Params.Arguments), nil
	})
	srv.AddTool(&mcp.Tool{
		Name: "batch",
		Description: `Run several refactorings and edits in order, in one call, planned from one inspect. Each step is {tool, args}: a tool and the arguments it takes when called alone; each runs on the code the earlier steps left.
Give line numbers and columns as in the code you inspected before the batch: the batch moves them past the earlier steps. A line an earlier step rewrote cannot be given by number. Address a declaration by name (file and name, no line), or give a reference in place of line, start_line or end_line: "$prev.start" or "$prev.end" (the first or last line the step before added or changed in that file), "$2.end" (step 2's), "$helper.end" (the step with "id": "helper"), with an offset such as "$prev.end+1". A step that changed several places, such as a rename, has no range.
If a step is refused, nothing changes and the result says which step and why; one undo reverts the whole batch. A step {"tool": "check"} is a checkpoint: the code must type-check there (args run_tests and packages also run go test); if a later step is refused, the steps up to the last checkpoint that passed are kept, as one change. Independent steps can instead be separate calls in one turn.
check "end" type-checks only after the last step, so edits may pass through code that does not compile yet (a declaration, then its first use); refactorings still need code that type-checks, so just before one the stubs and imports earlier edits need are added. A batch whose edit does not type-check runs again with "end", and functions the steps call but nothing declares get stubs (create_from_usage).`,
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "steps": {
      "type": "array",
      "description": "The steps, in order.",
      "items": {
        "type": "object",
        "properties": {
          "id":   {"type": "string", "description": "Optional: later steps address its lines as $id.start, $id.end."},
          "tool": {"type": "string", "description": "A refactoring or edit."},
          "args": {"type": "object", "description": "That tool's arguments (see its schema)."}
        },
        "required": ["tool", "args"],
        "additionalProperties": false
      }
    },
    "report": {"type": "string", "enum": ["diff", "summary"], "description": "After the steps' summaries, the change (default) or nothing more."},
    "check":  {"type": "string", "enum": ["each", "end"], "description": "Type-check after each step (default) or only after the last."}
  },
  "required": ["steps"],
  "additionalProperties": false
}`),
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &notDestructive},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return s.batch(req.Params.Arguments), nil
	})
	return srv
}

// symbolNote tells, for a tool whose line is optional, how it finds a
// declaration by name alone; the tools' own descriptions were written for
// file, line and name. A tool for locals only has none.
func symbolNote(r refactor.Refactoring) string {
	if _, ok := r.(interface{ LocalsOnly() }); ok {
		return ""
	}
	schema := r.InputSchema()
	var s struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if json.Unmarshal(schema, &s) != nil {
		return ""
	}
	_, line := s.Properties["line"]
	_, name := s.Properties["name"]
	if !line || !name || slices.Contains(s.Required, "line") {
		return ""
	}
	return "\nWithout line, name alone identifies it as a package-level symbol of file's package (Name, or Type.Method or Type.Field); unlike a line number, a name stays valid after edits."
}

// workspace returns the session's workspace, loading it on first use and
// refreshing it if files changed on disk. Callers hold s.mu.
func (s *Server) workspace() (*workspace.Workspace, error) {
	if s.ws == nil {
		ws, err := workspace.Load(s.dir, s.patterns...)
		if err != nil {
			return nil, err
		}
		s.ws = ws
		return ws, nil
	}
	if _, err := s.ws.Refresh(); err != nil {
		return nil, err
	}
	return s.ws, nil
}

// Preload starts loading the workspace in the background, so that the
// first call does not wait for all of it (minutes on a large module, the
// first time); the channel closes when it is done. Calls meanwhile wait
// for it, and a load that fails is tried again by the next call.
func (s *Server) Preload() <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.ws == nil {
			s.workspace()
		}
	}()
	return done
}

func (s *Server) refactor(r refactor.Refactoring, args json.RawMessage) *mcp.CallToolResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	ws, err := s.workspace()
	if err != nil {
		return toolError(err)
	}
	text, refused, err := refactorings.RunStep(ws, r, args, refactor.Options{Compact: !s.FullDiffs})
	if err != nil {
		return toolError(err)
	}
	if !refused {
		text += "\nRe-inspect the changed code before the next step; line numbers may have shifted.\n"
	}
	return textResult(text)
}

func (s *Server) inspect(args json.RawMessage) *mcp.CallToolResult {
	var q inspect.Query
	if err := refactor.DecodeArgs(args, &q); err != nil {
		return textResult(err.Error())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ws, err := s.workspace()
	if err != nil {
		return toolError(err)
	}
	out, err := inspect.Run(ws, q)
	if err != nil {
		if refactor.IsRefusal(err) {
			return textResult(err.Error())
		}
		return toolError(err)
	}
	return textResult(out)
}

func (s *Server) undo() *mcp.CallToolResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Undoing only needs the files, so it goes ahead even when the module
	// does not load (a go.mod broken by hand, say), and says so.
	var loadErr error
	ws := s.ws
	if ws == nil {
		var err error
		if ws, err = s.workspace(); err != nil {
			return toolError(err)
		}
	} else if _, err := ws.Refresh(); err != nil {
		loadErr = err
	}
	if !ws.CanUndo() {
		return textResult("Nothing to undo: no refactoring has been applied in this session." + notLoading(loadErr))
	}
	c, err := ws.Undo()
	if c.Label == "" { // not undone
		return textResult("Cannot undo: " + err.Error() + notLoading(loadErr))
	}
	if err == nil {
		err = loadErr
	}
	return textResult(fmt.Sprintf("UNDONE %s\n\n%s", strings.TrimPrefix(c.Label, "undo "), c.Diff) + notLoading(err))
}

// notLoading is the line undo adds when the module does not load, or "".
func notLoading(err error) string {
	if err == nil {
		return ""
	}
	return "\nThe module does not load, so the other tools cannot run until it does: " + err.Error()
}

// batch runs the steps of one batch call; see the tool's description.
func (s *Server) batch(args json.RawMessage) *mcp.CallToolResult {
	batch, refused := refactorings.ParseBatch(args)
	if refused != "" {
		return textResult(refused)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ws, err := s.workspace()
	if err != nil {
		return toolError(err)
	}
	b, err := refactor.RunBatch(ws, batch.Steps, refactor.Options{Compact: !s.FullDiffs, Unchecked: batch.Unchecked, Declare: refactorings.Declarer(), Imports: refactorings.Importer()})
	if err != nil {
		return toolError(err)
	}
	if batch.Summary {
		b.Diff = ""
	}
	return textResult(b.String())
}

// testTimeout bounds `go test` so a hanging test cannot stall the session.
var testTimeout = 10 * time.Minute

func (s *Server) check(ctx context.Context, args json.RawMessage) *mcp.CallToolResult {
	var a struct {
		RunTests bool   `json:"run_tests"`
		Packages string `json:"packages"`
	}
	if err := refactor.DecodeArgs(args, &a); err != nil {
		return textResult(err.Error())
	}
	s.mu.Lock()
	ws, err := s.workspace()
	var errs []string
	var root string
	if err == nil {
		errs, root = ws.Errors(), ws.Root
	}
	s.mu.Unlock()
	if err != nil {
		return toolError(err)
	}

	var sb strings.Builder
	if len(errs) == 0 {
		sb.WriteString("TYPE-CHECK OK: the module compiles.\n")
	} else {
		fmt.Fprintf(&sb, "TYPE-CHECK FAILED (%d errors):\n  %s\n", len(errs), strings.Join(errs, "\n  "))
	}
	if !a.RunTests {
		return textResult(sb.String())
	}

	pattern := a.Packages
	if pattern == "" {
		pattern = "./..."
	}
	if strings.HasPrefix(pattern, "-") {
		return textResult("packages must be a package pattern, not a flag")
	}
	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", pattern)
	cmd.Dir = root
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err = cmd.Run()
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		fmt.Fprintf(&sb, "\nTESTS TIMED OUT after %s.\n", testTimeout)
	case err != nil:
		sb.WriteString("\nTESTS FAILED:\n")
	default:
		sb.WriteString("\nTESTS PASSED:\n")
	}
	sb.WriteString(tail(out.String(), 150))
	return textResult(sb.String())
}

// tail keeps the last n lines of s, which is where go test puts failures
// and the summary.
func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= n {
		return strings.Join(lines, "\n") + "\n"
	}
	return fmt.Sprintf("... (%d earlier lines omitted)\n%s\n", len(lines)-n, strings.Join(lines[len(lines)-n:], "\n"))
}

func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

// toolError reports an internal failure (not a refusal) to the model.
func toolError(err error) *mcp.CallToolResult {
	r := textResult("error: " + err.Error())
	r.IsError = true
	return r
}
