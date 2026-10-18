package extractvar

// Opt-in stress test: tries extract_variable on many expressions of a real
// module and reports every change that broke the build (a rollback means a
// precondition is missing) or, with STRESS_TESTS=1, made the package's own
// tests fail (a behavior change). It modifies STRESS_DIR, so point it at a
// copy:
//
//	STRESS_DIR=/tmp/copy-of-module [STRESS_WORK=off] [STRESS_N=400] \
//	[STRESS_SEED=1] [STRESS_TESTS=1] go test -run TestStress -v -timeout 1h .

import (
	"encoding/json"
	"fmt"
	"go/ast"
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
		file string
		line int
		col  int
		text string
	}
	var cands []cand
	seen := map[string]bool{}
	for _, p := range ws.Pkgs {
		for _, f := range p.Syntax {
			name := ws.Fset.File(f.Pos()).Name()
			if seen[name] || !ws.Contains(name) || strings.HasSuffix(name, "_test.go") || strings.Contains(name, "testdata") {
				continue
			}
			seen[name] = true
			src, _ := os.ReadFile(name)
			tf := ws.Fset.File(f.Pos())
			ast.Inspect(f, func(n ast.Node) bool {
				e, ok := n.(ast.Expr)
				if !ok {
					return true
				}
				text := string(src[tf.Offset(e.Pos()):tf.Offset(e.End())])
				if strings.Contains(text, "\n") || len(text) > 80 {
					return true
				}
				pos := tf.Position(e.Pos())
				cands = append(cands, cand{ws.Rel(name), pos.Line, pos.Column, text})
				return true
			})
		}
	}
	rand.New(rand.NewSource(int64(envInt("STRESS_SEED", 1)))).Shuffle(len(cands), func(i, j int) { cands[i], cands[j] = cands[j], cands[i] })
	n := envInt("STRESS_N", 400)
	if len(cands) < n {
		n = len(cands)
	}
	applied, refused := 0, map[string]int{}
	var bugs []string
	for i, c := range cands[:n] {
		for _, all := range []bool{false, true} {
			args, _ := json.Marshal(map[string]any{"file": c.file, "line": c.line, "column": c.col, "expression": c.text, "name": fmt.Sprintf("zzExtracted%d", i), "all_occurrences": all})
			res, err := refactor.Run(ws, New(), args, refactor.Options{})
			if err != nil {
				bugs = append(bugs, fmt.Sprintf("ERROR %s:%d:%d %q all=%v: %v", c.file, c.line, c.col, c.text, all, err))
				continue
			}
			switch {
			case res.Applied:
				applied++
				if os.Getenv("STRESS_TESTS") != "" {
					cmd := refactortest.GoTestCommand(ws.Root, c.file)
					if out, err := cmd.CombinedOutput(); err != nil {
						bugs = append(bugs, fmt.Sprintf("TESTS FAIL %s:%d:%d %q all=%v:\n%s\n%s", c.file, c.line, c.col, c.text, all, res.Diff, tailLines(string(out), 15)))
					}
				}
				if _, err := ws.Undo(); err != nil {
					t.Fatalf("undo: %v", err)
				}
			case strings.Contains(res.Refused, "rolled back"):
				bugs = append(bugs, fmt.Sprintf("ROLLBACK %s:%d:%d %q all=%v:\n%s", c.file, c.line, c.col, c.text, all, res.Refused))
			default:
				r := res.Refused
				if i := strings.IndexAny(r, "`\"0123456789"); i > 0 {
					r = r[:i]
				}
				if len(r) > 50 {
					r = r[:50]
				}
				refused[r]++
			}
		}
	}
	var keys []string
	for k := range refused {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return refused[keys[i]] > refused[keys[j]] })
	t.Logf("tried %d expressions x2: applied %d, bugs %d", n, applied, len(bugs))
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
