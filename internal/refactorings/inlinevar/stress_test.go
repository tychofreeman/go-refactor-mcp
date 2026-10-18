package inlinevar

// Opt-in stress test: tries inline_variable on every local variable and
// constant of a real module (or a random sample) and reports every change
// that broke the build (a rollback means a precondition is missing) or,
// with STRESS_TESTS=1, made the package's own tests fail (a behavior
// change). It modifies STRESS_DIR, so point it at a copy:
//
//	STRESS_DIR=/tmp/copy-of-module [STRESS_WORK=off] [STRESS_N=400] \
//	[STRESS_SEED=1] [STRESS_TESTS=1] go test -run TestStress -v -timeout 1h .

import (
	"encoding/json"
	"fmt"
	"go/types"
	"math/rand"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor/refactortest"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

func TestStress(t *testing.T) {
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
	type cand struct {
		file      string
		line, col int
		name      string
	}
	var cands []cand
	seen := map[string]bool{}
	for _, p := range ws.Pkgs {
		for id, o := range p.TypesInfo.Defs {
			if o == nil || o.Pkg() == nil || o.Parent() == nil || o.Parent() == o.Pkg().Scope() {
				continue
			}
			switch o := o.(type) {
			case *types.Var:
				if o.IsField() {
					continue
				}
			case *types.Const:
			default:
				continue
			}
			pos := ws.Fset.Position(id.Pos())
			if !ws.Contains(pos.Filename) || strings.HasSuffix(pos.Filename, "_test.go") {
				continue
			}
			key := fmt.Sprintf("%s:%d:%d", pos.Filename, pos.Line, pos.Column)
			if seen[key] {
				continue
			}
			seen[key] = true
			cands = append(cands, cand{ws.Rel(pos.Filename), pos.Line, pos.Column, id.Name})
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].file != cands[j].file {
			return cands[i].file < cands[j].file
		}
		return cands[i].line*1000+cands[i].col < cands[j].line*1000+cands[j].col
	})
	rand.New(rand.NewSource(int64(envInt("STRESS_SEED", 1)))).Shuffle(len(cands), func(i, j int) { cands[i], cands[j] = cands[j], cands[i] })
	n := min(envInt("STRESS_N", 400), len(cands))

	applied, refused := 0, map[string]int{}
	var bugs []string
	for _, c := range cands[:n] {
		args, _ := json.Marshal(map[string]any{"file": c.file, "line": c.line, "column": c.col, "name": c.name})
		res, err := refactor.Run(ws, New(), args, refactor.Options{})
		where := fmt.Sprintf("%s:%d:%d %s", c.file, c.line, c.col, c.name)
		switch {
		case err != nil:
			bugs = append(bugs, fmt.Sprintf("ERROR %s: %v", where, err))
		case res.Applied:
			applied++
			if os.Getenv("STRESS_TESTS") != "" {
				cmd := refactortest.GoTestCommand(ws.Root, c.file)
				if out, err := cmd.CombinedOutput(); err != nil {
					bugs = append(bugs, fmt.Sprintf("TESTS FAIL %s:\n%s\n%s", where, res.Diff, tailLines(string(out), 15)))
				}
			}
			if _, err := ws.Undo(); err != nil {
				t.Fatalf("undo: %v", err)
			}
		case strings.Contains(res.Refused, "rolled back"):
			bugs = append(bugs, fmt.Sprintf("ROLLBACK %s:\n%s", where, res.Refused))
		default:
			if v := os.Getenv("STRESS_VERBOSE"); v != "" && strings.Contains(res.Refused, v) {
				t.Logf("REFUSED %s: %s", where, res.Refused)
			}
			r := res.Refused
			if i := strings.IndexAny(r, "`\"0123456789"); i >= 0 && i < 12 {
				if j := strings.IndexAny(r[i+1:], "`\""); j >= 0 {
					r = r[i+1+j+1:]
				}
			}
			r = strings.TrimSpace(r)
			if i := strings.IndexAny(r, "`\"0123456789"); i > 0 {
				r = r[:i]
			}
			refused[r]++
		}
	}
	var keys []string
	for k := range refused {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return refused[keys[i]] > refused[keys[j]] })
	t.Logf("tried %d variables: applied %d, bugs %d", n, applied, len(bugs))
	for _, k := range keys {
		t.Logf("  %4d refused: %s", refused[k], k)
	}
	for _, b := range bugs {
		t.Errorf("%s", b)
	}
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
