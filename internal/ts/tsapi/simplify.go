package tsapi

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/checker"
	"github.com/buke/typescript-go-internal/v7/pkg/scanner"
)

// This file has what the refactorings that simplify code share: where a
// node's statement is, what runs before a node in it, and how often.

// inList reports whether n is a statement of a statement list.
func inList(n *ast.Node) bool {
	return n.Parent != nil && n.Parent.CanHaveStatements() && slices.Contains(n.Parent.Statements(), n)
}

// stmtOf returns the statement of a statement list that holds n, or nil
// when a function lies between them.
func stmtOf(n *ast.Node) *ast.Node {
	for x := n; x != nil; x = x.Parent {
		if inList(x) {
			return x
		}
		if x != n && ast.IsFunctionLike(x) || ast.IsClassLike(x) {
			return nil
		}
	}
	return nil
}

// timesIn returns how often n is evaluated for each time stmt runs: Once,
// Maybe (after && or ||, in a branch, after ?.) or Shared (in a loop's
// condition or body), with the part that makes it so; or a refusal when a
// function lies between them.
func (l *lined) timesIn(n, stmt *ast.Node) (Times, string, string) {
	times, why := Once, ""
	for x := n; x != stmt && x.Parent != nil; x = x.Parent {
		p := x.Parent
		maybe := false
		switch {
		case ast.IsFunctionLike(p), ast.IsClassLike(p):
			return 0, "", fmt.Sprintf("it is inside %s, which runs at another time", l.part(p))
		case ast.IsBinaryExpression(p):
			op := p.AsBinaryExpression().OperatorToken.Kind
			maybe = x == p.AsBinaryExpression().Right && (op == ast.KindAmpersandAmpersandToken || op == ast.KindBarBarToken ||
				op == ast.KindQuestionQuestionToken || ast.IsLogicalOrCoalescingAssignmentOperator(op))
		case ast.IsConditionalExpression(p):
			maybe = x != p.AsConditionalExpression().Condition
		case ast.IsIfStatement(p):
			maybe = x != p.AsIfStatement().Expression
		case ast.IsCaseClause(p), ast.IsDefaultClause(p):
			maybe = true
		case p.Flags&ast.NodeFlagsOptionalChain != 0 && x != p.Expression():
			maybe = true
		case ast.IsForStatement(p):
			if x != p.AsForStatement().Initializer {
				times, why = Shared, l.part(p)
			}
		case ast.IsWhileStatement(p), ast.IsDoStatement(p):
			times, why = Shared, l.part(p)
		case ast.IsForInOrOfStatement(p):
			if x != p.AsForInOrOfStatement().Expression {
				times, why = Shared, l.part(p)
			}
		}
		if maybe && times == Once {
			times, why = Maybe, l.part(p)
		}
	}
	return times, why, ""
}

// part names n by its first line and where it is: "the if at a.ts:3".
func (l *lined) part(n *ast.Node) string {
	src := l.source(n)
	if i := strings.IndexByte(src, '\n'); i >= 0 {
		src = strings.TrimSpace(src[:i]) + " ..."
	}
	return fmt.Sprintf("%s at %s", src, l.where(n))
}

// parts returns the largest pieces of root, statements or expressions,
// that run before the offset until and overlap none of skip: what runs
// in root before the node there.
func parts(root *ast.Node, until int, skip []*ast.Node) []*ast.Node {
	var out []*ast.Node
	var visit func(n *ast.Node)
	visit = func(n *ast.Node) {
		n.ForEachChild(func(c *ast.Node) bool {
			if c.Pos() >= until {
				return true
			}
			holds := c.End() > until || slices.ContainsFunc(skip, func(s *ast.Node) bool { return s.Pos() >= c.Pos() && s.End() <= c.End() })
			switch {
			case slices.Contains(skip, c):
			case holds:
				if !ast.IsFunctionLike(c) {
					visit(c)
				}
			case ast.IsExpression(c) || ast.IsStatement(c):
				out = append(out, c)
			}
			return false
		})
	}
	visit(root)
	return out
}

// symbolOf is what the identifier id refers to: for a shorthand property
// ({ bed }), the value, not the property.
func symbolOf(c *checker.Checker, id *ast.Node) *ast.Symbol {
	if id.Parent != nil && ast.IsShorthandPropertyAssignment(id.Parent) && id.Parent.Name() == id {
		return c.GetShorthandAssignmentValueSymbol(id.Parent)
	}
	s := c.GetSymbolAtLocation(id)
	if s != nil && s.Flags&ast.SymbolFlagsAlias != 0 {
		return c.GetAliasedSymbol(s)
	}
	return s
}

// identifiers calls fn with each identifier in n, n included.
func identifiers(n *ast.Node, fn func(id *ast.Node)) {
	var visit func(x *ast.Node) bool
	visit = func(x *ast.Node) bool {
		if ast.IsIdentifier(x) {
			fn(x)
		}
		x.ForEachChild(visit)
		return false
	}
	visit(n)
}

// free returns the identifiers of e that refer to values declared outside
// it, leaving out property names.
func free(c *checker.Checker, e *ast.Node) []*ast.Node {
	var out []*ast.Node
	identifiers(e, func(id *ast.Node) {
		p := id.Parent
		if p != nil && (ast.IsPropertyAccessExpression(p) && p.Name() == id || ast.IsPropertyAssignment(p) && p.Name() == id ||
			ast.IsQualifiedName(p) || ast.IsPartOfTypeNode(id)) {
			return
		}
		s := symbolOf(c, id)
		if s == nil || s.Flags&ast.SymbolFlagsValue == 0 {
			return
		}
		if d := s.ValueDeclaration; d != nil && ast.GetSourceFileOfNode(d) == ast.GetSourceFileOfNode(e) && d.Pos() >= e.Pos() && d.End() <= e.End() {
			return
		}
		out = append(out, id)
	})
	return out
}

// seenAlike returns why the names ids would not mean at at what they mean
// where they are, or "".
func seenAlike(c *checker.Checker, l *lined, ids []*ast.Node, at *ast.Node) string {
	for _, id := range ids {
		s := symbolOf(c, id)
		if r := c.ResolveName(id.Text(), at, ast.SymbolFlagsValue, false); r == nil || c.SkipAlias(r) != s {
			return fmt.Sprintf("%s at %s would mean something else at %s", id.Text(), newLined(ast.GetSourceFileOfNode(id)).where(id), l.where(at))
		}
	}
	return ""
}

// taken returns why name cannot be declared where at is: something of
// that name is already seen there.
func taken(c *checker.Checker, l *lined, name string, at *ast.Node) string {
	if !scanner.IsIdentifierText(name, 0) {
		return fmt.Sprintf("%q is not a valid name", name)
	}
	if s := c.ResolveName(name, at, ast.SymbolFlagsValue|ast.SymbolFlagsType|ast.SymbolFlagsNamespace, false); s != nil {
		where := "the standard library"
		if d := s.ValueDeclaration; d != nil && !ast.GetSourceFileOfNode(d).IsDeclarationFile {
			where = newLined(ast.GetSourceFileOfNode(d)).where(d)
		} else if len(s.Declarations) > 0 && !ast.GetSourceFileOfNode(s.Declarations[0]).IsDeclarationFile {
			where = newLined(ast.GetSourceFileOfNode(s.Declarations[0])).where(s.Declarations[0])
		}
		return fmt.Sprintf("%s is already a name at %s (declared at %s); choose another", name, l.where(at), where)
	}
	return ""
}

// lineRange returns the bytes of the whole lines n is on, with the
// comments directly above it, or false when other code shares them.
func (l *lined) lineRange(n *ast.Node) (int, int, bool) {
	first, last := l.lines(n)
	if strings.TrimSpace(l.text[l.start(first):l.pos(n)]) != "" || !quiet(l.text[n.End():l.end(last)]) {
		return 0, 0, false
	}
	for first > 1 {
		above := strings.TrimSpace(l.text[l.start(first-1):l.end(first-1)])
		if above == "" || !quiet(above) || l.start(first-1) < n.Pos() {
			break
		}
		first--
	}
	return l.start(first), l.start(last + 1), true
}

// withBlank widens the whole lines [a, b) by a blank line after them, or
// else before them, so that removing them leaves no two blank lines.
func (l *lined) withBlank(a, b int) (int, int) {
	blank := func(line int) bool {
		return line >= 1 && line <= l.count() && strings.TrimSpace(l.text[l.start(line):l.end(line)]) == ""
	}
	switch first, last := l.line(a), l.line(b-1); {
	case blank(last + 1):
		return a, l.start(last + 2)
	case blank(first - 1):
		return l.start(first - 1), b
	}
	return a, b
}

// reindent returns the text of the whole lines [start, end) with the
// indentation from replaced by to, but for lines inside a string or
// template that spans lines.
func (l *lined) reindent(start, end int, from, to string, rs ...repl) string {
	inside := map[int]bool{}
	var visit func(n *ast.Node) bool
	visit = func(n *ast.Node) bool {
		if n.End() <= start || n.Pos() >= end {
			return false
		}
		if ast.IsTemplateLiteralKind(n.Kind) || ast.IsTemplateExpression(n) || ast.IsStringLiteral(n) {
			a, b := l.lines(n)
			for i := a + 1; i <= b; i++ {
				inside[i] = true
			}
			return false
		}
		n.ForEachChild(visit)
		return false
	}
	visit(l.f.AsNode())
	var b strings.Builder
	for line := l.line(start); line <= l.count() && l.start(line) < end; line++ {
		s := l.substitute(l.start(line), min(l.start(line+1), end), rs)
		if !inside[line] {
			if strings.HasPrefix(s, from) {
				s = to + s[len(from):]
			} else if strings.TrimSpace(s) == "" {
				s = strings.TrimLeft(s, " \t")
			}
		}
		b.WriteString(s)
	}
	return b.String()
}

// declared returns the names stmt declares in its block: variables,
// functions, classes and enums.
func declared(stmt *ast.Node) []*ast.Node {
	var out []*ast.Node
	switch {
	case ast.IsVariableStatement(stmt):
		for _, d := range stmt.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes {
			identifiers(d.Name(), func(id *ast.Node) {
				if !(ast.IsBindingElement(id.Parent) && id.Parent.AsBindingElement().PropertyName == id) {
					out = append(out, id)
				}
			})
		}
	case ast.IsFunctionDeclaration(stmt), ast.IsClassDeclaration(stmt), ast.IsEnumDeclaration(stmt):
		if stmt.Name() != nil {
			out = append(out, stmt.Name())
		}
	}
	return out
}

// uses returns the first identifier in n that refers to one of syms.
func uses(c *checker.Checker, n *ast.Node, syms []*ast.Symbol) *ast.Node {
	var found *ast.Node
	identifiers(n, func(id *ast.Node) {
		if found == nil && !ast.IsDeclarationName(id) && slices.Contains(syms, symbolOf(c, id)) {
			found = id
		}
	})
	return found
}

// symbols returns the symbols of the names.
func symbols(c *checker.Checker, names []*ast.Node) []*ast.Symbol {
	var out []*ast.Symbol
	for _, n := range names {
		if s := c.GetSymbolAtLocation(n); s != nil {
			out = append(out, s)
		}
	}
	return out
}

// ExtractVariable returns the edits that name the expression written expr
// that starts on line (at column, when given): const name = expr; just
// before its statement, and name in its place; with all, also in place of
// the same expression later in the same block.
func (p *Project) ExtractVariable(ctx context.Context, path string, line int, expr string, column int, name string, all bool) ([]Edit, string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, why
	}
	e, why := l.expressionAt(line, expr, column)
	if why != "" {
		return nil, why
	}
	c, done := p.prog.GetProgram().GetTypeCheckerForFile(ctx, l.f)
	defer done()
	src := strings.TrimSpace(expr)
	par := e.Parent
	switch {
	case ast.IsBinaryExpression(par) && ast.IsAssignmentOperator(par.AsBinaryExpression().OperatorToken.Kind) && par.AsBinaryExpression().Left == e,
		ast.IsPrefixUnaryExpression(par) && isUpdate(par.AsPrefixUnaryExpression().Operator), ast.IsPostfixUnaryExpression(par), ast.IsDeleteExpression(par):
		return nil, fmt.Sprintf("%s is assigned to at %s; a variable would take the assignment instead", src, l.where(e))
	case (ast.IsCallExpression(par) || ast.IsTaggedTemplateExpression(par)) && par.Expression() == e && (ast.IsPropertyAccessExpression(e) || ast.IsElementAccessExpression(e)):
		return nil, fmt.Sprintf("%s is the method of a call; called through a variable it would lose its this", src)
	case e.Flags&ast.NodeFlagsOptionalChain != 0 && par.Flags&ast.NodeFlagsOptionalChain != 0 && par.Expression() == e:
		return nil, fmt.Sprintf("%s is part of the optional chain %s, which a variable would cut", src, l.source(par))
	case ast.IsSpreadElement(e), ast.IsOmittedExpression(e), e.Kind == ast.KindSuperKeyword:
		return nil, fmt.Sprintf("%s is not a value of its own", src)
	}
	stmt := stmtOf(e)
	if stmt == nil {
		return nil, fmt.Sprintf("%s is in no statement of a block (an arrow's expression body, a parameter's default or a class field); give it a block first", src)
	}
	ef := newEffects(c, l)
	times, place, why := l.timesIn(e, stmt)
	if why != "" {
		return nil, fmt.Sprintf("%s cannot be evaluated before its statement: %s", src, why)
	}
	past := parts(stmt, l.pos(e), nil)
	if times == Shared {
		past = []*ast.Node{stmt} // the loop's other parts run between evaluations
	}
	if why := ef.Check(Move{Expr: e, Past: past, Times: times}); why != "" {
		if place != "" {
			why += " (in " + place + ")"
		}
		return nil, why
	}
	if why := seenAlike(c, l, free(c, e), stmt); why != "" {
		return nil, fmt.Sprintf("%s cannot move before its statement: %s", src, why)
	}
	if why := taken(c, l, name, stmt); why != "" {
		return nil, why
	}
	targets := []*ast.Node{e}
	if all {
		list := stmt.Parent.Statements()
		i := slices.Index(list, stmt)
		want, meaning := tokens(l.source(e)), symbolsOf(c, e)
		l.expressions(func(n *ast.Node) {
			if n.Pos() < e.End() || n.Kind != e.Kind || !slices.Equal(tokens(l.source(n)), want) || !slices.Equal(symbolsOf(c, n), meaning) {
				return
			}
			if slices.ContainsFunc(targets, func(t *ast.Node) bool { return t.Pos() <= n.Pos() && n.End() <= t.End() }) {
				return
			}
			s := stmtOf(n)
			if s == nil || s.Parent != stmt.Parent {
				return
			}
			if t, _, why := l.timesIn(n, s); why != "" || t != Once {
				return
			}
			j := slices.Index(list, s)
			past := slices.Clone(list[i:j])
			if j == i {
				past = nil
			}
			var skip []*ast.Node
			for _, t := range targets {
				if t.Pos() >= list[i].Pos() {
					skip = append(skip, t)
				}
			}
			var between []*ast.Node
			for _, x := range past {
				between = append(between, parts(x, x.End(), skip)...)
			}
			between = append(between, parts(s, l.pos(n), skip)...)
			if ef.Check(Move{Expr: n, Past: between, Times: Shared}) == "" {
				targets = append(targets, n)
			}
		})
	}
	var edits []Edit
	indent := l.indent(l.line(l.pos(stmt)))
	decl := "const " + name + " = " + l.source(e)
	if at := l.start(l.line(l.pos(stmt))); strings.TrimSpace(l.text[at:l.pos(stmt)]) == "" {
		edits = append(edits, Edit{File: l.f.FileName(), Start: at, End: at, NewText: indent + decl + l.semi() + "\n"})
	} else {
		edits = append(edits, Edit{File: l.f.FileName(), Start: l.pos(stmt), End: l.pos(stmt), NewText: decl + "; "})
	}
	for _, t := range targets {
		text := name
		if t.Parent != nil && ast.IsShorthandPropertyAssignment(t.Parent) {
			text = t.Text() + ": " + name
		}
		edits = append(edits, Edit{File: l.f.FileName(), Start: l.pos(t), End: t.End(), NewText: text})
	}
	return edits, ""
}

// expressionAt returns the expression written expr that starts on line,
// at column when it is given.
func (l *lined) expressionAt(line int, expr string, column int) (*ast.Node, string) {
	if line < 1 || line > l.count() {
		return nil, fmt.Sprintf("line %d out of range: the file has %d lines", line, l.count())
	}
	want := tokens(expr)
	if len(want) == 0 {
		return nil, "expression is empty"
	}
	var found []*ast.Node
	l.expressions(func(n *ast.Node) {
		start := l.pos(n)
		if l.line(start) == line && (column == 0 || start-l.start(line)+1 == column) && slices.Equal(tokens(l.source(n)), want) &&
			!slices.ContainsFunc(found, func(f *ast.Node) bool { return l.pos(f) == start }) {
			found = append(found, n)
		}
	})
	identifiers(l.f.AsNode(), func(id *ast.Node) { // a shorthand property's value: { name }
		start := l.pos(id)
		if ast.IsShorthandPropertyAssignment(id.Parent) && l.line(start) == line && (column == 0 || start-l.start(line)+1 == column) && slices.Equal(tokens(id.Text()), want) {
			found = append(found, id)
		}
	})
	switch len(found) {
	case 0:
		return nil, fmt.Sprintf("no expression %s starts on line %d: %s", strings.TrimSpace(expr), line, strings.TrimSpace(l.text[l.start(line):l.end(line)]))
	case 1:
		return found[0], ""
	}
	var cols []string
	for _, f := range found {
		cols = append(cols, fmt.Sprint(l.pos(f)-l.start(line)+1))
	}
	return nil, fmt.Sprintf("%s starts on line %d %d times; give column (%s)", strings.TrimSpace(expr), line, len(found), strings.Join(cols, ", "))
}

// InlineVariable returns the edits that replace every use of the variable
// name with its value and delete its declaration. The variable is the one
// named on line (at column, when given), or declared in function.
func (p *Project) InlineVariable(ctx context.Context, path string, line int, function, name string, column int) ([]Edit, string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, why
	}
	c, done := p.prog.GetProgram().GetTypeCheckerForFile(ctx, l.f)
	defer done()
	var sym *ast.Symbol
	switch {
	case function != "":
		fn, _, _, why := l.function(function)
		if why != "" {
			return nil, why
		}
		identifiers(fn, func(id *ast.Node) {
			if sym == nil && id.Text() == name && ast.IsVariableDeclaration(id.Parent) && id.Parent.Name() == id {
				sym = c.GetSymbolAtLocation(id)
			}
		})
		if sym == nil {
			return nil, fmt.Sprintf("%s declares no variable %s", function, name)
		}
	case line >= 1 && line <= l.count():
		var found []*ast.Symbol
		identifiers(l.f.AsNode(), func(id *ast.Node) {
			start := l.pos(id)
			if id.Text() == name && !propertyName(id) && l.line(start) == line && (column == 0 || start-l.start(line)+1 == column) {
				if s := symbolOf(c, id); s != nil && !slices.Contains(found, s) {
					found = append(found, s)
				}
			}
		})
		switch len(found) {
		case 0:
			return nil, fmt.Sprintf("no name %s on line %d: %s", name, line, strings.TrimSpace(l.text[l.start(line):l.end(line)]))
		case 1:
			sym = found[0]
		default:
			return nil, fmt.Sprintf("%s on line %d means %d things; give column", name, line, len(found))
		}
	default:
		return nil, "give line, or function"
	}
	d := sym.ValueDeclaration
	if d == nil || !ast.IsVariableDeclaration(d) || ast.GetSourceFileOfNode(d) != l.f {
		return nil, fmt.Sprintf("%s is not a variable declared in this file", name)
	}
	init := d.Initializer()
	list := d.Parent
	stmt := list.Parent
	switch {
	case !ast.IsIdentifier(d.Name()):
		return nil, fmt.Sprintf("%s is declared by a pattern at %s", name, l.where(d))
	case init == nil:
		return nil, fmt.Sprintf("%s has no value where it is declared (%s)", name, l.where(d))
	case !ast.IsVariableStatement(stmt) || !inList(stmt):
		return nil, fmt.Sprintf("%s is declared in %s, not by a statement of its own", name, l.part(stmt))
	case ast.HasSyntacticModifier(stmt, ast.ModifierFlagsExport):
		return nil, fmt.Sprintf("%s is exported; other modules may use it", name)
	}
	scope := stmt.Parent
	if where := l.assigns(scope, sym, c); where != "" {
		return nil, fmt.Sprintf("%s is assigned at %s after its declaration", name, where)
	}
	var refs []*ast.Node
	identifiers(scope, func(id *ast.Node) {
		if id != d.Name() && symbolOf(c, id) == sym {
			refs = append(refs, id)
		}
	})
	ef := newEffects(c, l)
	ie := ef.Of(init)
	idx := slices.Index(scope.Statements(), stmt)
	src := l.source(init)
	edits := []Edit{l.dropDeclaration(d)}
	if len(refs) == 0 && !ie.CanDrop() {
		return nil, fmt.Sprintf("nothing uses %s, but its value %s would no longer be evaluated: %s", name, src, ie.Why)
	}
	for _, r := range refs {
		if r.Pos() < stmt.End() {
			return nil, fmt.Sprintf("%s is used at %s, before or inside its declaration", name, l.where(r))
		}
		// What lies between the declaration and the use.
		var at *ast.Node
		for x := r; x != nil; x = x.Parent {
			if x.Parent == scope {
				at = x
				break
			}
		}
		times, place, why := l.timesIn(r, at)
		if why != "" {
			if !ie.CanRepeat() || ie.Reads || len(ie.Locals) > 0 {
				return nil, fmt.Sprintf("%s is used at %s, and %s, so %s would be evaluated then", name, l.where(r), why, src)
			}
			times = Several
		} else {
			past := slices.Clone(scope.Statements()[idx+1 : slices.Index(scope.Statements(), at)])
			if times == Shared {
				past = append(past, at) // the loop runs its other parts between evaluations
			} else {
				past = append(past, parts(at, l.pos(r), nil)...)
			}
			if len(refs) > 1 {
				times = Several
			} else if times == Shared {
				times = Several
			}
			if why := ef.Check(Move{Expr: init, Past: past, Times: times}); why != "" {
				if place != "" {
					why += " (the use at " + l.where(r) + " is in " + place + ")"
				}
				return nil, why
			}
		}
		if why := seenAlike(c, l, free(c, init), r); why != "" {
			return nil, fmt.Sprintf("%s cannot be used at %s: %s", src, l.where(r), why)
		}
		text := src
		if ast.IsShorthandPropertyAssignment(r.Parent) {
			text = name + ": " + src
		} else if needsParens(r, init) {
			text = "(" + src + ")"
		}
		edits = append(edits, Edit{File: l.f.FileName(), Start: l.pos(r), End: r.End(), NewText: text})
	}
	return edits, ""
}

// dropDeclaration returns the edit that removes the declaration d: its
// statement, with the lines it is on, when it is the only one there.
func (l *lined) dropDeclaration(d *ast.Node) Edit {
	list := d.Parent.AsVariableDeclarationList().Declarations
	stmt := d.Parent.Parent
	if len(list.Nodes) > 1 {
		return l.removeAt(list, slices.Index(list.Nodes, d))
	}
	if a, b, ok := l.lineRange(stmt); ok {
		return Edit{File: l.f.FileName(), Start: a, End: b}
	}
	return Edit{File: l.f.FileName(), Start: l.pos(stmt), End: stmt.End()}
}

// MoveStatement returns the edits that swap the statement starting on
// line with the one after it (down) or before it, each with the comments
// directly above it.
func (p *Project) MoveStatement(ctx context.Context, path string, line int, down bool) ([]Edit, string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, why
	}
	if line < 1 || line > l.count() {
		return nil, fmt.Sprintf("line %d out of range: the file has %d lines", line, l.count())
	}
	var stmt *ast.Node
	l.lists(func(_ *ast.Node, list []*ast.Node) {
		for _, s := range list {
			if stmt == nil && l.line(l.pos(s)) == line {
				stmt = s
			}
		}
	})
	if stmt == nil {
		return nil, fmt.Sprintf("no statement starts on line %d: %s", line, strings.TrimSpace(l.text[l.start(line):l.end(line)]))
	}
	list := stmt.Parent.Statements()
	i := slices.Index(list, stmt)
	j := i - 1
	if down {
		j = i + 1
	}
	if j < 0 || j >= len(list) {
		return nil, fmt.Sprintf("%s is the %s statement of its block", l.part(stmt), map[bool]string{true: "last", false: "first"}[down])
	}
	other := list[j]
	c, done := p.prog.GetProgram().GetTypeCheckerForFile(ctx, l.f)
	defer done()
	first, second := other, stmt // in their order now
	if down {
		first, second = stmt, other
	}
	// What the first declares, the second may not use once before it;
	// a function declaration is hoisted, so it may.
	if !ast.IsFunctionDeclaration(first) {
		if u := uses(c, second, symbols(c, declared(first))); u != nil {
			return nil, fmt.Sprintf("%s uses %s, which %s declares", l.part(second), u.Text(), l.part(first))
		}
	}
	if !ast.IsFunctionDeclaration(second) {
		if u := uses(c, first, symbols(c, declared(second))); u != nil {
			return nil, fmt.Sprintf("%s uses %s, which %s declares", l.part(first), u.Text(), l.part(second))
		}
	}
	if why := newEffects(c, l).Check(Move{Expr: stmt, Past: []*ast.Node{other}}); why != "" {
		return nil, why
	}
	a0, a1, ok1 := l.lineRange(first)
	b0, b1, ok2 := l.lineRange(second)
	if !ok1 || !ok2 {
		return nil, fmt.Sprintf("%s or %s shares its lines with other code", l.part(first), l.part(second))
	}
	text := l.text[b0:b1] + l.text[a1:b0] + l.text[a0:a1]
	return []Edit{{File: l.f.FileName(), Start: a0, End: b1, NewText: text}}, ""
}

// ifAt returns the if statement whose keyword is on line, at column when
// given.
func (l *lined) ifAt(line, column int) (*ast.Node, string) {
	if line < 1 || line > l.count() {
		return nil, fmt.Sprintf("line %d out of range: the file has %d lines", line, l.count())
	}
	var found []*ast.Node
	var visit func(n *ast.Node) bool
	visit = func(n *ast.Node) bool {
		if ast.IsIfStatement(n) && l.line(l.pos(n)) == line && (column == 0 || l.pos(n)-l.start(line)+1 == column) {
			found = append(found, n)
		}
		n.ForEachChild(visit)
		return false
	}
	visit(l.f.AsNode())
	switch len(found) {
	case 0:
		return nil, fmt.Sprintf("no if starts on line %d: %s", line, strings.TrimSpace(l.text[l.start(line):l.end(line)]))
	case 1:
		return found[0], ""
	}
	return nil, fmt.Sprintf("line %d has %d ifs; give column", line, len(found))
}

// terminates reports whether running s never goes on to what follows it:
// it ends with a return, throw, break or continue on every path.
func terminates(s *ast.Node) bool {
	switch {
	case s == nil:
		return false
	case ast.IsReturnStatement(s), ast.IsThrowStatement(s), ast.IsBreakOrContinueStatement(s):
		return true
	case ast.IsBlock(s):
		st := s.Statements()
		return len(st) > 0 && terminates(st[len(st)-1])
	case ast.IsIfStatement(s):
		is := s.AsIfStatement()
		return terminates(is.ThenStatement) && terminates(is.ElseStatement)
	}
	return false
}

// unnest returns the statements of branch, a block or one statement, as
// text re-indented to the if's own indentation; or why they cannot leave
// it: a name they declare is seen at the if already.
func (l *lined) unnest(c *checker.Checker, ifs, branch *ast.Node) (string, string) {
	indent := l.indent(l.line(l.pos(ifs)))
	stmts := []*ast.Node{branch}
	if ast.IsBlock(branch) {
		stmts = branch.Statements()
	}
	if len(stmts) == 0 {
		return "", ""
	}
	for _, s := range stmts {
		for _, n := range declared(s) {
			if r := c.ResolveName(n.Text(), ifs, ast.SymbolFlagsValue, false); r != nil {
				return "", fmt.Sprintf("%s, declared at %s, would then be seen where %s already is", n.Text(), l.where(n), n.Text())
			}
		}
	}
	first, last := stmts[0], stmts[len(stmts)-1]
	start := l.start(l.line(l.pos(first)))
	if strings.TrimSpace(l.text[start:l.pos(first)]) != "" {
		return indent + l.source(first), "" // on the else's line: if (c) {...} else foo();
	}
	end := l.end(l.line(last.End() - 1))
	return strings.TrimRight(l.reindent(start, end, l.indent(l.line(l.pos(first))), indent), "\n"), ""
}

// negate returns the source of the negation of e, simplified where that
// is exact, with whether it is an || at its top.
func (l *lined) negate(c *checker.Checker, e *ast.Node) (string, bool) {
	for ast.IsParenthesizedExpression(e) {
		e = e.Expression()
	}
	switch {
	case ast.IsPrefixUnaryExpression(e) && e.AsPrefixUnaryExpression().Operator == ast.KindExclamationToken:
		inner := e.AsPrefixUnaryExpression().Operand
		for ast.IsParenthesizedExpression(inner) {
			inner = inner.Expression()
		}
		return l.source(inner), ast.IsBinaryExpression(inner)
	case e.Kind == ast.KindTrueKeyword:
		return "false", false
	case e.Kind == ast.KindFalseKeyword:
		return "true", false
	case ast.IsBinaryExpression(e):
		b := e.AsBinaryExpression()
		left, right := l.source(b.Left), l.source(b.Right)
		swap := map[ast.Kind]string{ast.KindEqualsEqualsEqualsToken: "!==", ast.KindExclamationEqualsEqualsToken: "===",
			ast.KindEqualsEqualsToken: "!=", ast.KindExclamationEqualsToken: "=="}
		ordered := map[ast.Kind]string{ast.KindLessThanToken: ">=", ast.KindGreaterThanEqualsToken: "<",
			ast.KindGreaterThanToken: "<=", ast.KindLessThanEqualsToken: ">"}
		op := b.OperatorToken.Kind
		if s, ok := swap[op]; ok {
			return left + " " + s + " " + right, false
		}
		if s, ok := ordered[op]; ok && l.ordered(c, b.Left) && l.ordered(c, b.Right) {
			return left + " " + s + " " + right, false
		}
		if op == ast.KindAmpersandAmpersandToken || op == ast.KindBarBarToken {
			nl, lor := l.negate(c, b.Left)
			nr, ror := l.negate(c, b.Right)
			if op == ast.KindBarBarToken { // !(a || b) is !a && !b
				if lor {
					nl = "(" + nl + ")"
				}
				if ror {
					nr = "(" + nr + ")"
				}
				return nl + " && " + nr, false
			}
			return nl + " || " + nr, true
		}
	}
	switch {
	case ast.IsIdentifier(e), ast.IsPropertyAccessExpression(e), ast.IsElementAccessExpression(e), ast.IsCallExpression(e), e.Kind == ast.KindThisKeyword:
		return "!" + l.source(e), false
	}
	return "!(" + l.source(e) + ")", false
}

// ordered reports whether every value of e's type is a string or a
// bigint, for which < and >= are each other's negation (a number may be
// NaN).
func (l *lined) ordered(c *checker.Checker, e *ast.Node) bool {
	t := c.GetTypeAtLocation(e)
	if t == nil {
		return false
	}
	types := []*checker.Type{t}
	if t.Flags()&checker.TypeFlagsUnion != 0 {
		types = t.Types()
	}
	for _, u := range types {
		if u.Flags()&(checker.TypeFlagsStringLike|checker.TypeFlagsBigIntLike) == 0 {
			return false
		}
	}
	return true
}

// InvertIf returns the edits that negate the condition of the if on line
// and swap its branches; when the new first branch cannot fall through,
// the other follows the if un-nested, unless keepElse. An if without else
// is un-nested when it is the last statement of a function's body
// (return) or a loop's (continue).
func (p *Project) InvertIf(ctx context.Context, path string, line, column int, keepElse bool) ([]Edit, string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, why
	}
	ifs, why := l.ifAt(line, column)
	if why != "" {
		return nil, why
	}
	c, done := p.prog.GetProgram().GetTypeCheckerForFile(ctx, l.f)
	defer done()
	is := ifs.AsIfStatement()
	cond, _ := l.negate(c, is.Expression)
	head := "if (" + cond + ") "
	then, els := is.ThenStatement, is.ElseStatement
	indent := l.indent(l.line(l.pos(ifs)))
	edit := func(text string) []Edit {
		return []Edit{{File: l.f.FileName(), Start: l.pos(ifs), End: ifs.End(), NewText: text}}
	}
	if els == nil {
		jump := ""
		owner := ifs.Parent
		switch {
		case !inList(ifs):
		case ast.IsBlock(owner) && ast.IsFunctionLike(owner.Parent):
			jump = "return" + l.semi()
		case ast.IsBlock(owner) && ast.IsIterationStatement(owner.Parent, false):
			jump = "continue" + l.semi()
		}
		if list := owner.Statements(); inList(ifs) && list[len(list)-1] != ifs {
			// if (c) { T, which leaves } R becomes if (!c) { R } T: R
			// must then leave too, or end the body it is the end of.
			return l.invertBefore(c, ifs, head, jump)
		}
		if jump == "" {
			return nil, "an if without else is inverted only as the last statement of a function's body or a loop's (where it becomes a guard: return or continue), or when what follows it leaves"
		}
		rest, why := l.unnest(c, ifs, then)
		if why != "" {
			return nil, why
		}
		text := head + "{\n" + indent + l.unit() + jump + "\n" + indent + "}"
		if rest != "" {
			text += "\n" + rest
		}
		return edit(text), ""
	}
	if ast.IsIfStatement(els) {
		return nil, fmt.Sprintf("the else is another if (%s); invert that one, or give it braces first", l.part(els))
	}
	if keepElse || !terminates(els) {
		return edit(head + l.source(els) + " else " + l.source(then)), ""
	}
	rest, why := l.unnest(c, ifs, then)
	if why != "" {
		return nil, why
	}
	text := head + l.source(els)
	if rest != "" {
		text += "\n" + rest
	}
	return edit(text), ""
}

// invertBefore returns the edits that invert ifs, which has no else and
// statements after it, R: its first branch T leaves, so if (c) { T } R
// becomes if (!c) { R } T. R must leave too, or end a function's or a
// loop's body, where jump ends it.
func (l *lined) invertBefore(c *checker.Checker, ifs *ast.Node, head, jump string) ([]Edit, string) {
	then := ifs.AsIfStatement().ThenStatement
	list := ifs.Parent.Statements()
	rest := list[slices.Index(list, ifs)+1:]
	if !terminates(then) {
		return nil, "an if without else is inverted, with what follows it as its new first branch, only when its branch ends with a return, throw, break or continue"
	}
	last := rest[len(rest)-1]
	if terminates(last) {
		jump = ""
	} else if jump == "" {
		return nil, fmt.Sprintf("what follows the if can fall through (%s), and it would then go on to the if's branch", l.part(last))
	}
	var syms []*ast.Symbol
	for _, s := range rest {
		syms = append(syms, symbols(c, declared(s))...)
	}
	if u := uses(c, then, syms); u != nil {
		return nil, fmt.Sprintf("the if's branch uses %s, which what follows it declares", u.Text())
	}
	indent := l.indent(l.line(l.pos(ifs)))
	unit := l.unit()
	a := l.start(l.line(l.pos(rest[0])))
	_, endLine := l.lines(last)
	if strings.TrimSpace(l.text[a:l.pos(rest[0])]) != "" || !quiet(l.text[last.End():l.end(endLine)]) {
		return nil, "what follows the if shares its lines with other code"
	}
	body := l.reindent(a, l.start(endLine+1), indent, indent+unit)
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	if jump != "" {
		body += indent + unit + jump + "\n"
	}
	unnested, why := l.unnest(c, ifs, then)
	if why != "" {
		return nil, why
	}
	text := head + "{\n" + body + indent + "}"
	if unnested != "" {
		text += "\n" + unnested
	}
	return []Edit{{File: l.f.FileName(), Start: l.pos(ifs), End: l.end(endLine), NewText: text}}, ""
}

// RemoveElse returns the edits that un-nest the else of the if on line,
// whose first branch cannot fall through: its statements follow the if.
func (p *Project) RemoveElse(ctx context.Context, path string, line, column int) ([]Edit, string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, why
	}
	ifs, why := l.ifAt(line, column)
	if why != "" {
		return nil, why
	}
	is := ifs.AsIfStatement()
	switch {
	case is.ElseStatement == nil:
		return nil, "the if has no else"
	case !terminates(is.ThenStatement):
		return nil, fmt.Sprintf("the if's first branch can fall through to what follows it (it does not end with a return, throw, break or continue on every path); %s", map[bool]string{true: "the else does, so use invert_if", false: "neither branch can be un-nested"}[terminates(is.ElseStatement)])
	case !inList(ifs):
		return nil, fmt.Sprintf("the if is %s's only statement; give it braces first", l.part(ifs.Parent))
	}
	c, done := p.prog.GetProgram().GetTypeCheckerForFile(ctx, l.f)
	defer done()
	els := is.ElseStatement
	var rest, why2 string
	if ast.IsIfStatement(els) {
		rest = l.indent(l.line(l.pos(ifs))) + l.source(els)
	} else {
		rest, why2 = l.unnest(c, ifs, els)
	}
	if why2 != "" {
		return nil, why2
	}
	text := l.source(is.ThenStatement)
	if rest != "" {
		text += "\n" + rest
	}
	return []Edit{{File: l.f.FileName(), Start: l.pos(is.ThenStatement), End: ifs.End(), NewText: text}}, ""
}

// WrapElse returns the edits that make the statements after the if on
// line (at column, when given), whose first branch cannot fall through,
// its else branch: the inverse of RemoveElse.
func (p *Project) WrapElse(ctx context.Context, path string, line, column int) ([]Edit, string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, why
	}
	ifs, why := l.ifAt(line, column)
	if why != "" {
		return nil, why
	}
	is := ifs.AsIfStatement()
	switch {
	case is.ElseStatement != nil:
		return nil, "the if has an else already"
	case !terminates(is.ThenStatement):
		return nil, "the if's branch can fall through to what follows it (it does not end with a return, throw, break or continue on every path), so in an else that code would no longer run after it"
	case !inList(ifs):
		return nil, fmt.Sprintf("the if is %s's only statement, so nothing follows it", l.part(ifs.Parent))
	}
	list := ifs.Parent.Statements()
	rest := list[slices.Index(list, ifs)+1:]
	if len(rest) == 0 {
		return nil, "nothing follows the if in its block"
	}
	for _, s := range rest {
		if ast.IsFunctionDeclaration(s) || ast.IsClassDeclaration(s) || ast.IsVariableStatement(s) && s.AsVariableStatement().DeclarationList.Flags&ast.NodeFlagsBlockScoped == 0 {
			return nil, fmt.Sprintf("%s declares what the code before the if may use, which in the else it could not", l.where(s))
		}
	}
	f := l.f.FileName()
	edits := []Edit{{File: f, Start: is.ThenStatement.End(), End: is.ThenStatement.End(), NewText: " else {"}}
	end := rest[len(rest)-1].End()
	for n := l.line(l.pos(rest[0])); n <= l.line(end); n++ {
		edits = append(edits, Edit{File: f, Start: l.start(n), End: l.start(n), NewText: l.unit()})
	}
	edits = append(edits, Edit{File: f, Start: end, End: end, NewText: "\n" + l.indent(l.line(l.pos(ifs))) + "}"})
	return edits, ""
}
