// Package golang is the Go language of the server: its refactorings and
// edits, inspect, check and undo, on the module's workspace.
package golang

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/tychofreeman/go-refactor-mcp/internal/history"
	"github.com/tychofreeman/go-refactor-mcp/internal/inspect"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// Language is Go, for the module containing a directory.
type Language struct {
	// FullDiffs shows results with three lines of context around each
	// change; by default they have none.
	FullDiffs bool
	// ReadByName makes inspect show source only for what a request names.
	ReadByName bool

	dir      string
	patterns []string
	history  *history.Stack

	mu sync.Mutex
	ws *workspace.Workspace // loaded lazily, refreshed before each call
}

// New returns Go for the module containing dir, recording its changes on
// h.
func New(dir string, patterns []string, h *history.Stack) *Language {
	return &Language{dir: dir, patterns: patterns, history: h}
}

// Name is "Go".
func (*Language) Name() string { return "Go" }

// Owns reports whether path is a Go source file.
func (*Language) Owns(path string) bool { return strings.HasSuffix(path, ".go") }

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
		ws, err := workspace.Load(l.dir, l.patterns...)
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

// Preload starts loading the workspace in the background, so that the
// first call does not wait for all of it (minutes on a large module, the
// first time); the channel closes when it is done. Calls meanwhile wait
// for it, and a load that fails is tried again by the next call.
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

// Step runs the refactoring or edit named tool.
func (l *Language) Step(tool string, args json.RawMessage) (string, bool, error) {
	r, ok := refactorings.Lookup(tool)
	if !ok {
		return "", false, fmt.Errorf("unknown tool %q", tool)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	ws, err := l.workspace()
	if err != nil {
		return "", false, err
	}
	return refactorings.RunStep(ws, r, args, refactor.Options{Compact: !l.FullDiffs})
}

// Inspect reads Go code; a refusal is its text.
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

// Undo reverts the change on top of the stack.
func (l *Language) Undo() (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Undoing only needs the files, so it goes ahead even when the module
	// does not load (a go.mod broken by hand, say), and says so.
	var loadErr error
	ws := l.ws
	if ws == nil {
		var err error
		if ws, err = l.workspace(); err != nil {
			return "", err
		}
	} else if _, err := ws.Refresh(); err != nil {
		loadErr = err
	}
	if !ws.CanUndo() {
		return "Nothing to undo: no refactoring has been applied in this session." + notLoading(loadErr), nil
	}
	c, err := ws.Undo()
	if c.Label == "" { // not undone
		return "Cannot undo: " + err.Error() + notLoading(loadErr), nil
	}
	if err == nil {
		err = loadErr
	}
	return fmt.Sprintf("UNDONE %s\n\n%s", strings.TrimPrefix(c.Label, "undo "), c.Diff) + notLoading(err), nil
}

// notLoading is the line undo adds when the module does not load, or "".
func notLoading(err error) string {
	if err == nil {
		return ""
	}
	return "\nThe module does not load, so the other tools cannot run until it does: " + err.Error()
}

// Batch runs the steps of one batch call.
func (l *Language) Batch(args json.RawMessage) (string, error) {
	batch, refused := refactorings.ParseBatch(args)
	if refused != "" {
		return refused, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	ws, err := l.workspace()
	if err != nil {
		return "", err
	}
	b, err := refactor.RunBatch(ws, batch.Steps, refactor.Options{Compact: !l.FullDiffs, Unchecked: batch.Unchecked, Declare: refactorings.Declarer(), Imports: refactorings.Importer()})
	if err != nil {
		return "", err
	}
	if batch.Summary {
		b.Diff = ""
	}
	return b.String(), nil
}

// Check reports whether the module type-checks, and runs go test if
// asked.
func (l *Language) Check(ctx context.Context, args json.RawMessage) (string, error) {
	l.mu.Lock()
	ws, err := l.workspace()
	var errs []string
	var root string
	if err == nil {
		errs, root = ws.Errors(), ws.Root
	}
	l.mu.Unlock()
	if err != nil {
		return "", err
	}
	report, _, err := refactor.CheckReport(errs, root, args)
	if err != nil {
		return err.Error(), nil
	}
	return report, nil
}
