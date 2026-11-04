package split

// Opt-in stress test: applies split to a real module, kind loop at each
// statement but the first of every loop's body, kind variable at each
// assignment of a single name and kind phase at each statement but the
// first of every function's body, and reports every change that broke the
// build (a rollback means a precondition is missing) or, with
// STRESS_TESTS=1, made the package's own tests fail (a behavior change).
// It modifies STRESS_DIR, so point it at a copy:
//
//	STRESS_DIR=/tmp/copy-of-module [STRESS_WORK=off] [STRESS_N=400] \
//	[STRESS_SEED=1] [STRESS_TESTS=1] [STRESS_SHOW=1] go test -run TestStress -v -timeout 1h .

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
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
	var cands []map[string]any
	seen := map[string]bool{}
	for _, p := range ws.Pkgs {
		for _, f := range p.Syntax {
			name := ws.Fset.File(f.Pos()).Name()
			if seen[name] || !ws.Contains(name) || strings.HasSuffix(name, "_test.go") || strings.Contains(name, "testdata") {
				continue
			}
			seen[name] = true
			file := ws.Rel(name)
			line := func(n ast.Node) int { return ws.Fset.Position(n.Pos()).Line }
			ast.Inspect(f, func(n ast.Node) bool {
				var body *ast.BlockStmt
				switch n := n.(type) {
				case *ast.ForStmt:
					body = n.Body
				case *ast.RangeStmt:
					body = n.Body
				case *ast.FuncDecl:
					if n.Body != nil {
						for _, s := range n.Body.List[min(1, len(n.Body.List)):] {
							cands = append(cands, map[string]any{"kind": "phase", "file": file, "line": line(s), "new_name": "secondPhase", "type": "PhaseData"})
						}
					}
				case *ast.AssignStmt:
					if n.Tok == token.ASSIGN && len(n.Lhs) == 1 {
						if id, ok := n.Lhs[0].(*ast.Ident); ok && id.Name != "_" {
							cands = append(cands, map[string]any{"kind": "variable", "file": file, "line": line(n), "name": id.Name, "new_name": id.Name + "Split"})
						}
					}
				}
				if body != nil {
					for _, s := range body.List[min(1, len(body.List)):] {
						cands = append(cands, map[string]any{"kind": "loop", "file": file, "line": line(n), "at": line(s)})
					}
				}
				return true
			})
		}
	}
	rand.New(rand.NewSource(int64(envInt("STRESS_SEED", 1)))).Shuffle(len(cands), func(i, j int) { cands[i], cands[j] = cands[j], cands[i] })
	n := min(envInt("STRESS_N", 400), len(cands))
	applied := map[string]int{}
	refused := map[string]int{}
	var bugs []string
	r := New()
	for _, c := range cands[:n] {
		args, _ := json.Marshal(c)
		res, err := refactor.Run(ws, r, args, refactor.Options{})
		if err != nil {
			bugs = append(bugs, fmt.Sprintf("ERROR %s: %v", args, err))
			continue
		}
		switch {
		case res.Applied:
			applied[c["kind"].(string)]++
			if os.Getenv("STRESS_SHOW") != "" {
				t.Logf("APPLIED %s:\n%s", args, res.Diff)
			}
			if os.Getenv("STRESS_TESTS") != "" {
				cmd := refactortest.GoTestCommand(ws.Root, c["file"].(string))
				if out, err := cmd.CombinedOutput(); err != nil {
					bugs = append(bugs, fmt.Sprintf("TESTS FAIL %s:\n%s\n%s", args, res.Diff, tailLines(string(out), 15)))
					t.Logf("%s", bugs[len(bugs)-1])
				}
			}
			if _, err := ws.Undo(); err != nil {
				t.Fatalf("undo: %v", err)
			}
		case strings.Contains(res.Refused, "rolled back"):
			bugs = append(bugs, fmt.Sprintf("ROLLBACK %s:\n%s", args, res.Refused))
		default:
			why := regexp.MustCompile(`[0-9]+`).ReplaceAllString(res.Refused, "N")
			if i := strings.IndexAny(why, "(;,"); i > 0 {
				why = why[:i]
			}
			refused[c["kind"].(string)+": "+why]++
		}
	}
	var keys []string
	for k := range refused {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return refused[keys[i]] > refused[keys[j]] })
	t.Logf("tried %d: applied %v, bugs %d", n, applied, len(bugs))
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
