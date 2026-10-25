package alerts

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"reflect"
	"strings"
	"testing"
)

// TestAcceptOneFunction checks that at least nine of the Probe methods are a
// call of one package-level function.
func TestAcceptOneFunction(t *testing.T) {
	pkgs, err := parser.ParseDir(token.NewFileSet(), ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	callees := map[string]int{}
	for _, p := range pkgs {
		for _, f := range p.Files {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Recv == nil || fd.Name.Name != "Probe" || len(fd.Body.List) != 1 {
					continue
				}
				if es, ok := fd.Body.List[0].(*ast.ExprStmt); ok {
					if call, ok := es.X.(*ast.CallExpr); ok {
						if id, ok := call.Fun.(*ast.Ident); ok {
							callees[id.Name]++
						}
					}
				}
			}
		}
	}
	for _, n := range callees {
		if n >= 9 {
			return
		}
	}
	t.Errorf("Probe methods that are one call, by function: %v", callees)
}

func TestAcceptAtLimit(t *testing.T) {
	var alerts []string
	for _, c := range []interface{ Probe(float64, *[]string) }{
		Temp{"r", 1}, Humidity{"r", 1}, Smoke{"r", 1}, Load{"r", 1}, Noise{"r", 1},
		Dust{"r", 1}, Gas{"r", 1}, Vibration{"r", 1}, Light{"r", 1}, Flood{"r", 1},
	} {
		c.Probe(1, &alerts)
	}
	want := []string{"r: temp 1.0 over 1.0", "r: humidity 1.0 over 1.0", "r: smoke 1.0 over 1.0",
		"r: load 1.0 over 1.0", "r: noise 1.0 over 1.0", "r: dust 1.0 over 1.0",
		"r: vibration 1.0 over 1.0", "r: light 1.0 over 1.0", "r: flood 1.0 over 1.0"}
	if !reflect.DeepEqual(alerts, want) {
		t.Errorf("alerts at the limit = %q, want %q (gas alerts only above it)", alerts, want)
	}
}
