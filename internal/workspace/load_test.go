package workspace

import (
	"go/token"
	"slices"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/history"
	"golang.org/x/tools/go/packages"
)

// typedLoad loads the module at root as Reload did before loadPackages:
// with go/packages' types, for which go list compiles every package.
func typedLoad(t *testing.T, root string) *Workspace {
	t.Helper()
	fset := token.NewFileSet()
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedTypesSizes | packages.NeedModule,
		Dir: root, Fset: fset, Tests: true, BuildFlags: trimpath,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		t.Fatal(err)
	}
	return &Workspace{Root: root, Base: root, Patterns: []string{"./..."}, Fset: fset, Pkgs: withoutTestMains(pkgs), History: &history.Stack{}}
}

// A load gives the workspace go/packages' load with types gives: the same
// packages, files, errors and type information.
func TestLoadMatchesTypedLoad(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"chain":          incModule,
		"variants":       variantModule,
		"missing import": withFile(incModule, "c/d.go", "package c\n\nimport \"example.com/m/nope\"\n\nvar _ = nope.X\n"),
		"type error":     withFile(incModule, "c/d.go", "package c\n\nvar X int = \"x\"\n"),
		"parse error":    withFile(incModule, "c/d.go", "package c\n\nfunc (\n"),
	} {
		t.Run(name, func(t *testing.T) {
			root := writeModule(t, copyMap(files))
			w, err := Load(root)
			if err != nil {
				t.Fatal(err)
			}
			want := typedLoad(t, root)
			if diffs := Compare(want, w, 15); len(diffs) > 0 {
				t.Errorf("differs from go/packages' load:\n  %s", strings.Join(diffs, "\n  "))
			}
			if !slices.Equal(w.Errors(), want.Errors()) {
				t.Errorf("errors = %v, want %v", w.Errors(), want.Errors())
			}
		})
	}
}
