package tsapi

import (
	"fmt"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
)

// CaseLines returns the first and last lines of the body of the case
// value ("fern", fern, or default) of a switch in the function (name, or
// Class.method) of the file at path; without a last unlabeled break unless
// withBreak.
func (p *Project) CaseLines(path, function, value string, withBreak bool) (first, last int, refusal string) {
	l, why := p.lined(path)
	if why != "" {
		return 0, 0, why
	}
	fn, _, _, why := l.function(function)
	if why != "" {
		return 0, 0, why
	}
	matches := func(cl *ast.Node) bool {
		if ast.IsDefaultClause(cl) {
			return value == "default"
		}
		s := l.source(cl.Expression())
		return s == value || s == `"`+value+`"` || s == "'"+value+"'" || strings.Trim(s, `"'`) == strings.Trim(value, `"'`) && strings.ContainsAny(s, `"'`)
	}
	var found []*ast.Node
	var visit func(n *ast.Node) bool
	visit = func(n *ast.Node) bool {
		if ast.IsSwitchStatement(n) {
			for _, cl := range n.AsSwitchStatement().CaseBlock.AsCaseBlock().Clauses.Nodes {
				if matches(cl) {
					found = append(found, cl)
				}
			}
		}
		n.ForEachChild(visit)
		return false
	}
	visit(fn.Body())
	switch {
	case len(found) == 0:
		return 0, 0, fmt.Sprintf("%s has no case %s", function, value)
	case len(found) > 1:
		return 0, 0, fmt.Sprintf("%s has %d cases %s (%s and %s); give start_line and end_line", function, len(found), value, l.where(found[0]), l.where(found[1]))
	}
	stmts := found[0].Statements()
	if len(stmts) == 1 && ast.IsBlock(stmts[0]) {
		stmts = stmts[0].Statements()
	}
	if n := len(stmts); !withBreak && n > 0 && stmts[n-1].Kind == ast.KindBreakStatement && stmts[n-1].Label() == nil {
		stmts = stmts[:n-1]
	}
	if len(stmts) == 0 {
		return 0, 0, fmt.Sprintf("the case %s at %s has no statements of its own (it falls into the next case, or only breaks)", value, l.where(found[0]))
	}
	return l.line(l.pos(stmts[0])), l.line(stmts[len(stmts)-1].End() - 1), ""
}

// FunctionSpan returns the byte offsets of the function (name, or
// Class.method) of the file at path, from its first token to its end.
func (p *Project) FunctionSpan(path, function string) (start, end int, refusal string) {
	l, why := p.lined(path)
	if why != "" {
		return 0, 0, why
	}
	fn, _, _, why := l.function(function)
	if why != "" {
		return 0, 0, why
	}
	return l.pos(fn), fn.End(), ""
}
