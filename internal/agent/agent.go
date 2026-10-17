// Package agent runs a refactoring loop: a chat model (served by Ollama,
// or anything implementing Model) uses the refactoring tools on a Go module
// until it is done.
//
// The tools are exactly those the MCP server gives Claude Code: the agent
// starts mcpserver in-process and talks to it as an MCP client over an
// in-memory transport, so names, descriptions, schemas, instructions and
// behavior cannot drift apart.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/mcpserver"
	"github.com/tychofreeman/go-refactor-mcp/internal/metrics"
)

// Rules are added to the server's instructions in the system prompt. They
// are aimed at smaller models, which drift from tool-calling conventions.
const Rules = `Rules for this session:
- Call exactly one tool per turn, then read its result before deciding the next step.
- Use the exact argument names from each tool's schema. Line numbers must come from your latest inspect output.
- A REFUSED result changed nothing; read the reason and choose a different step or arguments.
- After an APPLIED result, inspect the changed code again before the next refactoring.
- When nothing clearly improves the code any more, stop calling tools and reply with a short summary of what you changed.`

// Config controls a run.
type Config struct {
	Dir      string   // a directory inside the module
	Patterns []string // package patterns (default ./...)
	Goal     string   // what the model should do, e.g. "Simplify calc/calc.go"
	Model    Model

	MaxSteps    int // tool calls allowed (default 30)
	MaxFailures int // consecutive failed tool calls before stopping (default 6)

	// Tests runs go test before and after. If they passed before and fail
	// after, every applied step is undone, unless Keep.
	Tests bool
	Keep  bool
	// Revert undoes every applied step at the end (for experiments).
	Revert bool

	Transcript io.Writer // JSON lines, one per event; may be nil
	Log        io.Writer // progress for a person; may be nil
}

// Report summarizes a run.
type Report struct {
	Steps      int            // tool calls made
	Applied    map[string]int // tool -> applied steps
	Refused    map[string]int // tool -> refused calls
	Errors     int            // tool errors, unknown tools, bad arguments
	Failures   []Failure      // every refused or failed call, in order
	StopReason string
	Summary    string // the model's final message, if it gave one

	TestsBefore, TestsAfter string // "passed", "failed" or "" (not run)
	RolledBack              bool   // every applied step was undone
	UndoFailed              string // why undoing failed (the files may still be changed), or ""
	Diff                    string // the combined change, against the starting files

	// The functions in the files the run changed (test files excluded),
	// at the start and at the end; file paths are relative to the module.
	FuncsBefore, FuncsAfter []metrics.Func
}

// Failure is a tool call that was refused or failed.
type Failure struct {
	Tool   string
	Status string // "refused" or "error"
	Reason string // the first line of the result
}

// Run runs the loop.
func Run(ctx context.Context, cfg Config) (*Report, error) {
	if cfg.Model == nil {
		return nil, fmt.Errorf("agent: no model")
	}
	if cfg.MaxSteps <= 0 {
		cfg.MaxSteps = 30
	}
	if cfg.MaxFailures <= 0 {
		cfg.MaxFailures = 6
	}
	root, err := moduleRoot(cfg.Dir)
	if err != nil {
		return nil, err
	}
	before, err := snapshot(root)
	if err != nil {
		return nil, err
	}
	r := &runner{cfg: cfg, rep: &Report{Applied: map[string]int{}, Refused: map[string]int{}}}

	// The session and the clean-up at the end (tests, undo) must outlive a
	// cancelled or timed-out run: only the model loop stops with ctx.
	cleanup := context.WithoutCancel(ctx)
	cs, closeAll, err := connect(cleanup, cfg.Dir, cfg.Patterns)
	if err != nil {
		return nil, err
	}
	defer closeAll()
	r.cs = cs
	if err := r.loadTools(ctx); err != nil {
		return nil, err
	}

	if cfg.Tests {
		r.rep.TestsBefore = r.runTests(ctx, "before")
	}
	r.loop(ctx)
	if cfg.Tests {
		tctx, cancel := context.WithTimeout(cleanup, 15*time.Minute)
		r.rep.TestsAfter = r.runTests(tctx, "after")
		cancel()
		if r.rep.TestsBefore == "passed" && r.rep.TestsAfter != "passed" && !cfg.Keep {
			r.logf("tests passed before and fail now; undoing every applied step")
			r.undoAll(cleanup, "tests failed after the run")
		}
	}
	if cfg.Revert && !r.rep.RolledBack && r.rep.UndoFailed == "" {
		r.undoAll(cleanup, "revert requested")
	}
	after, err := snapshot(root)
	if err != nil {
		return nil, err
	}
	changed := changedFiles(root, before, after)
	r.rep.Diff = diff(changed, before, after)
	if r.rep.FuncsBefore, err = metrics.Measure(changed.contents(before)); err != nil {
		return nil, err
	}
	if r.rep.FuncsAfter, err = metrics.Measure(changed.contents(after)); err != nil {
		return nil, err
	}
	r.event("end", map[string]any{"stop_reason": r.rep.StopReason, "steps": r.rep.Steps, "applied": r.rep.Applied,
		"refused": r.rep.Refused, "errors": r.rep.Errors, "tests_before": r.rep.TestsBefore, "tests_after": r.rep.TestsAfter,
		"rolled_back": r.rep.RolledBack, "undo_failed": r.rep.UndoFailed})
	return r.rep, nil
}

// connect starts the MCP server in-process and returns a client session.
func connect(ctx context.Context, dir string, patterns []string) (*mcp.ClientSession, func(), error) {
	srv := mcpserver.New(dir, patterns...).MCP()
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("agent: starting the tool server: %w", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "refactor-agent", Version: mcpserver.Version}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		ss.Close()
		return nil, nil, fmt.Errorf("agent: connecting to the tool server: %w", err)
	}
	return cs, func() { cs.Close(); ss.Close() }, nil
}

type runner struct {
	cfg     Config
	cs      *mcp.ClientSession
	tools   []ToolDef
	schemas map[string]json.RawMessage
	rep     *Report
}

func (r *runner) loadTools(ctx context.Context) error {
	r.schemas = map[string]json.RawMessage{}
	var cursor string
	for {
		res, err := r.cs.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			return fmt.Errorf("agent: listing tools: %w", err)
		}
		for _, t := range res.Tools {
			schema, err := json.Marshal(t.InputSchema)
			if err != nil {
				return err
			}
			r.tools = append(r.tools, ToolDef{Type: "function", Function: FunctionDef{Name: t.Name, Description: t.Description, Parameters: schema}})
			r.schemas[t.Name] = schema
		}
		if res.NextCursor == "" {
			return nil
		}
		cursor = res.NextCursor
	}
}

func (r *runner) systemPrompt() string {
	instr := ""
	if ir := r.cs.InitializeResult(); ir != nil {
		instr = ir.Instructions
	}
	return strings.TrimSpace(instr + "\n\n" + Rules)
}

func (r *runner) loop(ctx context.Context) {
	msgs := []Message{
		{Role: "system", Content: r.systemPrompt()},
		{Role: "user", Content: r.cfg.Goal},
	}
	r.event("start", map[string]any{"goal": r.cfg.Goal, "tools": len(r.tools)})
	failures := 0
	for {
		if ctx.Err() != nil {
			r.rep.StopReason = "cancelled"
			return
		}
		if r.rep.Steps >= r.cfg.MaxSteps {
			r.rep.StopReason = fmt.Sprintf("reached the limit of %d tool calls", r.cfg.MaxSteps)
			return
		}
		start := time.Now()
		reply, err := r.cfg.Model.Chat(ctx, msgs, r.tools)
		if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			r.rep.StopReason = "ran out of time (the -timeout limit) while waiting for the model"
			r.event("model_error", map[string]any{"error": err.Error(), "seconds": time.Since(start).Seconds()})
			return
		}
		if err != nil && ctx.Err() != nil {
			r.rep.StopReason = "cancelled"
			r.event("model_error", map[string]any{"error": err.Error(), "seconds": time.Since(start).Seconds()})
			return
		}
		if err != nil {
			r.rep.StopReason = "model error: " + err.Error()
			r.event("model_error", map[string]any{"error": err.Error()})
			return
		}
		reply.Role = "assistant"
		msgs = append(msgs, reply)
		secs := time.Since(start).Seconds()
		r.event("model", map[string]any{"content": reply.Content, "tool_calls": reply.ToolCalls, "seconds": secs})
		r.logf("   model replied in %.0fs", secs)
		if len(reply.ToolCalls) == 0 {
			r.rep.StopReason = "the model finished"
			r.rep.Summary = strings.TrimSpace(reply.Content)
			return
		}
		for i, call := range reply.ToolCalls {
			name := call.Function.Name
			if i > 0 {
				msgs = append(msgs, Message{Role: "tool", ToolName: name,
					Content: "NOT RUN: only the first tool call of a turn runs. Call one tool at a time, and re-inspect after any change."})
				continue
			}
			text, status := r.call(ctx, call)
			msgs = append(msgs, Message{Role: "tool", ToolName: name, Content: text})
			r.rep.Steps++
			switch status {
			case "applied":
				r.rep.Applied[name]++
				failures = 0
			case "refused":
				r.rep.Refused[name]++
				r.rep.Failures = append(r.rep.Failures, Failure{Tool: name, Status: status, Reason: strings.SplitN(text, "\n", 2)[0]})
				failures++
			case "error":
				r.rep.Errors++
				r.rep.Failures = append(r.rep.Failures, Failure{Tool: name, Status: status, Reason: strings.SplitN(text, "\n", 2)[0]})
				failures++
			default:
				failures = 0
			}
			r.logf("%2d %-17s %-8s %s", r.rep.Steps, name, status, firstLine(text))
		}
		if failures >= r.cfg.MaxFailures {
			r.rep.StopReason = fmt.Sprintf("%d tool calls in a row failed or were refused", failures)
			return
		}
	}
}

// call runs one tool call and classifies the result: applied, refused,
// error, or ok (a read-only or undo call that succeeded).
func (r *runner) call(ctx context.Context, call ToolCall) (string, string) {
	name := call.Function.Name
	schema, known := r.schemas[name]
	if !known {
		var names []string
		for n := range r.schemas {
			names = append(names, n)
		}
		sort.Strings(names)
		text := fmt.Sprintf("ERROR: there is no tool named %q. The tools are: %s.", name, strings.Join(names, ", "))
		r.event("tool", map[string]any{"name": name, "arguments": call.Function.Arguments, "status": "error", "result": text})
		return text, "error"
	}
	args, err := coerceArgs(call.Function.Arguments, schema)
	if err != nil {
		text := "ERROR: " + err.Error()
		r.event("tool", map[string]any{"name": name, "arguments": call.Function.Arguments, "status": "error", "result": text})
		return text, "error"
	}
	start := time.Now()
	res, err := r.cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	var text, status string
	switch {
	case err != nil:
		text, status = "ERROR: "+err.Error(), "error"
	default:
		text = contentText(res)
		status = classify(text, res.IsError)
	}
	r.event("tool", map[string]any{"name": name, "arguments": args, "status": status, "result": text, "seconds": time.Since(start).Seconds()})
	return text, status
}

// classify reads a tool result's status from its first word, which the
// tools write for exactly this purpose.
func classify(text string, isError bool) string {
	switch {
	case isError:
		return "error"
	case strings.HasPrefix(text, "APPLIED"):
		return "applied"
	case strings.HasPrefix(text, "REFUSED"):
		return "refused"
	case strings.HasPrefix(text, "invalid arguments"):
		return "refused"
	}
	return "ok"
}

func contentText(res *mcp.CallToolResult) string {
	var sb strings.Builder
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(t.Text)
		}
	}
	return sb.String()
}

// runTests runs go test through the check tool: "passed" or "failed".
func (r *runner) runTests(ctx context.Context, when string) string {
	res, err := r.cs.CallTool(ctx, &mcp.CallToolParams{Name: "check", Arguments: map[string]any{"run_tests": true}})
	result := "failed"
	text := ""
	if err == nil {
		text = contentText(res)
		if strings.Contains(text, "TYPE-CHECK OK") && strings.Contains(text, "TESTS PASSED") {
			result = "passed"
		}
	} else {
		text = err.Error()
	}
	r.event("tests", map[string]any{"when": when, "result": result, "output": text})
	r.logf("tests %s: %s", when, result)
	return result
}

// undoAll undoes applied steps until there is nothing left to undo, and
// records whether that worked.
func (r *runner) undoAll(ctx context.Context, why string) {
	for i := 0; i < 1000; i++ {
		cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		res, err := r.cs.CallTool(cctx, &mcp.CallToolParams{Name: "undo", Arguments: map[string]any{}})
		cancel()
		if err != nil {
			r.event("undo", map[string]any{"why": why, "error": err.Error()})
			r.rep.UndoFailed = err.Error()
			return
		}
		text := contentText(res)
		r.event("undo", map[string]any{"why": why, "result": firstLine(text)})
		if !strings.HasPrefix(text, "UNDONE") {
			if strings.HasPrefix(text, "Nothing to undo") {
				r.rep.RolledBack = true
			} else {
				r.rep.UndoFailed = firstLine(text)
			}
			return
		}
	}
	r.rep.UndoFailed = "gave up after 1000 undo steps"
}

func (r *runner) event(kind string, fields map[string]any) {
	if r.cfg.Transcript == nil {
		return
	}
	fields["kind"] = kind
	fields["time"] = time.Now().UTC().Format(time.RFC3339Nano)
	b, err := json.Marshal(fields)
	if err != nil {
		return
	}
	r.cfg.Transcript.Write(append(b, '\n'))
}

func (r *runner) logf(format string, args ...any) {
	if r.cfg.Log != nil {
		fmt.Fprintf(r.cfg.Log, format+"\n", args...)
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 110 {
		s = s[:107] + "..."
	}
	return s
}

// moduleRoot returns the directory of the go.mod enclosing dir.
func moduleRoot(dir string) (string, error) {
	d, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", fmt.Errorf("agent: no go.mod in %s or above", dir)
		}
		d = parent
	}
}

// snapshot records the Go files under root, to diff against at the end.
func snapshot(root string) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); p != root && (strings.HasPrefix(n, ".") || n == "vendor" || n == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ".go") {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			files[p] = b
		}
		return nil
	})
	return files, err
}

// changed maps the module-relative path of each changed Go file to its
// absolute path, which keys the snapshots.
type changed map[string]string

// changedFiles returns the Go files under root that differ between the
// snapshots, including files added or removed.
func changedFiles(root string, before, after map[string][]byte) changed {
	c := changed{}
	for _, files := range []map[string][]byte{before, after} {
		for p := range files {
			if string(before[p]) != string(after[p]) {
				rel, _ := filepath.Rel(root, p)
				c[filepath.ToSlash(rel)] = p
			}
		}
	}
	return c
}

// contents returns the changed files' contents in snap, by relative path;
// a file missing from snap is left out.
func (c changed) contents(snap map[string][]byte) map[string][]byte {
	out := map[string][]byte{}
	for rel, p := range c {
		if b, ok := snap[p]; ok {
			out[rel] = b
		}
	}
	return out
}

// diff returns the unified diff of the changed files.
func diff(c changed, before, after map[string][]byte) string {
	var rels []string
	for rel := range c {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	var sb strings.Builder
	for _, rel := range rels {
		sb.WriteString(edit.Unified(rel, before[c[rel]], after[c[rel]]))
	}
	return sb.String()
}
