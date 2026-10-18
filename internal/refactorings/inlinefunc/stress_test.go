package inlinefunc

// Opt-in tests on a real module. They modify STRESS_DIR, so point it at a
// copy:
//
//	STRESS_DIR=/tmp/copy-of-module [STRESS_WORK=off] [STRESS_N=400] \
//	[STRESS_SEED=1] [STRESS_TESTS=1] [STRESS_DIFFS=10] \
//	go test -run 'TestStress|TestRoundTrip' -v -timeout 1h .
//
// TestStress inlines calls to the module's own functions, one at a time,
// and reports every change that broke the build (a rollback means a
// precondition is missing) or, with STRESS_TESTS=1, made the package's own
// tests fail (a behavior change). STRESS_DIFFS=n logs n applied diffs, to
// read for quality.
//
// TestRoundTrip applies extract_function to runs of statements, then
// inline_function to the new call. The two are inverses, so the file must
// come back unchanged (after gofmt).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/types"
	"math/rand"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor/refactortest"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings/extractfunc"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

func loadStress(t *testing.T) *workspace.Workspace {
	dir := os.Getenv("STRESS_DIR")
	if dir == "" {
		t.Skip("set STRESS_DIR")
	}
	if w := os.Getenv("STRESS_WORK"); w != "" {
		t.Setenv("GOWORK", w)
	}
	ws, err := workspace.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if errs := ws.Errors(); len(errs) > 0 {
		t.Fatalf("target does not type-check: %v", errs[:min(5, len(errs))])
	}
	return ws
}

// sourceFiles calls f once per non-test source file of the module.
func sourceFiles(ws *workspace.Workspace, f func(p *types.Info, file *ast.File, name string)) {
	seen := map[string]bool{}
	for _, p := range ws.Pkgs {
		for _, file := range p.Syntax {
			name := ws.Fset.File(file.Pos()).Name()
			if seen[name] || !ws.Contains(name) || strings.HasSuffix(name, "_test.go") || strings.Contains(name, "testdata") {
				continue
			}
			seen[name] = true
			f(p.TypesInfo, file, name)
		}
	}
}

func runTests(ws *workspace.Workspace, file string) string {
	if os.Getenv("STRESS_TESTS") == "" {
		return ""
	}
	cmd := refactortest.GoTestCommand(ws.Root, file)
	if out, err := cmd.CombinedOutput(); err != nil {
		return tailLines(string(out), 15)
	}
	return ""
}

func reason(r string) string {
	r = strings.TrimPrefix(r, "the call to ")
	if i := strings.IndexAny(r, "`\"0123456789"); i == 0 { // skip a leading name
		if j := strings.IndexAny(r[1:], "`\""); j >= 0 {
			r = strings.TrimSpace(r[j+2:])
		}
	}
	if i := strings.IndexAny(r, "`\"0123456789"); i > 0 {
		r = r[:i]
	}
	if len(r) > 60 {
		r = r[:60]
	}
	return r
}

func logCounts(t *testing.T, what string, m map[string]int) {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return m[keys[i]] > m[keys[j]] || (m[keys[i]] == m[keys[j]] && keys[i] < keys[j]) })
	for _, k := range keys {
		t.Logf("  %4d %s: %s", m[k], what, k)
	}
}

func TestStress(t *testing.T) {
	ws := loadStress(t)
	type cand struct {
		file      string
		line, col int
		name      string
	}
	var cands []cand
	sourceFiles(ws, func(info *types.Info, f *ast.File, name string) {
		tf := ws.Fset.File(f.Pos())
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			var id *ast.Ident
			switch fun := call.Fun.(type) {
			case *ast.Ident:
				id = fun
			case *ast.SelectorExpr:
				id = fun.Sel
			}
			if id == nil {
				return true
			}
			if fn, ok := info.Uses[id].(*types.Func); ok && fn.Pkg() != nil && ws.Contains(ws.Fset.Position(fn.Pos()).Filename) {
				pos := tf.Position(id.Pos())
				cands = append(cands, cand{ws.Rel(name), pos.Line, pos.Column, id.Name})
			}
			return true
		})
	})
	sort.Slice(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		return fmt.Sprintf("%s:%06d:%04d", a.file, a.line, a.col) < fmt.Sprintf("%s:%06d:%04d", b.file, b.line, b.col)
	})
	rand.New(rand.NewSource(int64(envInt("STRESS_SEED", 1)))).Shuffle(len(cands), func(i, j int) { cands[i], cands[j] = cands[j], cands[i] })
	n := min(envInt("STRESS_N", 400), len(cands))
	applied, deleted := 0, 0
	refused := map[string]int{}
	var bugs, diffs []string
	for _, c := range cands[:n] {
		where := fmt.Sprintf("%s:%d:%d %s", c.file, c.line, c.col, c.name)
		args, _ := json.Marshal(map[string]any{"file": c.file, "line": c.line, "column": c.col, "name": c.name})
		res, err := refactor.Run(ws, New(), args, refactor.Options{})
		switch {
		case err != nil:
			bugs = append(bugs, fmt.Sprintf("ERROR %s: %v", where, err))
		case res.Applied:
			applied++
			if strings.Contains(res.Summary, "deleted") {
				deleted++
			}
			if out := runTests(ws, c.file); out != "" {
				bugs = append(bugs, fmt.Sprintf("TESTS FAIL %s:\n%s\n%s", where, res.Diff, out))
			}
			if len(diffs) < envInt("STRESS_DIFFS", 0) {
				diffs = append(diffs, fmt.Sprintf("%s:\n%s", where, res.Diff))
			}
			if _, err := ws.Undo(); err != nil {
				t.Fatalf("undo: %v", err)
			}
		case strings.Contains(res.Refused, "rolled back"):
			bugs = append(bugs, fmt.Sprintf("ROLLBACK %s:\n%s", where, res.Refused))
		default:
			refused[reason(res.Refused)]++
		}
	}
	t.Logf("tried %d calls: applied %d (function deleted in %d), bugs %d", n, applied, deleted, len(bugs))
	logCounts(t, "refused", refused)
	for _, d := range diffs {
		t.Logf("DIFF %s", d)
	}
	for _, b := range bugs {
		t.Errorf("%s", b)
	}
}

func TestRoundTrip(t *testing.T) {
	ws := loadStress(t)
	type cand struct {
		file        string
		first, last int
	}
	var cands []cand
	sourceFiles(ws, func(_ *types.Info, f *ast.File, name string) {
		tf := ws.Fset.File(f.Pos())
		add := func(list []ast.Stmt) {
			for i := range list {
				for j := i; j < len(list) && j < i+4; j++ {
					cands = append(cands, cand{ws.Rel(name), tf.Line(list[i].Pos()), tf.Line(list[j].End())})
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.BlockStmt:
				add(n.List)
			case *ast.CaseClause:
				add(n.Body)
			case *ast.CommClause:
				add(n.Body)
			}
			return true
		})
	})
	rand.New(rand.NewSource(int64(envInt("STRESS_SEED", 1)))).Shuffle(len(cands), func(i, j int) { cands[i], cands[j] = cands[j], cands[i] })
	n := min(envInt("STRESS_N", 400), len(cands))
	extracted, back, identical := 0, 0, 0
	refusedBack := map[string]int{}
	var bugs, diffs []string
	for i, c := range cands[:n] {
		abs := ws.Abs(c.file)
		orig, _ := os.ReadFile(abs)
		name := fmt.Sprintf("zzRound%d", i)
		where := fmt.Sprintf("%s:%d-%d", c.file, c.first, c.last)
		eargs, _ := json.Marshal(map[string]any{"file": c.file, "start_line": c.first, "end_line": c.last, "name": name})
		res, err := refactor.Run(ws, extractfunc.New(), eargs, refactor.Options{})
		if err != nil || !res.Applied {
			continue
		}
		extracted++
		line := callLine(ws, abs, name)
		iargs, _ := json.Marshal(map[string]any{"file": c.file, "line": line, "name": name})
		res2, err := refactor.Run(ws, New(), iargs, refactor.Options{})
		switch {
		case err != nil:
			bugs = append(bugs, fmt.Sprintf("ERROR inlining back %s: %v", where, err))
		case strings.Contains(res2.Refused, "rolled back"):
			bugs = append(bugs, fmt.Sprintf("ROLLBACK inlining back %s:\n%s\n%s", where, res.Diff, res2.Refused))
		case !res2.Applied:
			refusedBack[reason(res2.Refused)]++
			if os.Getenv("STRESS_VERBOSE") != "" {
				t.Logf("NOT INLINED %s: %s\n%s", where, res2.Refused, res.Diff)
			}
		default:
			back++
			if out := runTests(ws, c.file); out != "" {
				bugs = append(bugs, fmt.Sprintf("TESTS FAIL after round trip %s:\n%s", where, out))
			}
			now, _ := os.ReadFile(abs)
			fo, _ := format.Source(orig)
			fn, _ := format.Source(now)
			if bytes.Equal(fo, fn) {
				identical++
			} else if len(diffs) < envInt("STRESS_DIFFS", 15) {
				diffs = append(diffs, fmt.Sprintf("%s:\n%s", where, edit.Unified(c.file, fo, fn)))
			}
			if _, err := ws.Undo(); err != nil {
				t.Fatalf("undo inline: %v", err)
			}
		}
		if _, err := ws.Undo(); err != nil {
			t.Fatalf("undo extract: %v", err)
		}
		if now, _ := os.ReadFile(abs); !bytes.Equal(now, orig) {
			t.Fatalf("undo did not restore %s", c.file)
		}
	}
	t.Logf("tried %d statement runs: extracted %d, inlined back %d, identical after round trip %d", n, extracted, back, identical)
	logCounts(t, "not inlined back", refusedBack)
	for _, d := range diffs {
		t.Logf("DIFFERENT %s", d)
	}
	for _, b := range bugs {
		t.Errorf("%s", b)
	}
}

// callLine returns the line of the first call to name in the file.
func callLine(ws *workspace.Workspace, path, name string) int {
	_, f := ws.FileSyntax(path)
	line := 0
	ast.Inspect(f, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok && line == 0 {
			var id *ast.Ident
			switch fun := c.Fun.(type) {
			case *ast.Ident:
				id = fun
			case *ast.SelectorExpr:
				id = fun.Sel
			case *ast.IndexExpr:
				id, _ = fun.X.(*ast.Ident)
			}
			if id != nil && id.Name == name {
				line = ws.Fset.Position(id.Pos()).Line
			}
		}
		return line == 0
	})
	return line
}

func envInt(k string, def int) int {
	var v int
	if _, err := fmt.Sscan(os.Getenv(k), &v); err != nil {
		return def
	}
	return v
}

func tailLines(s string, n int) string {
	l := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(l) > n {
		l = l[len(l)-n:]
	}
	return strings.Join(l, "\n")
}
