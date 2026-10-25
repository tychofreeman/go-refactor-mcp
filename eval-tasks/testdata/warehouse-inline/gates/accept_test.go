package gates

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"reflect"
	"strings"
	"testing"
)

func TestAcceptHelpersGone(t *testing.T) {
	pkgs, err := parser.ParseDir(token.NewFileSet(), ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pkgs {
		for _, f := range p.Files {
			for _, d := range f.Decls {
				if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil {
					switch fd.Name.Name {
					case "both", "either", "within":
						t.Errorf("%s is still declared", fd.Name.Name)
					}
				}
			}
		}
	}
}

func TestAcceptAdmitChargesWithoutDoor(t *testing.T) {
	y := NewYard(nil)
	y.Paid["t1"] = true
	if y.Admit("t1") {
		t.Error("admitted without a door")
	}
	if !reflect.DeepEqual(y.Charged, []string{"t1"}) {
		t.Errorf("charged = %q, want [t1]", y.Charged)
	}
}

func TestAcceptExpressCharges(t *testing.T) {
	y := NewYard(nil)
	y.Paid["t1"] = true
	if !y.Express("t1") {
		t.Error("not admitted")
	}
	if !reflect.DeepEqual(y.Charged, []string{"t1"}) {
		t.Errorf("charged = %q, want [t1]", y.Charged)
	}
}

func TestAcceptOnTimeTakesOneSlot(t *testing.T) {
	y := NewYard(nil, 7, 12, 9)
	if !y.OnTime() || y.OnTime() || !y.OnTime() {
		t.Error("each call should take one slot: 7, 12, 9")
	}
}
