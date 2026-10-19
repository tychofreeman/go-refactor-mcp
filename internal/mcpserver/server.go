// Package mcpserver exposes the refactoring engine as an MCP server.
//
// Tools:
//   - one tool per registered refactoring (rename, ...), which applies a
//     single step and returns the diff or the reason it was refused;
//   - inspect: module overview, file outline with complexity and side effects, numbered source;
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
Every step, refactoring or edit, starts from code that type-checks and must leave code that type-checks; a step that would not is refused with the errors, and nothing changes. To write a failing test first, send a batch with check "end": the step that writes the test, then create_from_usage to declare stubs for what it calls.
Some tools choose for you (create_from_usage chooses a stub's signature and file); their result lists each choice on a "chose:" line. Check them: give the choice explicitly, or undo, to change it.

Which tool, by what you want to do:
- Read the code: inspect.
- Rename or move: rename, move_declaration.
- Split or join code (refactorings): extract_function, inline_function, extract_variable, inline_variable, extract_struct, merge_duplicate_function, function_to_method, method_to_function.
- Change a signature and every call (refactorings): add_parameter, remove_parameter, move_parameter, introduce_parameter, change_parameter_type.
- Simplify control flow (refactorings): invert_if, remove_else, move_statement_up, move_statement_down, remove_dead_code.
- Add code (edits): create_file, add_declaration, add_field, add_case, add_element, insert_statements; create_from_usage declares the functions and methods code calls before they exist, and the methods a type lacks to implement an interface it is used as.
- Change code (edits): replace_statements, replace_expression; replace_text for string literals, comments and files that are not Go.
- Several steps in one call: batch. Then: check, and undo to revert a step.

How to work with them:
1. Call inspect (no arguments) for an overview, then inspect a file or function to read it with line numbers. The outline shows each function's complexity, nesting, and whether calling it has side effects (calls of a function without them are easier to move or reorder).
2. To change a feature, first refactor so the change is easy, then make the change with an edit: "make the change easy, then make the easy change".
3. Apply ONE tool per call, or, when you can plan several steps from what you have read, send them together with batch: one call, rolled back as a whole if any step is refused. Each call returns APPLIED with what changed, or REFUSED with the reason; a refusal changes nothing, so read it and choose differently.
4. Line numbers change after edits. Before a single step that needs them, re-inspect the code an earlier step changed. In a batch, give line numbers as they were when the batch began (from the inspect you planned it with): the batch moves them past the earlier steps. To address what an earlier step of the batch made, give a reference instead of a number: "$prev.end", "$2.start", "$helper.end+1" (see batch). Tools that target a declaration (rename, add_parameter, move_declaration, ...) also accept file and name without line: name is then a package-level symbol of file's package (Name, Type.Method or Type.Field), which stays valid after edits.
5. Use check (with run_tests) at the end, and undo if a step made the code worse. Every step can be undone.

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
			Description: r.Description() + symbolNote(r) + "\nApplies the change to disk and returns what changed (the changed lines without context; for a rename, where they are); it can be reverted with undo.",
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
- file: outline of the file (each declaration's line range; for functions also length, cyclomatic complexity, nesting depth, and whether calling it has side effects, with the first reason) followed by its source.
- file + function ("Name" or "Type.Method"): the outline plus that function's source.
- file + start_line/end_line: the outline plus those lines.`,
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":       {"type": "string",  "description": "File path relative to the module root."},
    "function":   {"type": "string",  "description": "Function name, or Type.Method for a method."},
    "start_line": {"type": "integer", "description": "First line to show (1-based)."},
    "end_line":   {"type": "integer", "description": "Last line to show (inclusive)."}
  },
  "additionalProperties": false
}`),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return s.inspect(req.Params.Arguments), nil
	})

	srv.AddTool(&mcp.Tool{
		Name:        "undo",
		Description: "Revert the most recent refactoring or edit applied in this session and return the reverting diff. Can be called repeatedly to step further back. Refuses if a changed file was edited since.",
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
		Description: `Run several refactorings and edits in order, in one call: plan the sequence from one inspect and send it here instead of calling the tools one at a time.
Each step is {tool, args}: a refactoring or edit (rename, add_parameter, extract_function, replace_statements, ...) and the arguments that tool takes. Each step runs on the code the earlier steps left and, like any step, must leave code that type-checks.
Give every line number as it is in the code you inspected before the batch: the batch moves it to where that line is after the earlier steps. So one inspect is enough to plan a whole sequence, e.g. extract_function the duplicated lines in each of three files, then merge_duplicate_function the new functions, then add_parameter to the one left. A line an earlier step rewrote cannot be addressed by number; address what that step made by name (file and name, no line), which also works for declarations generally, or by reference: in place of a number, line, start_line or end_line may be "$prev.start" or "$prev.end" (the first or last line the step before added or changed in the step's file, as it is now), "$2.end" (step 2's), or "$helper.end" (the step with "id": "helper"), with an offset such as "$prev.end+1". E.g. insert_statements with line "$prev.end" and where "after" adds code right after what the step before inserted. A step that changed several places of the file, such as a rename, has no one range to refer to.
Independent steps can also be sent as separate tool calls in one turn; a batch is for steps that build on each other.
If a step is refused, the steps before it are rolled back, so nothing changes, and the result says which step and why. If all apply, the result lists each step's summary and then the change as a whole; one undo reverts the whole batch.
report: "diff" (default) shows the change; "summary" shows only the steps' summaries.
check: "each" (default) type-checks after every step; "end" only after the last, so edits may pass through code that does not compile yet, such as a variable declared in one step and first used in the next. A refactoring in such a batch still needs code that type-checks where it runs. If the code does not type-check at the end, the whole batch is rolled back.`,
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "steps": {
      "type": "array",
      "description": "The steps, in order.",
      "items": {
        "type": "object",
        "properties": {
          "id":   {"type": "string", "description": "Optional: a name for the step, so later steps can address the lines it made as $id.start or $id.end."},
          "tool": {"type": "string", "description": "A refactoring or edit, e.g. rename."},
          "args": {"type": "object", "description": "The arguments that tool takes."}
        },
        "required": ["tool", "args"],
        "additionalProperties": false
      }
    },
    "report": {"type": "string", "enum": ["diff", "summary"], "description": "What the result shows after the steps' summaries: the change (default) or nothing more."},
    "check":  {"type": "string", "enum": ["each", "end"], "description": "When the code must type-check: after each step (default), or only after the last, so that edits can pass through code that does not compile yet (a declaration, then its first use). Refactorings still need code that type-checks."}
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

func (s *Server) refactor(r refactor.Refactoring, args json.RawMessage) *mcp.CallToolResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	ws, err := s.workspace()
	if err != nil {
		return toolError(err)
	}
	res, err := refactor.Run(ws, r, args, refactor.Options{Compact: !s.FullDiffs})
	if err != nil {
		return toolError(err)
	}
	text := res.String()
	if res.Applied {
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
	ws, err := s.workspace()
	if err != nil {
		return toolError(err)
	}
	if !ws.CanUndo() {
		return textResult("Nothing to undo: no refactoring has been applied in this session.")
	}
	c, err := ws.Undo()
	if err != nil {
		return textResult("Cannot undo: " + err.Error())
	}
	return textResult(fmt.Sprintf("UNDONE %s\n\n%s", strings.TrimPrefix(c.Label, "undo "), c.Diff))
}

// batch runs the steps of one batch call; see the tool's description.
func (s *Server) batch(args json.RawMessage) *mcp.CallToolResult {
	var a struct {
		Steps []struct {
			ID   string          `json:"id"`
			Tool string          `json:"tool"`
			Args json.RawMessage `json:"args"`
		} `json:"steps"`
		Report string `json:"report"`
		Check  string `json:"check"`
	}
	if err := refactor.DecodeArgs(args, &a); err != nil {
		return textResult("REFUSED batch: " + err.Error() + "\nbatch takes:\n  steps (required): the steps, in order, each {\"tool\": ..., \"args\": {...}}; a tool's own arguments (file, name, line, ...) go in each step's args\n  report: diff (the default) or summary")
	}
	if a.Check != "" && a.Check != "each" && a.Check != "end" {
		return textResult(fmt.Sprintf("REFUSED batch: check is %q; give each or end", a.Check))
	}
	if a.Report != "" && a.Report != "diff" && a.Report != "summary" {
		return textResult(fmt.Sprintf("REFUSED batch: report is %q; give diff or summary", a.Report))
	}
	if len(a.Steps) == 0 {
		return textResult("REFUSED batch: steps is empty; give the refactorings and edits to run, in order")
	}
	byName := map[string]refactor.Refactoring{}
	for _, r := range refactorings.All() {
		byName[r.Name()] = r
	}
	steps := make([]refactor.Step, len(a.Steps))
	for i, st := range a.Steps {
		r := byName[st.Tool]
		if r == nil {
			return textResult(fmt.Sprintf("REFUSED batch: step %d: %q is not a refactoring or edit; a batch runs tools such as rename, add_parameter or replace_statements, not inspect, undo, check or batch. Nothing changed.", i+1, st.Tool))
		}
		steps[i] = refactor.Step{ID: st.ID, Refactoring: r, Args: st.Args}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	ws, err := s.workspace()
	if err != nil {
		return toolError(err)
	}
	b, err := refactor.RunBatch(ws, steps, refactor.Options{Compact: !s.FullDiffs, Unchecked: a.Check == "end"})
	if err != nil {
		return toolError(err)
	}
	if a.Report == "summary" {
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
