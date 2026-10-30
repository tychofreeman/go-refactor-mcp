package tsapi

import (
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
)

// inspect calls f for n and for each node under it, depth first; where f
// returns false, it skips the nodes under that one.
func inspect(n *ast.Node, f func(*ast.Node) bool) {
	var visit func(*ast.Node) bool
	visit = func(n *ast.Node) bool {
		if f(n) {
			n.ForEachChild(visit)
		}
		return false
	}
	visit(n)
}

// sources returns the program's files that are the solution's own code:
// not declaration files, and not under node_modules.
func (p *Project) sources() []*ast.SourceFile {
	var out []*ast.SourceFile
	for _, f := range p.prog.GetProgram().GetSourceFiles() {
		if !f.IsDeclarationFile && !strings.Contains(f.FileName(), "/node_modules/") {
			out = append(out, f)
		}
	}
	return out
}
