// Package golang is the Go language of the server: its refactorings and
// edits, inspect, check and undo, on the module's workspace.
package golang

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/tychofreeman/go-refactor-mcp/internal/history"
	"github.com/tychofreeman/go-refactor-mcp/internal/inspect"
	"github.com/tychofreeman/go-refactor-mcp/internal/lang"
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

	mu     sync.Mutex
	ws     *workspace.Workspace            // loaded lazily, refreshed before each call
	others map[string]*workspace.Workspace // other modules a call named a file of, by root
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

// Tools are Go's refactorings and edits; a description says how a tool
// whose line is optional finds a declaration by name.
func (*Language) Tools() []lang.Tool {
	var out []lang.Tool
	for _, r := range refactorings.All() {
		out = append(out, lang.Tool{Name: r.Name(), Description: r.Description() + symbolNote(r), Schema: r.InputSchema()})
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

// homeRoot is the root of the directory's own module: the directory
// itself when no go.mod is at or above it.
func (l *Language) homeRoot() string {
	dir, err := filepath.Abs(l.dir)
	if err != nil {
		dir = l.dir
	}
	return workspace.ModuleRoot(dir)
}

// undoModule returns the workspace of the module, other than the
// directory's own, whose files the change on top of the stack changed,
// and the line that says which; or nil. Callers hold l.mu.
func (l *Language) undoModule() (*workspace.Workspace, string) {
	c, ok := l.history.Last()
	if !ok {
		return nil, ""
	}
	home := l.homeRoot()
	for p := range c.After {
		if r := workspace.ModuleRoot(existingDir(filepath.Dir(p))); r != home && l.others[r] != nil {
			return l.others[r], fmt.Sprintf("In the module at %s:\n", relTo(home, r))
		}
		break
	}
	return nil, ""
}

// route returns the workspace of the module whose files args names, and
// args with their paths relative to its root. For a module other than
// the directory's own it also returns the line that says which; for
// files of several modules, a refusal instead. Callers hold l.mu.
func (l *Language) route(args json.RawMessage) (ws *workspace.Workspace, out json.RawMessage, intro, refused string, err error) {
	home := l.homeRoot()
	dir, err := filepath.Abs(l.dir)
	if err != nil {
		return nil, nil, "", "", err
	}
	var v any
	if json.Unmarshal(args, &v) != nil {
		ws, err = l.workspace()
		return ws, args, "", "", err
	}
	abs := func(p string) string {
		if filepath.IsAbs(p) {
			return filepath.Clean(p)
		}
		if a := filepath.Join(home, p); exists(a) || !exists(filepath.Join(dir, p)) {
			return a
		}
		return filepath.Join(dir, p)
	}
	byRoot := map[string][]string{}
	var roots []string
	for _, f := range lang.NamedFiles(v) {
		r := workspace.ModuleRoot(existingDir(filepath.Dir(abs(f))))
		if byRoot[r] == nil {
			roots = append(roots, r)
		}
		byRoot[r] = append(byRoot[r], f)
	}
	switch {
	case len(roots) > 1:
		var parts []string
		for _, r := range roots {
			parts = append(parts, fmt.Sprintf("%s (%s)", relTo(home, r), strings.Join(byRoot[r], ", ")))
		}
		return nil, nil, "", fmt.Sprintf("this call names files of more than one module: %s. A call or batch works in one module; send the steps for each as separate calls.", strings.Join(parts, "; ")), nil
	case len(roots) == 0 || roots[0] == home:
		ws, err = l.workspace()
		return ws, args, "", "", err
	}
	root := roots[0]
	if ws, err = l.module(root); err != nil {
		return nil, nil, "", "", err
	}
	out, err = json.Marshal(lang.MapFiles(v, func(p string) string { return relTo(root, abs(p)) }))
	return ws, out, fmt.Sprintf("In the module at %s (paths below are relative to it):\n", relTo(home, root)), "", err
}

// module returns the workspace of the module at root, other than the
// directory's own, loading it on first use and refreshing it after.
// Callers hold l.mu.
func (l *Language) module(root string) (*workspace.Workspace, error) {
	if ws := l.others[root]; ws != nil {
		_, err := ws.Refresh()
		return ws, err
	}
	ws, err := workspace.Load(root, "./...")
	if err != nil {
		return nil, err
	}
	ws.History = l.history
	if l.others == nil {
		l.others = map[string]*workspace.Workspace{}
	}
	l.others[root] = ws
	return ws, nil
}

// exists reports whether there is a file or directory at path.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// existingDir is dir, or its nearest ancestor that exists.
func existingDir(dir string) string {
	for !exists(dir) && filepath.Dir(dir) != dir {
		dir = filepath.Dir(dir)
	}
	return dir
}

// relTo is path relative to base, with slashes.
func relTo(base, path string) string {
	if r, err := filepath.Rel(base, path); err == nil {
		return filepath.ToSlash(r)
	}
	return path
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
	ws, args, intro, refused, err := l.route(args)
	if err != nil || refused != "" {
		return refused, refused != "", err
	}
	text, wasRefused, err := refactorings.RunStep(ws, r, args, refactor.Options{Compact: !l.FullDiffs})
	return intro + text, wasRefused, err
}

// Inspect reads Go code; a refusal is its text.
func (l *Language) Inspect(args json.RawMessage) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	ws, args, intro, refused, err := l.route(args)
	if err != nil || refused != "" {
		return refused, err
	}
	var q inspect.Query
	if err := refactor.DecodeArgs(args, &q); err != nil {
		return err.Error(), nil
	}
	out, err := inspect.RunWith(ws, q, inspect.Options{ByName: l.ReadByName})
	if err != nil {
		if refactor.IsRefusal(err) {
			return intro + err.Error(), nil
		}
		return "", err
	}
	return intro + out, nil
}

// Undo reverts the change on top of the stack.
func (l *Language) Undo() (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Undoing only needs the files, so it goes ahead even when the module
	// does not load (a go.mod broken by hand, say), and says so.
	var loadErr error
	ws, intro := l.undoModule()
	switch {
	case ws != nil:
		if _, err := ws.Refresh(); err != nil {
			loadErr = err
		}
	case l.ws == nil:
		var err error
		if ws, err = l.workspace(); err != nil {
			return "", err
		}
	default:
		ws = l.ws
		if _, err := ws.Refresh(); err != nil {
			loadErr = err
		}
	}
	if !ws.CanUndo() {
		return "Nothing to undo: no refactoring has been applied in this session." + notLoading(loadErr), nil
	}
	c, err := ws.Undo()
	if c.Label == "" { // not undone
		return intro + "Cannot undo: " + err.Error() + notLoading(loadErr), nil
	}
	if err == nil {
		err = loadErr
	}
	return intro + fmt.Sprintf("UNDONE %s\n\n%s", strings.TrimPrefix(c.Label, "undo "), c.Diff) + notLoading(err), nil
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
	l.mu.Lock()
	defer l.mu.Unlock()
	ws, args, intro, refused, err := l.route(args)
	if err != nil || refused != "" {
		return refused, err
	}
	batch, refused := refactorings.ParseBatch(args)
	if refused != "" {
		return intro + refused, nil
	}
	b, err := refactor.RunBatch(ws, batch.Steps, refactor.Options{Compact: !l.FullDiffs, Unchecked: batch.Unchecked, Declare: refactorings.Declarer(), Imports: refactorings.Importer()})
	if err != nil {
		return "", err
	}
	if batch.Summary {
		b.Diff = ""
	}
	return intro + b.String(), nil
}

// Check reports whether the module type-checks, and runs go test if
// asked.
func (l *Language) Check(ctx context.Context, args json.RawMessage) (string, error) {
	l.mu.Lock()
	ws, args, intro, refused, err := l.route(args)
	var errs []string
	var root string
	if err == nil && refused == "" {
		errs, root = ws.Errors(), ws.Root
	}
	l.mu.Unlock()
	if err != nil || refused != "" {
		return refused, err
	}
	report, _, err := refactor.CheckReport(errs, root, args)
	if err != nil {
		return intro + err.Error(), nil
	}
	return intro + report, nil
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
