package tsapi

import (
	"slices"
	"testing"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
)

func TestInspect(t *testing.T) {
	f := parseText("function water(bed) { const depth = 2; }\nconst plant = 1;")
	names := func(descend func(*ast.Node) bool) []string {
		var out []string
		inspect(f.AsNode(), func(n *ast.Node) bool {
			if ast.IsIdentifier(n) {
				out = append(out, n.Text())
			}
			return descend(n)
		})
		return out
	}
	if got := names(func(*ast.Node) bool { return true }); !slices.Equal(got, []string{"water", "bed", "depth", "plant"}) {
		t.Errorf("every identifier: got %v", got)
	}
	if got := names(func(n *ast.Node) bool { return !ast.IsFunctionDeclaration(n) }); !slices.Equal(got, []string{"plant"}) {
		t.Errorf("skipping the function: got %v", got)
	}
}
