package analysis

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

func checkSource(t *testing.T, src string) (*token.FileSet, *types.Info) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Types: map[ast.Expr]types.TypeAndValue{}}
	if _, err := (&types.Config{}).Check("example.com/p", fset, []*ast.File{f}, info); err != nil {
		t.Fatal(err)
	}
	return fset, info
}

func TestInterfaceWithMethod(t *testing.T) {
	fset, info := checkSource(t, `package p

type sizer interface{ size() int }

func lit(v any) bool { _, ok := v.(interface{ tick() }); return ok }
`)
	tests := []struct {
		pkgPath, name string
		wantIface     string // "" for a literal
		wantLine      int    // 0 for not found
	}{
		{"example.com/p", "size", "sizer", 3},
		{"example.com/p", "tick", "", 5},
		{"example.com/q", "size", "", 0},
		{"example.com/p", "other", "", 0},
	}
	for _, tt := range tests {
		iface, at, found := InterfaceWithMethod([]*types.Info{info}, tt.pkgPath, tt.name)
		switch {
		case found != (tt.wantLine != 0):
			t.Errorf("%s.%s: found = %v", tt.pkgPath, tt.name, found)
		case !found:
		case (iface == nil) != (tt.wantIface == "") || iface != nil && iface.Name() != tt.wantIface:
			t.Errorf("%s.%s: iface = %v, want %q", tt.pkgPath, tt.name, iface, tt.wantIface)
		case fset.Position(at).Line != tt.wantLine:
			t.Errorf("%s.%s: at line %d, want %d", tt.pkgPath, tt.name, fset.Position(at).Line, tt.wantLine)
		}
	}
}
