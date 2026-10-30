package tsapi

import (
	"fmt"
	"slices"
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
	cl, why := l.caseClause(function, value)
	if why != "" {
		return 0, 0, why
	}
	return l.caseBody(cl, value, withBreak)
}

// caseClause returns the clause of the case value of a switch in the
// function.
func (l *lined) caseClause(function, value string) (*ast.Node, string) {
	fn, _, _, why := l.function(function)
	if why != "" {
		return nil, why
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
		return nil, fmt.Sprintf("%s has no case %s", function, value)
	case len(found) > 1:
		return nil, fmt.Sprintf("%s has %d cases %s (%s and %s); give start_line and end_line", function, len(found), value, l.where(found[0]), l.where(found[1]))
	}
	return found[0], ""
}

// caseStatements returns the statements of the clause cl, those of a block
// that is all it holds.
func caseStatements(cl *ast.Node) []*ast.Node {
	stmts := cl.Statements()
	if len(stmts) == 1 && ast.IsBlock(stmts[0]) {
		stmts = stmts[0].Statements()
	}
	return stmts
}

// caseBody returns the first and last lines of the body of the clause cl,
// of the case value; without a last unlabeled break unless withBreak.
func (l *lined) caseBody(cl *ast.Node, value string, withBreak bool) (first, last int, refusal string) {
	stmts := caseStatements(cl)
	if n := len(stmts); !withBreak && n > 0 && stmts[n-1].Kind == ast.KindBreakStatement && stmts[n-1].Label() == nil {
		stmts = stmts[:n-1]
	}
	if len(stmts) == 0 {
		return 0, 0, fmt.Sprintf("the case %s at %s has no statements of its own (it falls into the next case, or only breaks)", value, l.where(cl))
	}
	return l.line(l.pos(stmts[0])), l.line(stmts[len(stmts)-1].End() - 1), ""
}

// CaseBody returns the lines of the body of the case value of a switch in
// the function, as CaseLines does without the last break, where the case
// shares its body with the cases before it (case "fern": case "moss":):
// that shared body, and, when split, the edits that give value a case of
// its own with that body and the others a copy of it before.
func (p *Project) CaseBody(path, function, value string, split bool) (first, last int, edits []Edit, refusal string) {
	l, why := p.lined(path)
	if why != "" {
		return 0, 0, nil, why
	}
	cl, why := l.caseClause(function, value)
	if why != "" {
		return 0, 0, nil, why
	}
	clauses := cl.Parent.AsCaseBlock().Clauses.Nodes
	i := slices.Index(clauses, cl)
	j, k := i, i
	for j > 0 && len(clauses[j-1].Statements()) == 0 {
		j--
	}
	for k < len(clauses)-1 && len(clauses[k].Statements()) == 0 {
		k++
	}
	if j == k {
		first, last, why = l.caseBody(cl, value, false)
		return first, last, nil, why
	}
	body := clauses[k]
	if first, last, why = l.caseBody(body, value, false); why != "" || !split {
		return first, last, nil, why
	}
	stmts := caseStatements(body)
	end := stmts[len(stmts)-1]
	switch end.Kind {
	case ast.KindBreakStatement, ast.KindReturnStatement, ast.KindThrowStatement, ast.KindContinueStatement:
	default:
		return 0, 0, nil, fmt.Sprintf("the body the case %s shares (%s) does not end in break, return, throw or continue, so a copy of it would fall into the next case; give split false to extract the shared body", value, l.where(body))
	}
	for _, s := range body.Statements() {
		if names := declared(s); len(names) > 0 {
			return 0, 0, nil, fmt.Sprintf("the body the case %s shares (%s) declares %s; splitting it would copy the declaration into the same scope; give split false to extract the shared body", value, l.where(body), names[0].Text())
		}
	}
	// header is the text of a clause up to its colon.
	header := func(c *ast.Node) string {
		from := l.pos(c) + len("default")
		if !ast.IsDefaultClause(c) {
			if e := c.Expression(); !ast.IsLiteralExpression(e) && !ast.IsIdentifier(e) && !ast.IsPropertyAccessExpression(e) {
				return ""
			}
			from = c.Expression().End()
		}
		colon := strings.IndexByte(l.text[from:], ':')
		return l.text[l.pos(c) : from+colon+1]
	}
	var others []string
	for m := j; m <= k; m++ {
		c := clauses[m]
		h := header(c)
		if h == "" {
			return 0, 0, nil, fmt.Sprintf("the case at %s is not a constant; splitting the cases would change the order they are compared in", l.where(c))
		}
		if l.line(l.pos(c)) != l.line(l.pos(clauses[j]))+m-j || strings.TrimSpace(l.text[l.start(l.line(l.pos(c))):l.end(l.line(l.pos(c)))]) != h {
			return 0, 0, nil, fmt.Sprintf("the cases %s shares its body with are not one to a line (%s); put each on a line of its own first", value, l.where(c))
		}
		if m != i {
			others = append(others, l.indent(l.line(l.pos(c)))+h+"\n")
		}
	}
	firstAll := l.line(l.pos(stmts[0]))
	lastAll := l.line(end.End() - 1)
	text := strings.Join(others, "") + l.text[l.start(firstAll):l.start(lastAll+1)] + l.indent(l.line(l.pos(cl))) + header(cl) + "\n"
	from, to := l.start(l.line(l.pos(clauses[j]))), l.start(l.line(l.pos(body))+1)
	return first, last, []Edit{{File: l.f.FileName(), Start: from, End: to, NewText: text}}, ""
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
