package changesig

// Opt-in stress test of convert_params_to_object: applies it to every
// function of a real module that has parameters, and reports every change
// that broke the build or, with STRESS_TESTS=1, made the package's own
// tests fail. It modifies STRESS_DIR, so point it at a copy:
//
//	STRESS_DIR=/tmp/copy-of-module [STRESS_N=300] [STRESS_TESTS=1] \
//	go test -run TestStressParamsToObject -v .

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor/refactortest"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

func TestStressParamsToObject(t *testing.T) {
	dir := os.Getenv("STRESS_DIR")
	if dir == "" {
		t.Skip("set STRESS_DIR")
	}
	ws, err := workspace.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	var cands []map[string]any
	seen := map[string]bool{}
	for _, p := range ws.Pkgs {
		for _, f := range p.Syntax {
			name := ws.Fset.File(f.Pos()).Name()
			if seen[name] || !ws.Contains(name) || strings.HasSuffix(name, "_test.go") {
				continue
			}
			seen[name] = true
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Body == nil || fd.Type.Params.NumFields() == 0 {
					continue
				}
				fn := fd.Name.Name
				if fd.Recv != nil {
					fn = receiverTypeName(fd.Recv.List[0].Type) + "." + fn
				}
				cands = append(cands, map[string]any{"file": ws.Rel(name), "name": fn})
			}
		}
	}
	n := min(300, len(cands))
	fmt.Sscan(os.Getenv("STRESS_N"), &n)
	n = min(n, len(cands))
	applied, refused := 0, map[string]int{}
	for _, c := range cands[:n] {
		args, _ := json.Marshal(c)
		res, err := refactor.Run(ws, NewParamsToObject(), args, refactor.Options{})
		switch {
		case err != nil:
			t.Errorf("ERROR %s: %v", args, err)
		case res.Applied:
			applied++
			if os.Getenv("STRESS_TESTS") != "" {
				if out, err := refactortest.GoTestCommand(ws.Root, c["file"].(string)).CombinedOutput(); err != nil {
					t.Errorf("TESTS FAIL %s:\n%s\n%s", args, res.Diff, out)
				}
			}
			if _, err := ws.Undo(); err != nil {
				t.Fatalf("undo: %v", err)
			}
		case strings.Contains(res.Refused, "rolled back"):
			t.Errorf("ROLLBACK %s:\n%s", args, res.Refused)
		default:
			why := regexp.MustCompile(`\S+\.go:\d+(:\d+)?|[0-9]+`).ReplaceAllString(res.Refused, "N")
			if i := strings.IndexAny(why, ";,("); i > 0 {
				why = why[:i]
			}
			refused[why]++
		}
	}
	t.Logf("tried %d: applied %d", n, applied)
	for k, v := range refused {
		if v > 1 {
			t.Logf("  %4d refused: %s", v, k)
		}
	}
}
