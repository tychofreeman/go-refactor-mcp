package extractiface

// Opt-in stress test: applies extract_interface to every named type of a
// real module that has methods, with its first two methods, and reports
// every change that broke the build (a rollback means a precondition is
// missing). It modifies STRESS_DIR, so point it at a copy:
//
//	STRESS_DIR=/tmp/copy-of-module [STRESS_N=400] go test -run TestStress -v .

import (
	"encoding/json"
	"fmt"
	"go/types"
	"os"
	"regexp"
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
	ws, err := workspace.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	var cands []map[string]any
	for _, p := range ws.Pkgs {
		if strings.HasSuffix(p.ID, ".test]") || strings.HasSuffix(p.PkgPath, "_test") {
			continue
		}
		scope := p.Types.Scope()
		for _, name := range scope.Names() {
			tn, ok := scope.Lookup(name).(*types.TypeName)
			if !ok || tn.IsAlias() {
				continue
			}
			mset := types.NewMethodSet(types.NewPointer(tn.Type()))
			var methods []string
			for i := 0; i < mset.Len() && len(methods) < 2; i++ {
				if mset.At(i).Obj().Pkg() == p.Types {
					methods = append(methods, mset.At(i).Obj().Name())
				}
			}
			file := ws.Fset.Position(tn.Pos()).Filename
			if len(methods) == 0 || !ws.Contains(file) || strings.HasSuffix(file, "_test.go") {
				continue
			}
			cands = append(cands, map[string]any{"file": ws.Rel(file), "name": name, "fields": methods, "new_type": name + "Iface"})
		}
	}
	n := len(cands)
	if v, err := fmt.Sscan(os.Getenv("STRESS_N"), &n); v == 0 || err != nil {
		n = min(400, len(cands))
	}
	n = min(n, len(cands))
	applied, refused := 0, map[string]int{}
	for _, c := range cands[:n] {
		args, _ := json.Marshal(c)
		res, err := refactor.Run(ws, New(), args, refactor.Options{})
		switch {
		case err != nil:
			t.Errorf("ERROR %s: %v", args, err)
		case res.Applied:
			applied++
			if _, err := ws.Undo(); err != nil {
				t.Fatalf("undo: %v", err)
			}
		case strings.Contains(res.Refused, "rolled back"):
			t.Errorf("ROLLBACK %s:\n%s", args, res.Refused)
		default:
			refused[regexp.MustCompile(`[A-Za-z0-9_]+Iface|[A-Z][A-Za-z0-9_]*`).ReplaceAllString(res.Refused, "X")]++
		}
	}
	t.Logf("tried %d: applied %d", n, applied)
	for k, v := range refused {
		t.Logf("  %4d refused: %s", v, k)
	}
}
