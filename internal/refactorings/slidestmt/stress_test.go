package slidestmt

// Opt-in stress test: applies move_statement_up to the statements of a
// real module (each separately, except the first of each block) and
// reports every change that broke the build (a rollback means a
// precondition is missing) or, with STRESS_TESTS=1, made the package's
// own tests fail (a behavior change). It modifies STRESS_DIR, so point it
// at a copy:
//
//	STRESS_DIR=/tmp/copy-of-module [STRESS_WORK=off] [STRESS_N=400] \
//	[STRESS_SEED=1] [STRESS_TESTS=1] go test -run TestStress -v -timeout 1h .

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"math/rand"
	"os"
	"regexp"
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
			ast.Inspect(f, func(n ast.Node) bool {
				var list []ast.Stmt
				switch n := n.(type) {
				case *ast.BlockStmt:
					list = n.List
				case *ast.CaseClause:
					list = n.Body
				case *ast.CommClause:
					list = n.Body
				}
				for i, s := range list {
					switch s.(type) {
					case *ast.CaseClause, *ast.CommClause:
						continue
					}
					if i > 0 {
						cands = append(cands, cand{ws.Rel(name), ws.Fset.Position(s.Pos()).Line})
					}
				}
				return true
			})
		}
	}
	rand.New(rand.NewSource(int64(envInt("STRESS_SEED", 1)))).Shuffle(len(cands), func(i, j int) { cands[i], cands[j] = cands[j], cands[i] })
	n := min(envInt("STRESS_N", 400), len(cands))
	applied := 0
	refused := map[string]int{}
	var bugs []string
	r := NewUp()
	for _, c := range cands[:n] {
		args, _ := json.Marshal(map[string]any{"file": c.file, "line": c.line})
		res, err := refactor.Run(ws, r, args, refactor.Options{})
		if err != nil {
			bugs = append(bugs, fmt.Sprintf("ERROR %s:%d: %v", c.file, c.line, err))
			continue
		}
		switch {
		case res.Applied:
			applied++
			if os.Getenv("STRESS_TESTS") != "" {
				cmd := refactortest.GoTestCommand(ws.Root, c.file)
				if out, err := cmd.CombinedOutput(); err != nil {
					bugs = append(bugs, fmt.Sprintf("TESTS FAIL %s:%d:\n%s\n%s", c.file, c.line, res.Diff, tailLines(string(out), 15)))
				}
			}
			if _, err := ws.Undo(); err != nil {
				t.Fatalf("undo: %v", err)
			}
		case strings.Contains(res.Refused, "rolled back"):
			bugs = append(bugs, fmt.Sprintf("ROLLBACK %s:%d:\n%s", c.file, c.line, res.Refused))
		default:
			why := regexp.MustCompile(`[0-9]+`).ReplaceAllString(res.Refused, "N")
			if i := strings.IndexAny(why, "(;,"); i > 0 {
				why = why[:i]
			}
			refused[why]++
		}
	}
	var keys []string
	for k := range refused {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return refused[keys[i]] > refused[keys[j]] })
	t.Logf("tried %d statements: applied %d, bugs %d", n, applied, len(bugs))
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
