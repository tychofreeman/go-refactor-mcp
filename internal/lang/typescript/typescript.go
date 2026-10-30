// Package typescript is the TypeScript language of the server: its
// refactorings and edits on the solution's workspace, under the Go tools'
// names where the operation is the same (docs/ts-plan.md); a tool it does
// not have refuses its files, saying so.
package typescript

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tychofreeman/go-refactor-mcp/internal/history"
	"github.com/tychofreeman/go-refactor-mcp/internal/lang"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/edits/addcase"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/edits/adddecl"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/edits/addelement"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/edits/addfield"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/edits/changetype"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/edits/createfile"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/edits/createfromusage"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/edits/fiximports"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/edits/insertstmts"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/edits/replacedecl"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/edits/replaceexpr"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/edits/replacestmts"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/edits/replacetext"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/inspect"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/refactorings/changesig"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/refactorings/move"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/refactorings/rename"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/refactorings/shapes"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/refactorings/simplify"
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

	mu     sync.Mutex
	ws     *workspace.Workspace            // loaded lazily, refreshed before each call
	others map[string]*workspace.Workspace // other solutions a call named a file of, by root
}

// New returns TypeScript for the solution at or above dir, recording its
// changes on h.
func New(dir string, h *history.Stack) *Language {
	return &Language{dir: dir, history: h}
}

// all is TypeScript's refactorings and edits.
func all() []refactor.RefactoringOf[*workspace.Workspace] {
	return []refactor.RefactoringOf[*workspace.Workspace]{rename.New(), createfile.New(), adddecl.New(), replacetext.New(), replacestmts.New(), insertstmts.New(), replaceexpr.New(), addfield.New(), createfromusage.New(), changesig.NewAdd(), changesig.NewRemove(), changesig.NewMove(), changesig.NewIntroduce(), changesig.NewParamsToObject(), changesig.NewAddResult(), changesig.NewChangeFieldType(), changesig.NewChangeParameterType(), changetype.New(), changesig.NewFunctionToMethod(), changesig.NewMethodToFunction(), changesig.NewMoveMethod(),
		simplify.NewExtractVariable(), simplify.NewInlineVariable(), simplify.NewMoveUp(), simplify.NewMoveDown(), simplify.NewInvertIf(), simplify.NewRemoveElse(), simplify.NewExtractFunction(), simplify.NewInlineFunction(),
		move.NewMoveDeclaration(), move.NewMoveFile(), move.NewDeleteFile(),
		shapes.NewFold(), shapes.NewParameterize(), shapes.NewMergeDuplicate(), shapes.NewRemoveDeadCode(), shapes.NewExtractInterface(), shapes.NewUnionSwitchToMethod(), shapes.NewPrepareSwitchForTypes(), addcase.New(), addelement.New(), replacedecl.New()}
}

// goNames are Go tools' names for the TypeScript tool that does their
// operation, with the same arguments: a call of the Go name on a
// TypeScript file runs it. extract_struct embeds a struct in Go; an
// interface that extends one is TypeScript's counterpart.
var goNames = map[string]string{"extract_struct": "extract_interface", "type_switch_to_method": "union_switch_to_method"}

func lookup(name string) (refactor.RefactoringOf[*workspace.Workspace], bool) {
	name = refactor.ToolName(name)
	if ts, ok := goNames[name]; ok {
		name = ts
	}
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

// route returns the workspace for the files args names, and args with
// their paths relative to its root: the solution's own, unless a file is
// in none of its projects and the nearest tsconfig.json at or above it is
// not one of them either; then that tsconfig's solution, with the line
// that says which. Files of two solutions are refused. Callers hold l.mu.
func (l *Language) route(args json.RawMessage) (ws *workspace.Workspace, out json.RawMessage, intro, refused string, err error) {
	home, err := l.workspace()
	if err != nil {
		return nil, nil, "", "", err
	}
	var v any
	if json.Unmarshal(args, &v) != nil {
		return home, args, "", "", nil
	}
	homeRoot := home.Dir()
	abs := func(p string) string {
		if filepath.IsAbs(p) {
			return filepath.Clean(p)
		}
		return filepath.Join(homeRoot, p)
	}
	configs := map[string]bool{}
	for _, c := range home.Projects() {
		configs[filepath.Join(homeRoot, c)] = true
	}
	byRoot := map[string][]string{}
	var roots []string
	for _, f := range lang.NamedFiles(v) {
		a, r := abs(f), homeRoot
		if !home.Has(a) {
			if c := nearestConfig(lang.ExistingDir(filepath.Dir(a))); c != "" && !configs[c] {
				r = filepath.Dir(c)
			}
		}
		if byRoot[r] == nil {
			roots = append(roots, r)
		}
		byRoot[r] = append(byRoot[r], f)
	}
	switch {
	case len(roots) > 1:
		var parts []string
		for _, r := range roots {
			parts = append(parts, fmt.Sprintf("%s (%s)", lang.RelTo(homeRoot, r), strings.Join(byRoot[r], ", ")))
		}
		return nil, nil, "", fmt.Sprintf("this call names files of more than one solution: %s. A call or batch works in one solution; send the steps for each as separate calls.", strings.Join(parts, "; ")), nil
	case len(roots) == 0 || roots[0] == homeRoot:
		return home, args, "", "", nil
	}
	root := roots[0]
	if ws, err = l.solution(root); err != nil {
		return nil, nil, "", "", err
	}
	out, err = json.Marshal(lang.MapFiles(v, func(p string) string { return lang.RelTo(root, abs(p)) }))
	return ws, out, fmt.Sprintf("In the solution at %s (paths below are relative to it):\n", lang.RelTo(homeRoot, root)), "", err
}

// solution returns the workspace of the solution at root, other than the
// directory's own, loading it on first use and refreshing it after.
// Callers hold l.mu.
func (l *Language) solution(root string) (*workspace.Workspace, error) {
	if ws := l.others[root]; ws != nil {
		_, err := ws.Refresh()
		return ws, err
	}
	ws, err := workspace.Load(root)
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

// undoSolution returns the workspace of the solution, other than the
// directory's own, whose files the change on top of the stack changed,
// and the line that says which; or nil. Callers hold l.mu.
func (l *Language) undoSolution() (*workspace.Workspace, string) {
	c, ok := l.history.Last()
	if !ok || l.ws == nil {
		return nil, ""
	}
	for p := range c.After {
		if l.ws.Has(p) {
			return nil, ""
		}
		best := ""
		for root := range l.others {
			if strings.HasPrefix(p, root+string(filepath.Separator)) && len(root) > len(best) {
				best = root
			}
		}
		if best != "" {
			return l.others[best], fmt.Sprintf("In the solution at %s:\n", lang.RelTo(l.ws.Dir(), best))
		}
		break
	}
	return nil, ""
}

// nearestConfig returns the tsconfig.json at or above dir, or "".
func nearestConfig(dir string) string {
	for {
		if c := filepath.Join(dir, "tsconfig.json"); lang.Exists(c) {
			return c
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
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
	ws, args, intro, refused, err := l.route(args)
	if err != nil || refused != "" {
		return refused, refused != "", err
	}
	text, wasRefused, err := l.step(ws, r, args)
	return intro + text, wasRefused, err
}

// step runs r in ws. Callers hold l.mu.
func (l *Language) step(ws *workspace.Workspace, r refactor.RefactoringOf[*workspace.Workspace], args json.RawMessage) (string, bool, error) {
	res, err := refactor.Run(ws, r, args, refactor.OptionsOf[*workspace.Workspace]{Compact: !l.FullDiffs})
	if err != nil {
		return "", false, err
	}
	if _, edit := r.(refactor.Edit); edit && refactor.RefusedForErrors(res.Refused) {
		// An edit refused for its errors runs again as a batch, which adds
		// the imports its code needs.
		b, err := refactor.RunBatch(ws, []refactor.StepOf[*workspace.Workspace]{{Refactoring: r, Args: args}}, l.options(true))
		if err != nil {
			return "", false, err
		}
		if b.Applied {
			return b.String(), false, nil
		}
	}
	return res.String(), res.Refused != "", nil
}

// options are a batch's options, checked at its end when unchecked: the
// engine adds the imports the steps' code needs, and stubs for what it
// calls but nothing declares.
func (l *Language) options(unchecked bool) refactor.OptionsOf[*workspace.Workspace] {
	return refactor.OptionsOf[*workspace.Workspace]{Compact: !l.FullDiffs, Unchecked: unchecked, Imports: fiximports.New(), Declare: createfromusage.New(), Test: l.test}
}

// test runs the project's tests: package.json's test script when it has
// one, else node --test; a pattern other than ./... names the test files.
func (l *Language) test(pattern string) (string, error) {
	root := l.root()
	argv := testCommand(root, pattern)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// root is the directory of the solution's tsconfig.json, or the server's
// directory before the solution loads.
func (l *Language) root() string {
	if l.ws != nil {
		return l.ws.Dir()
	}
	return l.dir
}

// testCommand is the command test runs in dir.
func testCommand(dir, pattern string) []string {
	var files []string
	if pattern != "" && pattern != "./..." {
		files = strings.Fields(pattern)
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if b, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil && json.Unmarshal(b, &pkg) == nil && pkg.Scripts["test"] != "" {
		argv := []string{"npm", "test", "--silent"}
		if len(files) > 0 {
			argv = append(append(argv, "--"), files...)
		}
		return argv
	}
	return append([]string{"node", "--test"}, files...)
}

// Batch runs the steps of one batch call.
func (l *Language) Batch(args json.RawMessage) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	ws, args, intro, refused, err := l.route(args)
	if err != nil || refused != "" {
		return refused, err
	}
	batch, refused := refactor.ParseBatch(args, lookup)
	if refused != "" {
		return intro + refused, nil
	}
	b, err := refactor.RunBatch(ws, batch.Steps, l.options(batch.Unchecked))
	if err != nil {
		return "", err
	}
	if batch.Summary {
		b.Diff = ""
	}
	return intro + b.String(), nil
}

// Inspect reads TypeScript code; a refusal is its text.
func (l *Language) Inspect(args json.RawMessage) (string, error) {
	var q inspect.Query
	if err := refactor.DecodeArgs(args, &q); err != nil {
		return err.Error(), nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	ws, args, intro, refused, err := l.route(args)
	if err != nil || refused != "" {
		return refused, err
	}
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

// Check reports whether the solution type-checks, and runs its tests if
// asked (Language.test).
func (l *Language) Check(_ context.Context, args json.RawMessage) (string, error) {
	var a struct {
		RunTests bool   `json:"run_tests"`
		Packages string `json:"packages"`
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
		out, testErr := l.test(a.Packages)
		report += refactor.TestsReport(out, testErr)
	}
	return report, nil
}

// Undo reverts the change on top of the stack.
func (l *Language) Undo() (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, err := l.workspace(); err != nil {
		return "", err
	}
	ws, intro := l.undoSolution()
	if ws == nil {
		ws = l.ws
	} else if _, err := ws.Refresh(); err != nil {
		return "", err
	}
	if !ws.CanUndo() {
		return "Nothing to undo: no refactoring has been applied in this session.", nil
	}
	c, err := ws.Undo()
	if c.Label == "" { // not undone
		return "Cannot undo: " + err.Error(), nil
	}
	text := fmt.Sprintf("%sUNDONE %s\n\n%s", intro, strings.TrimPrefix(c.Label, "undo "), c.Diff)
	if err != nil {
		text += "\nThe solution could not be loaded again: " + err.Error()
	}
	return text, nil
}
