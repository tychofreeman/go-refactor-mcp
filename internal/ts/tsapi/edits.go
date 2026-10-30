package tsapi

import (
	"context"
	"fmt"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/checker"
)

// ReplaceDeclaration returns the edit that puts content in place of the
// declaration name ("name", or "Class.member") of the file at path,
// indented as it was. Its doc comment stays unless content starts with
// one of its own.
func (p *Project) ReplaceDeclaration(path, name, content string) ([]Edit, string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, why
	}
	var found []Decl
	for _, d := range p.Outline(path) {
		if d.Name == name {
			found = append(found, d)
		}
	}
	switch {
	case len(found) == 0:
		return nil, fmt.Sprintf("the file declares no %s (give name, or Class.member)", name)
	case len(found) > 1:
		return nil, fmt.Sprintf("the file declares %s %d times (overloads, or merged declarations); replace them with replace_text", name, len(found))
	}
	d := found[0]
	if strings.TrimSpace(content) == "" {
		// Deleted, with its doc comment and the blank line after it, or
		// before it when it is the last.
		from, to := l.start(d.Start), min(l.end(d.End)+1, len(l.text))
		switch {
		case to < len(l.text) && l.text[to] == '\n':
			to++
		case from > 1 && l.text[from-1] == '\n' && l.text[from-2] == '\n':
			from--
		}
		return []Edit{{File: l.f.FileName(), Start: from, End: to}}, ""
	}
	first := d.Start
	content = strings.Trim(content, "\n")
	if lead := strings.TrimSpace(content); !strings.HasPrefix(lead, "/*") && !strings.HasPrefix(lead, "//") {
		// The doc comment stays: start below it.
		for first < d.End {
			t := strings.TrimSpace(l.text[l.start(first):l.end(first)])
			if !(strings.HasPrefix(t, "/*") || strings.HasPrefix(t, "*") || strings.HasPrefix(t, "//")) {
				break
			}
			first++
		}
	}
	text := indented(content, l.indent(first))
	return []Edit{{File: l.f.FileName(), Start: l.start(first), End: l.end(d.End), NewText: text}}, ""
}

// AddElement returns the edit that adds element to the object or array
// literal that starts on line of the file at path (at column, when more
// than one does): at its end, or just after or before the element whose
// key is after or before (a property's name; an array element as
// written). It follows the literal's layout: on one line, or an element
// a line with or without a trailing comma.
func (p *Project) AddElement(path string, line, column int, element, after, before string) ([]Edit, string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, why
	}
	if after != "" && before != "" {
		return nil, "give after or before, not both"
	}
	var lits []*ast.Node
	var visit func(n *ast.Node) bool
	visit = func(n *ast.Node) bool {
		if (ast.IsObjectLiteralExpression(n) || ast.IsArrayLiteralExpression(n)) && l.line(l.pos(n)) == line && (column == 0 || l.pos(n)-l.start(line)+1 == column) {
			lits = append(lits, n)
		}
		n.ForEachChild(visit)
		return false
	}
	visit(l.f.AsNode())
	switch {
	case len(lits) == 0:
		return nil, fmt.Sprintf("no object or array literal starts on line %d", line)
	case len(lits) > 1 && column == 0:
		var at []string
		for _, n := range lits {
			at = append(at, fmt.Sprint(l.pos(n)-l.start(line)+1))
		}
		return nil, fmt.Sprintf("%d literals start on line %d (columns %s); give column", len(lits), line, strings.Join(at, ", "))
	}
	lit := lits[0]
	return l.addElement(lit, element, after, before)
}

// addElement returns the edit that adds element to the object or array
// literal lit, as AddElement does.
func (l *lined) addElement(lit *ast.Node, element string, after string, before string) ([]Edit, string) {
	var elems []*ast.Node
	if ast.IsObjectLiteralExpression(lit) {
		elems = lit.AsObjectLiteralExpression().Properties.Nodes
	} else {
		elems = lit.AsArrayLiteralExpression().Elements.Nodes
	}
	key := func(e *ast.Node) string {
		if ast.IsObjectLiteralExpression(lit) && e.Name() != nil {
			return l.source(e.Name())
		}
		return l.source(e)
	}
	element = strings.TrimSpace(element)
	closing := lit.End() - 1
	if len(elems) == 0 {
		return []Edit{{File: l.f.FileName(), Start: l.pos(lit) + 1, End: closing, NewText: map[bool]string{true: " " + element + " ", false: element}[ast.IsObjectLiteralExpression(lit)]}}, ""
	}
	multiline := l.line(l.pos(elems[0])) != l.line(l.pos(lit))
	at := len(elems) // the element goes before elems[at]
	if name := after + before; name != "" {
		at = -1
		for i, e := range elems {
			if key(e) == name {
				at = i
			}
		}
		if at < 0 {
			return nil, fmt.Sprintf("the literal has no element %s", name)
		}
		if after != "" {
			at++
		}
	}
	if !multiline {
		if at == len(elems) {
			return []Edit{{File: l.f.FileName(), Start: elems[at-1].End(), End: elems[at-1].End(), NewText: ", " + element}}, ""
		}
		return []Edit{{File: l.f.FileName(), Start: l.pos(elems[at]), End: l.pos(elems[at]), NewText: element + ", "}}, ""
	}
	ind := l.indent(l.line(l.pos(elems[0])))
	if at < len(elems) {
		s := l.start(l.line(l.pos(elems[at])))
		if strings.TrimSpace(l.text[s:l.pos(elems[at])]) != "" {
			return nil, fmt.Sprintf("the element %s shares its line with another", key(elems[at]))
		}
		return []Edit{{File: l.f.FileName(), Start: s, End: s, NewText: ind + element + ",\n"}}, ""
	}
	last := elems[len(elems)-1]
	trailing := strings.HasPrefix(strings.TrimSpace(l.text[last.End():closing]), ",")
	if trailing {
		e := l.end(l.line(last.End() - 1))
		return []Edit{{File: l.f.FileName(), Start: e, End: e, NewText: "\n" + ind + element + ","}}, ""
	}
	return []Edit{{File: l.f.FileName(), Start: last.End(), End: last.End(), NewText: ",\n" + ind + element}}, ""
}

// FillLiterals returns the edits that add "name: value" to every object
// literal of the project whose type is the interface, class or object type
// typeName at the top level of the file at path, and which has no property
// name (nor a spread, which may give it).
func (p *Project) FillLiterals(ctx context.Context, path, typeName, name, value string) ([]Edit, string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, why
	}
	var decl *ast.Node
	for _, s := range l.f.Statements.Nodes {
		if (ast.IsInterfaceDeclaration(s) || ast.IsClassDeclaration(s) || ast.IsTypeAliasDeclaration(s)) && s.Name() != nil && s.Name().Text() == typeName {
			decl = s
		}
	}
	if decl == nil {
		return nil, fmt.Sprintf("the file declares no interface, class or object type %s", typeName)
	}
	c, done := p.prog.GetProgram().GetTypeChecker(ctx)
	defer done()
	target := c.GetSymbolAtLocation(decl.Name())
	var edits []Edit
	for _, f := range p.prog.GetProgram().GetSourceFiles() {
		if f.IsDeclarationFile || strings.Contains(f.FileName(), "/node_modules/") {
			continue
		}
		fl := newLined(f)
		var visit func(n *ast.Node) bool
		visit = func(n *ast.Node) bool {
			if why != "" {
				return true
			}
			if ast.IsObjectLiteralExpression(n) && !mayHave(n, name) {
				if t := c.GetContextualType(n, checker.ContextFlagsNone); t != nil && target != nil && (t.Symbol() == target || t.Alias() != nil && t.Alias().Symbol() == target) {
					var es []Edit
					es, why = fl.addElement(n, name+": "+strings.TrimSpace(value), "", "")
					if why != "" {
						why = fmt.Sprintf("the literal at %s: %s", fl.where(n), why)
						return true
					}
					edits = append(edits, es...)
				}
			}
			return n.ForEachChild(visit)
		}
		visit(f.AsNode())
		if why != "" {
			return nil, why
		}
	}
	return edits, ""
}

// mayHave reports whether the object literal lit has a property called
// name, or a spread, which may give it one.
func mayHave(lit *ast.Node, name string) bool {
	for _, e := range lit.AsObjectLiteralExpression().Properties.Nodes {
		if ast.IsSpreadAssignment(e) {
			return true
		}
		if nm := e.Name(); nm != nil && (ast.IsIdentifier(nm) || ast.IsStringLiteral(nm)) && nm.Text() == name {
			return true
		}
	}
	return false
}

// AddCase returns the edits that add content to a switch or an if chain
// of the file at path. Clauses ("case x: ..." or "default: ...") go at the
// end of the switch on line (before a default that ends it, and the cases
// falling into that default), or before or after (where) the clause on
// line. Branches ("else if (c) { ... }" or "else { ... }") go at the end of
// the chain of the if on line (before its else), or before or after the
// else if on line.
func (p *Project) AddCase(path string, line int, where, content string) ([]Edit, string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, why
	}
	if where != "" && where != "before" && where != "after" {
		return nil, fmt.Sprintf("where is %q; give before or after", where)
	}
	content = strings.Trim(content, "\n")
	lead := strings.TrimSpace(content)
	var sw, clause, ifs *ast.Node
	var visit func(n *ast.Node) bool
	visit = func(n *ast.Node) bool {
		if l.line(l.pos(n)) == line || ast.IsIfStatement(n) && isElseIf(n) && l.line(elseKeyword(l, n)) == line {
			switch {
			case ast.IsSwitchStatement(n) && sw == nil && clause == nil:
				sw = n
			case (ast.IsCaseClause(n) || ast.IsDefaultClause(n)) && clause == nil:
				clause = n
			case ast.IsIfStatement(n) && ifs == nil:
				ifs = n
			}
		}
		n.ForEachChild(visit)
		return false
	}
	visit(l.f.AsNode())
	switch {
	case strings.HasPrefix(lead, "case") || strings.HasPrefix(lead, "default"):
		if clause != nil {
			sw = clause.Parent.Parent
		}
		if sw == nil {
			return nil, fmt.Sprintf("no switch or case clause starts on line %d", line)
		}
		return l.addClauses(sw, clause, where, content)
	case strings.HasPrefix(lead, "else"):
		if ifs == nil {
			return nil, fmt.Sprintf("no if or else if starts on line %d", line)
		}
		return l.addBranches(ifs, where, content)
	}
	return nil, "content is case or default clauses, or else if or else branches"
}

// addClauses adds the clauses content to the switch sw: before or after
// clause when it is given, else at its end.
func (l *lined) addClauses(sw, clause *ast.Node, where, content string) ([]Edit, string) {
	parsed := parseText("switch (0) {\n" + content + "\n}\n")
	if ds := parsed.Diagnostics(); len(ds) > 0 || len(parsed.Statements.Nodes) != 1 || !ast.IsSwitchStatement(parsed.Statements.Nodes[0]) {
		return nil, "content is not case or default clauses: " + messages(parsed.Diagnostics())
	}
	clauses := sw.AsSwitchStatement().CaseBlock.AsCaseBlock().Clauses.Nodes
	newOnes := parsed.Statements.Nodes[0].AsSwitchStatement().CaseBlock.AsCaseBlock().Clauses.Nodes
	for _, n := range newOnes {
		if ast.IsDefaultClause(n) {
			for _, c := range clauses {
				if ast.IsDefaultClause(c) {
					return nil, fmt.Sprintf("the switch has a default already (%s)", l.where(c))
				}
			}
		}
	}
	ind := l.indent(l.line(l.pos(sw))) + l.unit()
	if len(clauses) > 0 {
		ind = l.indent(l.line(l.pos(clauses[0])))
	}
	text := indented(content, ind) + "\n"
	var at int
	switch {
	case clause != nil && where == "before":
		at = l.start(l.line(l.pos(clause)))
	case clause != nil:
		at = l.start(l.line(clause.End()-1) + 1)
	default:
		i := len(clauses)
		if i > 0 && ast.IsDefaultClause(clauses[i-1]) {
			i--
			for i > 0 && len(clauses[i-1].Statements()) == 0 {
				i--
			}
		}
		if i < len(clauses) {
			at = l.start(l.line(l.pos(clauses[i])))
		} else {
			closing := sw.AsSwitchStatement().CaseBlock.End() - 1
			at = l.start(l.line(closing))
			if strings.TrimSpace(l.text[at:closing]) != "" {
				return nil, "the switch's closing brace shares its line; put it on a line of its own"
			}
		}
	}
	return []Edit{{File: l.f.FileName(), Start: at, End: at, NewText: text}}, ""
}

// addBranches adds the branches content (else if or else) to the chain
// of ifs: at its end when ifs heads it, else before or after ifs.
func (l *lined) addBranches(ifs *ast.Node, where, content string) ([]Edit, string) {
	parsed := parseText("if (0) {}\n" + content + "\n")
	if ds := parsed.Diagnostics(); len(ds) > 0 || len(parsed.Statements.Nodes) != 1 {
		return nil, "content is not else if or else branches: " + messages(parsed.Diagnostics())
	}
	finalElse := false
	for n := parsed.Statements.Nodes[0]; n != nil && ast.IsIfStatement(n); n = n.AsIfStatement().ElseStatement {
		if e := n.AsIfStatement().ElseStatement; e != nil && !ast.IsIfStatement(e) {
			finalElse = true
		}
	}
	head := ifs
	for isElseIf(head) {
		head = head.Parent
	}
	// The last if of the chain, and whether the chain ends with an else.
	last := head
	for e := last.AsIfStatement().ElseStatement; e != nil && ast.IsIfStatement(e); e = last.AsIfStatement().ElseStatement {
		last = e
	}
	hasElse := last.AsIfStatement().ElseStatement != nil
	ind := l.indent(l.line(l.pos(head)))
	text := indented(content, ind)
	text = strings.TrimPrefix(text, ind)
	var after *ast.Node // the then-branch the content follows
	switch {
	case ifs == head:
		if finalElse && hasElse {
			return nil, fmt.Sprintf("the chain has an else already (%s)", l.where(last.AsIfStatement().ElseStatement))
		}
		after = last.AsIfStatement().ThenStatement
	case where == "before":
		if finalElse {
			return nil, "an else goes last, at the end of the chain"
		}
		after = ifs.Parent.AsIfStatement().ThenStatement
	default:
		if finalElse && ifs.AsIfStatement().ElseStatement != nil {
			return nil, "an else goes last, at the end of the chain"
		}
		after = ifs.AsIfStatement().ThenStatement
	}
	at := after.End()
	return []Edit{{File: l.f.FileName(), Start: at, End: at, NewText: " " + text}}, ""
}

// isElseIf reports whether the if n is the else of another.
func isElseIf(n *ast.Node) bool {
	return n.Parent != nil && ast.IsIfStatement(n.Parent) && n.Parent.AsIfStatement().ElseStatement == n
}

// elseKeyword returns the position of the else before the else if n.
func elseKeyword(l *lined, n *ast.Node) int {
	then := n.Parent.AsIfStatement().ThenStatement
	if i := strings.Index(l.text[then.End():l.pos(n)], "else"); i >= 0 {
		return then.End() + i
	}
	return l.pos(n)
}

// indented returns text without its lines' common indentation, each line
// indented with indent instead.
func indented(text, indent string) string {
	lines := strings.Split(strings.Trim(text, "\n"), "\n")
	common, first := "", true
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		lead := ln[:len(ln)-len(strings.TrimLeft(ln, " \t"))]
		if first {
			common, first = lead, false
		}
		for !strings.HasPrefix(lead, common) {
			common = common[:len(common)-1]
		}
	}
	for i, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			lines[i] = ""
		} else {
			lines[i] = indent + strings.TrimPrefix(ln, common)
		}
	}
	return strings.Join(lines, "\n")
}
