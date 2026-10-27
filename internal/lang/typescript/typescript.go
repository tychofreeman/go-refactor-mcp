// Package typescript is the TypeScript language of the server: its
// refactorings on the solution's workspace. It has rename so far
// (docs/ts-plan.md, step 3); the other tools refuse its files, saying so.
package typescript

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/tychofreeman/go-refactor-mcp/internal/history"
	"github.com/tychofreeman/go-refactor-mcp/internal/lang"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/inspect"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/refactorings/rename"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

// Language is TypeScript, for the solution of the nearest tsconfig.json
// at or above a directory.
type Language struct {
	// FullDiffs shows results with three lines of context around each
	// change; by default they have none.
	FullDiffs bool
	// ReadByName makes inspect show source only for what a request names.
	ReadByName bool

	dir     string
	history *history.Stack

	mu sync.Mutex
	ws *workspace.Workspace // loaded lazily, refreshed before each call
}

// New returns TypeScript for the solution at or above dir, recording its
// changes on h.
func New(dir string, h *history.Stack) *Language {
	return &Language{dir: dir, history: h}
}

// all is TypeScript's refactorings and edits.
func all() []refactor.RefactoringOf[*workspace.Workspace] {
	return []refactor.RefactoringOf[*workspace.Workspace]{rename.New()}
}

func lookup(name string) (refactor.RefactoringOf[*workspace.Workspace], bool) {
	for _, r := range all() {
		if r.Name() == name {
			return r, true
		}
	}
	return nil, false
}

// Name is "TypeScript".
func (*Language) Name() string { return "TypeScript" }

// Owns reports whether path is a TypeScript source file.
func (*Language) Owns(path string) bool {
	for _, ext := range []string{".ts", ".tsx", ".mts", ".cts"} {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}

// Tools are TypeScript's refactorings and edits.
func (*Language) Tools() []lang.Tool {
	var out []lang.Tool
	for _, r := range all() {
		out = append(out, lang.Tool{Name: r.Name(), Description: r.Description(), Schema: r.InputSchema()})
	}
	return out
}

// Loaded reports whether the workspace has been loaded.
func (l *Language) Loaded() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ws != nil
}

// workspace returns the workspace, loading it on first use and
// refreshing it if files changed on disk. Callers hold l.mu.
func (l *Language) workspace() (*workspace.Workspace, error) {
	if l.ws == nil {
		ws, err := workspace.Load(l.dir)
		if err != nil {
			return nil, err
		}
		ws.History = l.history
		l.ws = ws
		return ws, nil
	}
	if _, err := l.ws.Refresh(); err != nil {
		return nil, err
	}
	return l.ws, nil
}

// Preload starts loading the workspace in the background; the channel
// closes when it is done.
func (l *Language) Preload() <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.ws == nil {
			l.workspace()
		}
	}()
	return done
}

func (l *Language) names() string {
	var names []string
	for _, r := range all() {
		names = append(names, r.Name())
	}
	return strings.Join(names, ", ")
}

// Step runs the refactoring or edit named tool.
func (l *Language) Step(tool string, args json.RawMessage) (string, bool, error) {
	r, ok := lookup(tool)
	if !ok {
		return fmt.Sprintf("REFUSED %s: it does not work on TypeScript files yet; for them there is %s.", tool, l.names()), true, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	ws, err := l.workspace()
	if err != nil {
		return "", false, err
	}
	res, err := refactor.Run(ws, r, args, refactor.OptionsOf[*workspace.Workspace]{Compact: !l.FullDiffs})
	if err != nil {
		return "", false, err
	}
	return res.String(), res.Refused != "", nil
}

// Batch runs the steps of one batch call.
func (l *Language) Batch(args json.RawMessage) (string, error) {
	batch, refused := refactor.ParseBatch(args, lookup)
	if refused != "" {
		return refused, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	ws, err := l.workspace()
	if err != nil {
		return "", err
	}
	b, err := refactor.RunBatch(ws, batch.Steps, refactor.OptionsOf[*workspace.Workspace]{Compact: !l.FullDiffs, Unchecked: batch.Unchecked})
	if err != nil {
		return "", err
	}
	if batch.Summary {
		b.Diff = ""
	}
	return b.String(), nil
}

// Inspect reads TypeScript code; a refusal is its text.
func (l *Language) Inspect(args json.RawMessage) (string, error) {
	var q inspect.Query
	if err := refactor.DecodeArgs(args, &q); err != nil {
		return err.Error(), nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	ws, err := l.workspace()
	if err != nil {
		return "", err
	}
	out, err := inspect.RunWith(ws, q, inspect.Options{ByName: l.ReadByName})
	if err != nil {
		if refactor.IsRefusal(err) {
			return err.Error(), nil
		}
		return "", err
	}
	return out, nil
}

// Check reports whether the solution type-checks; it runs no tests yet.
func (l *Language) Check(_ context.Context, args json.RawMessage) (string, error) {
	var a struct {
		RunTests bool `json:"run_tests"`
	}
	if err := refactor.DecodeArgs(args, &a); err != nil {
		return err.Error(), nil
	}
	l.mu.Lock()
	ws, err := l.workspace()
	var errs []string
	if err == nil {
		errs = ws.Errors()
	}
	l.mu.Unlock()
	if err != nil {
		return "", err
	}
	report, _, err := refactor.CheckReport(errs, l.dir, json.RawMessage(`{}`))
	if err != nil {
		return err.Error(), nil
	}
	if a.RunTests {
		report += "\nThe tests were not run: check does not run TypeScript tests yet."
	}
	return report, nil
}

// Undo reverts the change on top of the stack.
func (l *Language) Undo() (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	ws, err := l.workspace()
	if err != nil {
		return "", err
	}
	if !ws.CanUndo() {
		return "Nothing to undo: no refactoring has been applied in this session.", nil
	}
	c, err := ws.Undo()
	if c.Label == "" { // not undone
		return "Cannot undo: " + err.Error(), nil
	}
	text := fmt.Sprintf("UNDONE %s\n\n%s", strings.TrimPrefix(c.Label, "undo "), c.Diff)
	if err != nil {
		text += "\nThe solution could not be loaded again: " + err.Error()
	}
	return text, nil
}
