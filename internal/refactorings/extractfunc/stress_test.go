package extractfunc

// Opt-in stress test: tries extract_function on many statement runs of a
// real module and reports every change that broke the build (a rollback
// means a precondition is missing) or, with STRESS_TESTS=1, made the
// package's own tests fail (a behavior change). Some candidates are random
// line ranges inside function bodies, which mostly check that bad
// selections are refused cleanly. It modifies STRESS_DIR, so point it at a
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
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
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
	rng := rand.New(rand.NewSource(int64(envInt("STRESS_SEED", 1))))
	type cand struct {
		file        string
		first, last int
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
			tf := ws.Fset.File(f.Pos())
			line := func(n ast.Node, end bool) int {
				if end {
					return tf.Line(n.End())
				}
				return tf.Line(n.Pos())
			}
			add := func(list []ast.Stmt) {
				for i := range list {
					for j := i; j < len(list) && j < i+4; j++ {
						cands = append(cands, cand{ws.Rel(name), line(list[i], false), line(list[j], true)})
					}
				}
			}
			ast.Inspect(f, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.BlockStmt:
					add(n.List)
					if len(n.List) > 0 && rng.Intn(4) == 0 { // a random range in the block
						lo, hi := line(n, false), line(n, true)
						a := lo + rng.Intn(hi-lo+1)
						cands = append(cands, cand{ws.Rel(name), a, a + rng.Intn(hi-a+1)})
					}
				case *ast.CaseClause:
					add(n.Body)
				case *ast.CommClause:
					add(n.Body)
				}
				return true
			})
		}
	}
	rng.Shuffle(len(cands), func(i, j int) { cands[i], cands[j] = cands[j], cands[i] })
	n := min(envInt("STRESS_N", 400), len(cands))
	applied, refused, kinds := 0, map[string]int{}, map[string]int{}
	var bugs []string
	for i, c := range cands[:n] {
		args, _ := json.Marshal(map[string]any{"file": c.file, "start_line": c.first, "end_line": c.last, "name": fmt.Sprintf("zzExtracted%d", i)})
		res, err := refactor.Run(ws, New(), args, refactor.Options{})
		if err != nil {
			bugs = append(bugs, fmt.Sprintf("ERROR %s:%d-%d: %v", c.file, c.first, c.last, err))
			continue
		}
		switch {
		case res.Applied:
			applied++
			for _, n := range res.Notes {
				for _, k := range []string{"to package level", "passed as a pointer", "is a method of", "added import", "always return", "early returns"} {
					if strings.Contains(n, k) {
						kinds[k]++
					}
				}
			}
			if os.Getenv("STRESS_TESTS") != "" {
				cmd := exec.Command("go", "test", "-count=1", "./"+filepath.Dir(c.file))
				cmd.Dir = ws.Root
				if out, err := cmd.CombinedOutput(); err != nil {
					bugs = append(bugs, fmt.Sprintf("TESTS FAIL %s:%d-%d:\n%s\n%s", c.file, c.first, c.last, res.Diff, tailLines(string(out), 15)))
				}
			}
			if _, err := ws.Undo(); err != nil {
				t.Fatalf("undo: %v", err)
			}
		case strings.Contains(res.Refused, "rolled back"):
			bugs = append(bugs, fmt.Sprintf("ROLLBACK %s:%d-%d:\n%s", c.file, c.first, c.last, res.Refused))
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
	var keys []string
	for k := range refused {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return refused[keys[i]] > refused[keys[j]] })
	t.Logf("tried %d selections: applied %d, bugs %d", n, applied, len(bugs))
	for _, k := range []string{"always return", "early returns", "passed as a pointer", "is a method of", "added import", "to package level"} {
		if kinds[k] > 0 {
			t.Logf("  %4d applied with a note containing %q", kinds[k], k)
		}
	}
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
