// Package mcpserver exposes the refactoring engine as an MCP server.
//
// Tools:
//   - one tool per registered refactoring (rename, ...), which applies a
//     single step and returns the diff or the reason it was refused;
//   - inspect: module overview, file outline with complexity, numbered source;
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
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cwfreeman/gorefactor/internal/inspect"
	"github.com/cwfreeman/gorefactor/internal/refactor"
	"github.com/cwfreeman/gorefactor/internal/refactorings"
	"github.com/cwfreeman/gorefactor/internal/workspace"
)

// Version is reported to MCP clients.
const Version = "0.2.0"

// Instructions are sent to the client at initialization; MCP clients such
// as Claude Code show them to the model. They set the pace of the loop.
const Instructions = `Behavior-preserving refactoring tools for the Go module in this project.

How to simplify code with these tools:
1. Call inspect (no arguments) for an overview, then inspect a file or function to read it with line numbers. The outline shows each function's complexity and nesting; high values are good candidates.
2. Apply exactly ONE refactoring per call. Each call returns APPLIED with a diff, or REFUSED with the reason; a refusal changes nothing, so read it and pick a different name or refactoring.
3. After each applied step, re-inspect the affected code before choosing the next step. Line numbers change after edits; never reuse old ones.
4. Stop when no remaining refactoring clearly improves readability. Use check (with run_tests) at the end, and undo if a step made the code worse.

Prefer these tools over editing code by hand for the transformations they cover: they find every reference with the type checker and refuse changes that would alter behavior. Every applied step is verified to compile and can be undone.`

// Server is the refactoring MCP server for one module.
type Server struct {
	dir      string
	patterns []string

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
			Description: r.Description() + "\nApplies the change to disk and returns a diff; it can be reverted with undo.",
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
- file: outline of the file (each declaration's line range; for functions also length, cyclomatic complexity and nesting depth) followed by its source.
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
		Description: "Revert the most recent refactoring applied in this session and return the reverting diff. Can be called repeatedly to step further back. Refuses if a changed file was edited since.",
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
	return srv
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
	res, err := refactor.Run(ws, r, args, refactor.Options{})
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
