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
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tychofreeman/go-refactor-mcp/internal/history"
	"github.com/tychofreeman/go-refactor-mcp/internal/lang"
	"github.com/tychofreeman/go-refactor-mcp/internal/lang/golang"
	"github.com/tychofreeman/go-refactor-mcp/internal/lang/typescript"
)

// Version is reported to MCP clients.
const Version = "0.5.0"

// BuildVersion is Version with what the build knows of its source: the
// module version it was installed at, or the commit it was built from
// and whether the tree had changes.
func BuildVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return Version
	}
	rev, modified := "", ""
	for _, s := range bi.Settings {
		switch {
		case s.Key == "vcs.revision":
			rev = s.Value
		case s.Key == "vcs.modified" && s.Value == "true":
			modified = ", modified"
		}
	}
	switch {
	case bi.Main.Version != "" && bi.Main.Version != "(devel)":
		return Version + " (" + bi.Main.Version + ")"
	case rev != "":
		return Version + " (commit " + rev[:min(len(rev), 12)] + modified + ")"
	}
	return Version
}

// Instructions are sent to the client at initialization; MCP clients such
// as Claude Code show them to the model. They set the pace of the loop.
const Instructions = `Refactoring and editing tools for the Go module in this project, and any other module a call names a file of by its path from this one (../tools/a.go).

Two kinds of tool, by what they guarantee:
- Refactorings preserve behavior. They find every reference with the type checker (every use and call, and a doc comment that starts with the name), so there is no need to search for them first; they refuse changes that would alter behavior.
- Edits change behavior on purpose: the step that adds or changes a feature.
Every step, refactoring or edit, starts from code that type-checks and must leave code that type-checks; a step that would not is refused with the errors, and nothing changes. An edit may also start from code that does not type-check if it fixes some of the errors and adds none (add_field for a field a test written first uses). And an edit that calls functions or methods nothing declares yet gets stubs for them (create_from_usage), so to write a failing test first, just write the test.
Some tools choose for you (create_from_usage chooses a stub's signature and file); their result lists each choice on a "chose:" line. Check them: give the choice explicitly, or undo, to change it.

Which tool, by what you want to do:
- Read the code: inspect.
- Rename or move: rename, move_declaration.
- Split or join code (refactorings): extract_function, inline_function, extract_variable, inline_variable, extract_struct, merge_duplicate_function, function_to_method, method_to_function, move_method (to a struct embedding its receiver's type); fold replaces code that does what an existing function does with calls to it; parameterize makes functions that differ only in constants calls of one that takes them; type_switch_to_method makes each case of a type switch a method of its type; prepare_switch_for_types prepares a switch on a value for its cases to move into types one at a time.
- Change a signature and every call (refactorings): add_parameter, remove_parameter, move_parameter, introduce_parameter, change_parameter_type, add_result, change_field_type; an interface method changes with its implementations.
- Simplify control flow (refactorings): invert_if, remove_else, move_statement_up, move_statement_down, remove_dead_code.
- Add code (edits): create_file for any new file, Go or not (test fixtures, JSON, go.mod; several at once in one batch), add_declaration, add_field, add_case, add_element, insert_statements; create_from_usage declares the functions, methods, types and fields code uses before they exist, and the methods a type lacks to implement an interface it is used as.
- Change code (edits): replace_statements, replace_expression, replace_declaration (a whole declaration, by name); change_type for what a type (an alias) is, with the fixes it lists in the same batch; replace_text for string literals, comments and files that are not Go.
- Move or delete files and directories (fixtures, docs, data): move_file, delete_file.
- Several steps in one call: batch. Then: check, and undo to revert a step.

How to work with them:
1. Call inspect (no arguments) for an overview. Inspect a file to see its declarations without their bodies, with line numbers: a third to a half of reading the whole file. Then read what you will change: inspect with function, or start_line/end_line, or several at once with targets; or several declarations by name alone with names; how each use treats a field or function, with uses. Each function shows its complexity, nesting, and whether calling it has side effects (calls of a function without them are easier to move or reorder).
2. Refactor only while tests pass: first to make the change easy, after it to remove duplication. A failing test needs passing code first.
3. Apply ONE tool per call, or, when you can plan several steps from what you have read, send them together with batch: one call, rolled back as a whole if any step is refused. A step's args are that tool's own arguments, so if your client loads tool schemas on demand, first load the schema of every tool its steps use, all in one request, rather than guessing their names. Load schemas in the same response as another call, never in a turn of their own: when you read the code you will change, load the tools you will change it with in that response. Each call returns APPLIED with what changed, or REFUSED with the reason; a refusal changes nothing, so read it and choose differently.
4. Line numbers change after edits. Before a single step that needs them, re-inspect the code an earlier step changed. In a batch, give line numbers as they were when the batch began (from the inspect you planned it with): the batch moves them past the earlier steps. To address what an earlier step of the batch made, give a reference instead of a number: "$prev.end", "$2.start", "$helper.end+1" (see batch). Tools that target a declaration (rename, add_parameter, move_declaration, ...) also accept file and name without line: name is then a package-level symbol of file's package (Name, Type.Method or Type.Field), which stays valid after edits.
5. Use check (with run_tests) at the end, and undo if a step made the code worse. Every step can be undone.

Example batch, for a feature added test first: the test calls Discount, which nothing declares, so it gets a stub; the next step replaces the stub with the least code that passes; then a rename. One call, one turn:
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
	// ReadByName makes inspect show source only for what a request
	// names (function, names, uses): lines by number are refused, and
	// the tool's description, schema and the instructions do not offer
	// them.
	ReadByName bool
	// NoCodeByText makes replace_text change a Go file only inside
	// string literals and comments: code is changed by the structured
	// edits. Published text is the same.
	NoCodeByText bool

	golang     *golang.Language     // nil when the root holds no Go
	typescript *typescript.Language // nil when it holds no TypeScript
	langs      *lang.Dispatcher     // chooses the language of each call
}

// byName removes what offers lines by number from the texts that
// describe inspect, for a server that reads source only by name.
var byName = strings.NewReplacer(
	"Read Go code with line numbers.", "Read Go code with line numbers; source is shown only for what a request names (function, names, uses), not lines by number.",
	"- file + start_line/end_line: those lines.\n", "",
	`{"file": "b.go", "start_line": 10, "end_line": 30}`, `{"file": "b.go", "function": "G"}`,
	`    "start_line": {"type": "integer", "description": "First line to show (1-based)."},`+"\n", "",
	`    "end_line":   {"type": "integer", "description": "Last line to show (inclusive)."},`+"\n", "",
	"with function or start_line/end_line to read part of it", "with function to read part of it",
	`, "start_line": {"type": "integer"}, "end_line": {"type": "integer"}`, "",
	"inspect with function, or start_line/end_line, or several at once with targets", "inspect with function, or several at once with targets",
)

// reads returns text, which describes inspect, as this server's reads
// allow and for the languages it serves.
func (s *Server) reads(text string) string {
	if s.ReadByName {
		text = byName.Replace(text)
	}
	switch {
	case s.typescript != nil && s.golang != nil:
		text = strings.Replace(text, "Read Go code", "Read Go and TypeScript code", 1)
	case s.typescript != nil:
		text = strings.Replace(text, "Read Go code", "Read TypeScript code", 1)
	}
	return text
}

// instructions are Instructions for the languages this server serves:
// TypeScript's alone, or Go's, naming TypeScript when it serves both.
func (s *Server) instructions() string {
	switch {
	case s.golang == nil:
		return s.reads(TypeScriptInstructions)
	case s.typescript != nil:
		return s.reads(strings.Replace(Instructions, "Refactoring and editing tools for the Go module in this project, and", "Refactoring and editing tools for the Go module in this project and for its TypeScript (the solution of the nearest tsconfig.json; each call goes to the language of the files it names), and", 1))
	}
	return s.reads(Instructions)
}

// New returns a server for the code at dir: Go for the module containing
// it (also when it holds neither language), TypeScript for the solution
// of the nearest tsconfig.json at or above it.
func New(dir string, patterns ...string) *Server {
	h := &history.Stack{}
	s := &Server{langs: &lang.Dispatcher{History: h}}
	goMod, tsconfig := above(dir, "go.mod"), above(dir, "tsconfig.json")
	if goMod || !tsconfig {
		s.golang = golang.New(dir, patterns, h)
		s.langs.Languages = append(s.langs.Languages, s.golang)
	}
	if tsconfig {
		s.typescript = typescript.New(dir, h)
		s.langs.Languages = append(s.langs.Languages, s.typescript)
	}
	return s
}

// above reports whether a file name is at dir or a directory above it.
func above(dir, name string) bool {
	d, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	for {
		if fi, err := os.Stat(filepath.Join(d, name)); err == nil && !fi.IsDir() {
			return true
		}
		parent := filepath.Dir(d)
		if parent == d {
			return false
		}
		d = parent
	}
}

// MCP builds the MCP server with all tools registered.
func (s *Server) MCP() *mcp.Server {
	if s.golang != nil {
		s.golang.FullDiffs, s.golang.ReadByName = s.FullDiffs, s.ReadByName
		s.golang.NoCodeByText = s.NoCodeByText
	}
	if s.typescript != nil {
		s.typescript.FullDiffs, s.typescript.ReadByName = s.FullDiffs, s.ReadByName
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "gorefactor", Version: Version}, &mcp.ServerOptions{
		Instructions: s.instructions(),
	})

	notDestructive := false
	for _, t := range s.langs.Tools() {
		name := t.Name
		srv.AddTool(&mcp.Tool{
			Name:        name,
			Description: t.Description + "\nApplies the change and returns what changed; undo reverts it.",
			InputSchema: t.Schema,
			Annotations: &mcp.ToolAnnotations{DestructiveHint: &notDestructive},
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return s.refactor(name, req.Params.Arguments), nil
		})
	}

	srv.AddTool(&mcp.Tool{
		Name: "inspect",
		Description: s.reads(`Read Go code with line numbers. Use it before every refactoring to get exact line numbers, and again after each change.
- No arguments: overview of the module's packages and files.
- file: the file with its functions without their bodies, a third to a half of reading it whole: declarations, doc comments and types in full, and in place of each body the lines it takes, with the function's length, cyclomatic complexity, nesting depth, and whether calling it has side effects (with the first reason).
- file + function ("Name" or "Type.Method"): that function's source.
- file + start_line/end_line: those lines.
- targets: several of these in one call, e.g. [{"file": "a.go", "function": "F"}, {"file": "b.go", "start_line": 10, "end_line": 30}].
- names: declarations by name, wherever in the module they are, without file: functions, methods, types, variables and constants, e.g. ["Water", "Bed.Plant", "Schedule"]. A name declared in several packages is refused with where each is: qualify it as "pkg.Name". One nothing declares is said to be free to declare, or why not (predeclared, an import's name): check new names so.
- uses: how each use treats something, before changing what it holds or means: every use of a field, function, method, variable or constant in the module, found by the type checker, under what is done with it (switch on it, compared, asserted, written, passed as an interface, called, read; for a type also type switch cases, conversions and literals), each with its line, e.g. ["Plant.Species"] lists "compared (2):  garden/bed.go:85  p.Species == \"cactus\"" and the switches with their cases.`),
		InputSchema: json.RawMessage(s.reads(`{
  "type": "object",
  "properties": {
    "file":       {"type": "string",  "description": "File path relative to the module root."},
    "function":   {"type": "string",  "description": "Function name, or Type.Method for a method."},
    "start_line": {"type": "integer", "description": "First line to show (1-based)."},
    "end_line":   {"type": "integer", "description": "Last line to show (inclusive)."},
    "targets":    {"type": "array", "description": "Several views in one call, in place of the arguments above: each a file, with function or start_line/end_line to read part of it.", "items": {"type": "object", "properties": {"file": {"type": "string"}, "function": {"type": "string"}, "start_line": {"type": "integer"}, "end_line": {"type": "integer"}}, "required": ["file"], "additionalProperties": false}},
    "names":      {"type": "array", "description": "Declarations to read by name, in place of the arguments above: Name, Type.Method, or pkg.Name.", "items": {"type": "string"}},
    "uses":       {"type": "array", "description": "Names whose every use to list by what it does with them, in place of the arguments above: Name, Type.Field, Type.Method, or pkg.Name.", "items": {"type": "string"}}
  },
  "additionalProperties": false
}`)),
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
    "race":      {"type": "boolean", "description": "With run_tests, run them with -race."},
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

// Preload starts loading the workspace in the background, so that the
// first call does not wait for all of it (minutes on a large module, the
// first time); the channel closes when it is done. Calls meanwhile wait
// for it, and a load that fails is tried again by the next call.
func (s *Server) Preload() <-chan struct{} {
	var started []<-chan struct{}
	if s.golang != nil {
		started = append(started, s.golang.Preload())
	}
	if s.typescript != nil {
		started = append(started, s.typescript.Preload())
	}
	done := make(chan struct{})
	go func() {
		for _, c := range started {
			<-c
		}
		close(done)
	}()
	return done
}

func (s *Server) refactor(tool string, args json.RawMessage) *mcp.CallToolResult {
	l, refused := s.langs.For(args)
	if refused != "" {
		return textResult(refused)
	}
	text, wasRefused, err := l.Step(tool, args)
	if err != nil {
		return toolError(err)
	}
	if !wasRefused {
		text += "\nRe-inspect the changed code before the next step; line numbers may have shifted.\n"
	}
	return textResult(text)
}

func (s *Server) inspect(args json.RawMessage) *mcp.CallToolResult {
	l, refused := s.langs.For(args)
	if refused != "" {
		return textResult(refused)
	}
	text, err := l.Inspect(args)
	if err != nil {
		return toolError(err)
	}
	return textResult(text)
}

func (s *Server) undo() *mcp.CallToolResult {
	text, err := s.langs.ForUndo().Undo()
	if err != nil {
		return toolError(err)
	}
	return textResult(text)
}

// batch runs the steps of one batch call; see the tool's description.
func (s *Server) batch(args json.RawMessage) *mcp.CallToolResult {
	l, refused := s.langs.For(args)
	if refused != "" {
		return textResult(refused)
	}
	text, err := l.Batch(args)
	if err != nil {
		return toolError(err)
	}
	return textResult(text)
}

func (s *Server) check(ctx context.Context, args json.RawMessage) *mcp.CallToolResult {
	l, refused := s.langs.For(args)
	if refused != "" {
		return textResult(refused)
	}
	text, err := l.Check(ctx, args)
	if err != nil {
		return toolError(err)
	}
	return textResult(text)
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
