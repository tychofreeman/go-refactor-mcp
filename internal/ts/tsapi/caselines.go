package tsapi

import (
	"fmt"
	"slices"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
)

// CaseLines returns the first and last lines of the body of the case
// value ("fern", fern, or default) of a switch in the function (name, or
// Class.method) of the file at path; without a last unlabeled break unless
// withBreak.
func (p *Project) CaseLines(path, function, value string, withBreak bool) (first, last int, refusal string) {
	l, why := p.Lined(path)
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
func (l *Lined) caseClause(function, value string) (*ast.Node, string) {
	fn, _, _, why := l.function(function)
	if why != "" {
		return nil, why
	}
	matches := func(cl *ast.Node) bool {
		if ast.IsDefaultClause(cl) {
			return value == "default"
		}
		s := l.Source(cl.Expression())
		return s == value || s == `"`+value+`"` || s == "'"+value+"'" || strings.Trim(s, `"'`) == strings.Trim(value, `"'`) && strings.ContainsAny(s, `"'`)
	}
	var found []*ast.Node
	Inspect(fn.Body(), func(n *ast.Node) bool {
		if ast.IsSwitchStatement(n) {
			for _, cl := range n.AsSwitchStatement().CaseBlock.AsCaseBlock().Clauses.Nodes {
				if matches(cl) {
					found = append(found, cl)
				}
			}
		}
		return true
	})
	switch {
	case len(found) == 0:
		return nil, fmt.Sprintf("%s has no case %s", function, value)
	case len(found) > 1:
		return nil, fmt.Sprintf("%s has %d cases %s (%s and %s); give start_line and end_line", function, len(found), value, l.Where(found[0]), l.Where(found[1]))
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
func (l *Lined) caseBody(cl *ast.Node, value string, withBreak bool) (first, last int, refusal string) {
	stmts := caseStatements(cl)
	if n := len(stmts); !withBreak && n > 0 && stmts[n-1].Kind == ast.KindBreakStatement && stmts[n-1].Label() == nil {
		stmts = stmts[:n-1]
	}
	if len(stmts) == 0 {
		return 0, 0, fmt.Sprintf("the case %s at %s has no statements of its own (it falls into the next case, or only breaks)", value, l.Where(cl))
	}
	return l.Line(l.Pos(stmts[0])), l.Line(stmts[len(stmts)-1].End() - 1), ""
}

// CaseBody returns the lines of the body of the case value of a switch in
// the function, as CaseLines does without the last break, where the case
// shares its body with the cases before it (case "fern": case "moss":):
// that shared body, and, when split, the edits that give value a case of
// its own with that body and the others a copy of it before.
func (p *Project) CaseBody(path, function, value string, split bool) (first, last int, edits []Edit, refusal string) {
	l, why := p.Lined(path)
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
		return 0, 0, nil, fmt.Sprintf("the body the case %s shares (%s) does not end in break, return, throw or continue, so a copy of it would fall into the next case; give split false to extract the shared body", value, l.Where(body))
	}
	for _, s := range body.Statements() {
		if names := Declared(s); len(names) > 0 {
			return 0, 0, nil, fmt.Sprintf("the body the case %s shares (%s) declares %s; splitting it would copy the declaration into the same scope; give split false to extract the shared body", value, l.Where(body), names[0].Text())
		}
	}
	// header is the text of a clause up to its colon.
	header := func(c *ast.Node) string {
		from := l.Pos(c) + len("default")
		if !ast.IsDefaultClause(c) {
			if e := c.Expression(); !ast.IsLiteralExpression(e) && !ast.IsIdentifier(e) && !ast.IsPropertyAccessExpression(e) {
				return ""
			}
			from = c.Expression().End()
		}
		colon := strings.IndexByte(l.Text[from:], ':')
		return l.Text[l.Pos(c) : from+colon+1]
	}
	var others []string
	for m := j; m <= k; m++ {
		c := clauses[m]
		h := header(c)
		if h == "" {
			return 0, 0, nil, fmt.Sprintf("the case at %s is not a constant; splitting the cases would change the order they are compared in", l.Where(c))
		}
		if l.Line(l.Pos(c)) != l.Line(l.Pos(clauses[j]))+m-j || strings.TrimSpace(l.Text[l.Start(l.Line(l.Pos(c))):l.End(l.Line(l.Pos(c)))]) != h {
			return 0, 0, nil, fmt.Sprintf("the cases %s shares its body with are not one to a line (%s); put each on a line of its own first", value, l.Where(c))
		}
		if m != i {
			others = append(others, l.Indent(l.Line(l.Pos(c)))+h+"\n")
		}
	}
	firstAll := l.Line(l.Pos(stmts[0]))
	lastAll := l.Line(end.End() - 1)
	text := strings.Join(others, "") + l.Text[l.Start(firstAll):l.Start(lastAll+1)] + l.Indent(l.Line(l.Pos(cl))) + header(cl) + "\n"
	from, to := l.Start(l.Line(l.Pos(clauses[j]))), l.Start(l.Line(l.Pos(body))+1)
	return first, last, []Edit{{File: l.File.FileName(), Start: from, End: to, NewText: text}}, ""
}

// A MissingCase is a case to add at the end of a switch, for a value it
// has no case for, whose body calls the function extracted from the code
// after the switch, which stays.
type MissingCase struct {
	First, Last int // the lines of the code after the switch
	file        string
	from, to    int    // that code's offsets
	at          int    // the start of the line of the switch's closing brace
	header      string // the new case's line, indented
	indent      string // its body's indentation
}

// MissingCase answers refusal, CaseBody's for a value that no case of the
// function's switch has, when the code after the switch is what runs for
// it: the function has one switch, with no default, each of whose cases
// ends in a jump, and the statements after it in its block end in a return
// or throw. Otherwise it returns the refusal.
func (p *Project) MissingCase(path, function, value, refusal string) (*MissingCase, string) {
	l, why := p.Lined(path)
	if why != "" || value == "default" || !strings.Contains(refusal, " has no case ") {
		return nil, refusal
	}
	fn, _, _, why := l.function(function)
	if why != "" {
		return nil, refusal
	}
	var switches []*ast.Node
	Inspect(fn.Body(), func(n *ast.Node) bool {
		if ast.IsSwitchStatement(n) {
			switches = append(switches, n)
		}
		return true
	})
	if len(switches) != 1 || !InList(switches[0]) {
		return nil, refusal
	}
	sw := switches[0]
	clauses := sw.AsSwitchStatement().CaseBlock.AsCaseBlock().Clauses.Nodes
	quoted := false
	for i, cl := range clauses {
		stmts := caseStatements(cl)
		if ast.IsDefaultClause(cl) || len(stmts) == 0 && i == len(clauses)-1 || len(stmts) > 0 && !Terminates(stmts[len(stmts)-1]) {
			return nil, refusal
		}
		quoted = quoted || ast.IsStringLiteral(cl.Expression())
	}
	list := sw.Parent.Statements()
	rest := list[slices.Index(list, sw)+1:]
	if len(rest) == 0 || len(clauses) == 0 {
		return nil, refusal
	}
	if end := rest[len(rest)-1]; !Terminates(end) || end.Kind == ast.KindBreakStatement || end.Kind == ast.KindContinueStatement {
		return nil, refusal
	}
	closing := l.Line(sw.End() - 1)
	if closing == l.Line(l.Pos(sw)) {
		return nil, refusal
	}
	if quoted && !strings.ContainsAny(value, `"'.`) {
		value = l.quote() + value + l.quote() // a bare word, as for a case that is there
	}
	caseIndent := l.Indent(l.Line(l.Pos(clauses[0])))
	last := rest[len(rest)-1]
	return &MissingCase{
		First:  l.Line(l.Pos(rest[0])),
		Last:   l.Line(last.End() - 1),
		file:   l.File.FileName(),
		from:   l.Pos(rest[0]),
		to:     last.End(),
		at:     l.Start(closing),
		header: caseIndent + "case " + value + ":\n",
		indent: caseIndent + l.Unit(),
	}, ""
}

// Place moves the call that edits put in place of the code after the
// switch into the new case, so that code stays. A nil m returns edits.
func (m *MissingCase) Place(edits []Edit) []Edit {
	if m == nil {
		return edits
	}
	for i, e := range edits {
		if e.File != m.file || e.Start > m.from || e.End < m.to {
			continue
		}
		lines := strings.Split(strings.Trim(e.NewText, "\n"), "\n")
		from := textutil.Indent(lines[0])
		for j, line := range lines {
			lines[j] = m.indent + strings.TrimPrefix(line, from)
		}
		edits[i] = Edit{File: m.file, Start: m.at, End: m.at, NewText: m.header + strings.Join(lines, "\n") + "\n"}
	}
	return edits
}

// FunctionSpan returns the byte offsets of the function (name, or
// Class.method) of the file at path, from its first token to its end.
func (p *Project) FunctionSpan(path, function string) (start, end int, refusal string) {
	l, why := p.Lined(path)
	if why != "" {
		return 0, 0, why
	}
	fn, _, _, why := l.function(function)
	if why != "" {
		return 0, 0, why
	}
	return l.Pos(fn), fn.End(), ""
}
