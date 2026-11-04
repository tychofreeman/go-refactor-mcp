package inlinestruct

// Opt-in stress test: applies inline_struct to every field of a struct of
// a real module that holds another struct of the same package, with
// elsewhere keep, and reports every change that broke the build (a
// rollback means a precondition is missing) or, with STRESS_TESTS=1, made
// the package's own tests fail. It modifies STRESS_DIR, so point it at a
// copy:
//
//	STRESS_DIR=/tmp/copy-of-module [STRESS_TESTS=1] go test -run TestStress -v .

import (
	"encoding/json"
	"go/ast"
	"go/types"
	"os"
	"regexp"
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
			ast.Inspect(f, func(n ast.Node) bool {
				ts, ok := n.(*ast.TypeSpec)
				if !ok {
					return true
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					return true
				}
				for _, fl := range st.Fields.List {
					nt, ok := p.TypesInfo.TypeOf(fl.Type).(*types.Named)
					if !ok || nt.Obj().Pkg() != p.Types {
						continue
					}
					if _, ok := nt.Underlying().(*types.Struct); !ok {
						continue
					}
					field := embeddedName(fl.Type)
					if len(fl.Names) == 1 {
						field = fl.Names[0].Name
					}
					cands = append(cands, map[string]any{"file": ws.Rel(name), "name": ts.Name.Name, "field": field, "elsewhere": "keep"})
				}
				return true
			})
		}
	}
	applied, refused := 0, map[string]int{}
	for _, c := range cands {
		args, _ := json.Marshal(c)
		res, err := refactor.Run(ws, New(), args, refactor.Options{})
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
			why := regexp.MustCompile(`\S+\.go:\d+:\d+|[0-9]+`).ReplaceAllString(res.Refused, "N")
			if i := strings.IndexAny(why, ";,("); i > 0 {
				why = why[:i]
			}
			refused[why]++
		}
	}
	t.Logf("tried %d: applied %d", len(cands), applied)
	for k, v := range refused {
		t.Logf("  %4d refused: %s", v, k)
	}
}
