package tsapi

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/checker"
	"github.com/buke/typescript-go-internal/v7/pkg/scanner"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
)

// This file has what the refactorings that simplify code share: where a
// node's statement is, what runs before a node in it, and how often.

// InList reports whether n is a statement of a statement list.
func InList(n *ast.Node) bool {
	return n.Parent != nil && n.Parent.CanHaveStatements() && slices.Contains(n.Parent.Statements(), n)
}

// stmtOf returns the statement of a statement list that holds n, or nil
// when a function lies between them.
func stmtOf(n *ast.Node) *ast.Node {
	for x := n; x != nil; x = x.Parent {
		if InList(x) {
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
// function lies between them, unless during (which may be nil) says it runs
// only during the call it is passed to: then Shared, as in a loop.
func (l *Lined) timesIn(n, stmt *ast.Node, during func(*ast.Node) bool) (Times, string, string) {
	times, why := Once, ""
	for x := n; x != stmt && x.Parent != nil; x = x.Parent {
		p := x.Parent
		maybe := false
		switch {
		case ast.IsFunctionLike(p) && during != nil && during(p):
			times, why = Shared, l.part(p)
			continue
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
func (l *Lined) part(n *ast.Node) string {
	src := l.Source(n)
	if i := strings.IndexByte(src, '\n'); i >= 0 {
		src = strings.TrimSpace(src[:i]) + " ..."
	}
	return fmt.Sprintf("%s at %s", src, l.Where(n))
}

// parts returns the largest pieces of root, statements or expressions,
// that run before the offset until and overlap none of skip: what runs
// in root before the node there. A name that = assigns to is left out,
// since it is not read; a property read of a skipped object is kept
// whole, since it reads state though its object is skipped.
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
			case (ast.IsPropertyAccessExpression(c) || ast.IsElementAccessExpression(c)) && slices.Contains(skip, c.Expression()) && c.End() <= until &&
				!(ast.IsBinaryExpression(n) && n.AsBinaryExpression().OperatorToken.Kind == ast.KindEqualsToken && n.AsBinaryExpression().Left == c):
				// a read of a skipped object's property still reads state
				out = append(out, c)
			case holds:
				if !ast.IsFunctionLike(c) {
					visit(c)
				}
			case ast.IsIdentifier(c) && ast.IsBinaryExpression(n) && n.AsBinaryExpression().OperatorToken.Kind == ast.KindEqualsToken && n.AsBinaryExpression().Left == c:
				// a name assigned to is not read before the value
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

// Identifiers calls fn with each identifier in n, n included.
func Identifiers(n *ast.Node, fn func(id *ast.Node)) {
	Inspect(n, func(x *ast.Node) bool {
		if ast.IsIdentifier(x) {
			fn(x)
		}
		return true
	})
}

// free returns the identifiers of e that refer to values declared outside
// it, leaving out property names.
func free(c *checker.Checker, e *ast.Node) []*ast.Node {
	var out []*ast.Node
	Identifiers(e, func(id *ast.Node) {
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
func seenAlike(c *checker.Checker, l *Lined, ids []*ast.Node, at *ast.Node) string {
	for _, id := range ids {
		s := symbolOf(c, id)
		if r := c.ResolveName(id.Text(), at, ast.SymbolFlagsValue, false); r == nil || c.SkipAlias(r) != s {
			return fmt.Sprintf("%s at %s would mean something else at %s", id.Text(), newLined(ast.GetSourceFileOfNode(id)).Where(id), l.Where(at))
		}
	}
	return ""
}

// taken returns why name cannot be declared where at is: something of
// that name is already seen there.
func taken(c *checker.Checker, l *Lined, name string, at *ast.Node) string {
	if !scanner.IsIdentifierText(name, 0) {
		return fmt.Sprintf("%q is not a valid name", name)
	}
	if s := c.ResolveName(name, at, ast.SymbolFlagsValue|ast.SymbolFlagsType|ast.SymbolFlagsNamespace, false); s != nil {
		where := "the standard library"
		if d := s.ValueDeclaration; d != nil && !ast.GetSourceFileOfNode(d).IsDeclarationFile {
			where = newLined(ast.GetSourceFileOfNode(d)).Where(d)
		} else if len(s.Declarations) > 0 && !ast.GetSourceFileOfNode(s.Declarations[0]).IsDeclarationFile {
			where = newLined(ast.GetSourceFileOfNode(s.Declarations[0])).Where(s.Declarations[0])
		}
		return fmt.Sprintf("%s is already a name at %s (declared at %s); choose another", name, l.Where(at), where)
	}
	return ""
}

// lineRange returns the bytes of the whole lines n is on, with the
// comments directly above it, or false when other code shares them.
func (l *Lined) lineRange(n *ast.Node) (int, int, bool) {
	first, last := l.Lines(n)
	if strings.TrimSpace(l.Text[l.Start(first):l.Pos(n)]) != "" || !quiet(l.Text[n.End():l.End(last)]) {
		return 0, 0, false
	}
	for first > 1 {
		above := strings.TrimSpace(l.Text[l.Start(first-1):l.End(first-1)])
		if above == "" || !quiet(above) || l.Start(first-1) < n.Pos() {
			break
		}
		first--
	}
	return l.Start(first), l.Start(last + 1), true
}

// withBlank widens the whole lines [a, b) by a blank line after them, or
// else before them, so that removing them leaves no two blank lines.
func (l *Lined) withBlank(a, b int) (int, int) {
	blank := func(line int) bool {
		return line >= 1 && line <= l.Count() && strings.TrimSpace(l.Text[l.Start(line):l.End(line)]) == ""
	}
	switch first, last := l.Line(a), l.Line(b-1); {
	case blank(last + 1):
		return a, l.Start(last + 2)
	case blank(first - 1):
		return l.Start(first - 1), b
	}
	return a, b
}

// reindent returns the text of the whole lines [start, end) with the
// indentation from replaced by to, but for lines inside a string or
// template that spans lines.
func (l *Lined) reindent(start, end int, from, to string, rs ...repl) string {
	inside := l.literalLines(start, end)
	var b strings.Builder
	for line := l.Line(start); line <= l.Count() && l.Start(line) < end; line++ {
		s := l.substitute(l.Start(line), min(l.Start(line+1), end), rs)
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

// literalLines is the lines in [start, end) that start inside a string or
// template spanning lines: changing their indentation changes the string.
func (l *Lined) literalLines(start int, end int) map[int]bool {
	inside := map[int]bool{}
	Inspect(l.File.AsNode(), func(n *ast.Node) bool {
		if n.End() <= start || n.Pos() >= end {
			return false
		}
		if ast.IsTemplateLiteralKind(n.Kind) || ast.IsTemplateExpression(n) || ast.IsStringLiteral(n) {
			a, b := l.Lines(n)
			for i := a + 1; i <= b; i++ {
				inside[i] = true
			}
			return false
		}
		return true
	})
	return inside
}

// Declared returns the names stmt declares in its block: variables,
// functions, classes and enums.
func Declared(stmt *ast.Node) []*ast.Node {
	var out []*ast.Node
	switch {
	case ast.IsVariableStatement(stmt):
		for _, d := range stmt.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes {
			Identifiers(d.Name(), func(id *ast.Node) {
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

// Uses returns the first identifier in n that refers to one of syms.
func Uses(c *checker.Checker, n *ast.Node, syms []*ast.Symbol) *ast.Node {
	var found *ast.Node
	Identifiers(n, func(id *ast.Node) {
		if found == nil && !ast.IsDeclarationName(id) && slices.Contains(syms, symbolOf(c, id)) {
			found = id
		}
	})
	return found
}

// Symbols returns the symbols of the names.
func Symbols(c *checker.Checker, names []*ast.Node) []*ast.Symbol {
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
	l, why := p.Lined(path)
	if why != "" {
		return nil, why
	}
	e, why := l.expressionAt(line, expr, column)
	if why != "" {
		return nil, why
	}
	c, done := p.prog.GetProgram().GetTypeCheckerForFile(ctx, l.File)
	defer done()
	src := strings.TrimSpace(expr)
	par := e.Parent
	switch {
	case ast.IsBinaryExpression(par) && ast.IsAssignmentOperator(par.AsBinaryExpression().OperatorToken.Kind) && par.AsBinaryExpression().Left == e,
		ast.IsPrefixUnaryExpression(par) && IsUpdate(par.AsPrefixUnaryExpression().Operator), ast.IsPostfixUnaryExpression(par), ast.IsDeleteExpression(par):
		return nil, fmt.Sprintf("%s is assigned to at %s; a variable would take the assignment instead", src, l.Where(e))
	case (ast.IsCallExpression(par) || ast.IsTaggedTemplateExpression(par)) && par.Expression() == e && (ast.IsPropertyAccessExpression(e) || ast.IsElementAccessExpression(e)):
		return nil, fmt.Sprintf("%s is the method of a call; called through a variable it would lose its this", src)
	case e.Flags&ast.NodeFlagsOptionalChain != 0 && par.Flags&ast.NodeFlagsOptionalChain != 0 && par.Expression() == e:
		return nil, fmt.Sprintf("%s is part of the optional chain %s, which a variable would cut", src, l.Source(par))
	case ast.IsSpreadElement(e), ast.IsOmittedExpression(e), e.Kind == ast.KindSuperKeyword:
		return nil, fmt.Sprintf("%s is not a value of its own", src)
	}
	stmt := stmtOf(e)
	if stmt == nil {
		return nil, fmt.Sprintf("%s is in no statement of a block (an arrow's expression body, a parameter's default or a class field); give it a block first", src)
	}
	ef := newEffects(c, l)
	times, place, why := l.timesIn(e, stmt, nil)
	if why != "" {
		return nil, fmt.Sprintf("%s cannot be evaluated before its statement: %s", src, why)
	}
	past := parts(stmt, l.Pos(e), nil)
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
		want, meaning := tokens(l.Source(e)), symbolsOf(c, e)
		l.expressions(func(n *ast.Node) {
			if n.Pos() < e.End() || n.Kind != e.Kind || !slices.Equal(tokens(l.Source(n)), want) || !slices.Equal(symbolsOf(c, n), meaning) {
				return
			}
			if slices.ContainsFunc(targets, func(t *ast.Node) bool { return t.Pos() <= n.Pos() && n.End() <= t.End() }) {
				return
			}
			s := stmtOf(n)
			if s == nil || s.Parent != stmt.Parent {
				return
			}
			if t, _, why := l.timesIn(n, s, nil); why != "" || t != Once {
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
			between = append(between, parts(s, l.Pos(n), skip)...)
			if ef.Check(Move{Expr: n, Past: between, Times: Shared}) == "" {
				targets = append(targets, n)
			}
		})
	}
	var edits []Edit
	indent := l.Indent(l.Line(l.Pos(stmt)))
	decl := "const " + name + " = " + typedFunction(c, l, e)
	if at := l.Start(l.Line(l.Pos(stmt))); strings.TrimSpace(l.Text[at:l.Pos(stmt)]) == "" {
		edits = append(edits, Edit{File: l.File.FileName(), Start: at, End: at, NewText: indent + decl + l.Semi() + "\n"})
	} else {
		edits = append(edits, Edit{File: l.File.FileName(), Start: l.Pos(stmt), End: l.Pos(stmt), NewText: decl + "; "})
	}
	for _, t := range targets {
		text := name
		if t.Parent != nil && ast.IsShorthandPropertyAssignment(t.Parent) {
			text = t.Text() + ": " + name
		}
		edits = append(edits, Edit{File: l.File.FileName(), Start: l.Pos(t), End: t.End(), NewText: text})
	}
	return edits, ""
}

// expressionAt returns the expression written expr that starts on line,
// at column when it is given.
func (l *Lined) expressionAt(line int, expr string, column int) (*ast.Node, string) {
	if line < 1 || line > l.Count() {
		return nil, fmt.Sprintf("line %d out of range: the file has %d lines", line, l.Count())
	}
	want := tokens(expr)
	if len(want) == 0 {
		return nil, "expression is empty"
	}
	var found []*ast.Node
	l.expressions(func(n *ast.Node) {
		start := l.Pos(n)
		if l.Line(start) == line && (column == 0 || start-l.Start(line)+1 == column) && slices.Equal(tokens(l.Source(n)), want) &&
			!slices.ContainsFunc(found, func(f *ast.Node) bool { return l.Pos(f) == start }) {
			found = append(found, n)
		}
	})
	Identifiers(l.File.AsNode(), func(id *ast.Node) { // a shorthand property's value: { name }
		start := l.Pos(id)
		if ast.IsShorthandPropertyAssignment(id.Parent) && l.Line(start) == line && (column == 0 || start-l.Start(line)+1 == column) && slices.Equal(tokens(id.Text()), want) {
			found = append(found, id)
		}
	})
	switch len(found) {
	case 0:
		return nil, fmt.Sprintf("no expression %s starts on line %d: %s", strings.TrimSpace(expr), line, strings.TrimSpace(l.Text[l.Start(line):l.End(line)]))
	case 1:
		return found[0], ""
	}
	var cols []string
	for _, f := range found {
		cols = append(cols, fmt.Sprint(l.Pos(f)-l.Start(line)+1))
	}
	return nil, fmt.Sprintf("%s starts on line %d %d times; give column (%s)", strings.TrimSpace(expr), line, len(found), strings.Join(cols, ", "))
}

// InlineVariable returns the edits that replace every use of the variable
// name with its value and delete its declaration. The variable is the one
// named on line (at column, when given), or declared in function.
func (p *Project) InlineVariable(ctx context.Context, path string, line int, function, name string, column int) ([]Edit, string) {
	l, why := p.Lined(path)
	if why != "" {
		return nil, why
	}
	c, done := p.prog.GetProgram().GetTypeCheckerForFile(ctx, l.File)
	defer done()
	var sym *ast.Symbol
	switch {
	case function != "":
		fn, _, _, why := l.function(function)
		if why != "" {
			return nil, why
		}
		Identifiers(fn, func(id *ast.Node) {
			if sym == nil && id.Text() == name && ast.IsVariableDeclaration(id.Parent) && id.Parent.Name() == id {
				sym = c.GetSymbolAtLocation(id)
			}
		})
		if sym == nil {
			return nil, fmt.Sprintf("%s declares no variable %s", function, name)
		}
	case line >= 1 && line <= l.Count():
		var found []*ast.Symbol
		Identifiers(l.File.AsNode(), func(id *ast.Node) {
			start := l.Pos(id)
			if id.Text() == name && !propertyName(id) && l.Line(start) == line && (column == 0 || start-l.Start(line)+1 == column) {
				if s := symbolOf(c, id); s != nil && !slices.Contains(found, s) {
					found = append(found, s)
				}
			}
		})
		switch len(found) {
		case 0:
			return nil, fmt.Sprintf("no name %s on line %d: %s", name, line, strings.TrimSpace(l.Text[l.Start(line):l.End(line)]))
		case 1:
			sym = found[0]
		default:
			return nil, fmt.Sprintf("%s on line %d means %d things; give column", name, line, len(found))
		}
	default:
		return nil, "give line, or function"
	}
	d := sym.ValueDeclaration
	if d == nil || !ast.IsVariableDeclaration(d) || ast.GetSourceFileOfNode(d) != l.File {
		return nil, fmt.Sprintf("%s is not a variable declared in this file", name)
	}
	init := d.Initializer()
	list := d.Parent
	stmt := list.Parent
	switch {
	case !ast.IsIdentifier(d.Name()):
		return nil, fmt.Sprintf("%s is declared by a pattern at %s", name, l.Where(d))
	case init == nil:
		return nil, fmt.Sprintf("%s has no value where it is declared (%s)", name, l.Where(d))
	case !ast.IsVariableStatement(stmt) || !InList(stmt):
		return nil, fmt.Sprintf("%s is declared in %s, not by a statement of its own", name, l.part(stmt))
	case ast.HasSyntacticModifier(stmt, ast.ModifierFlagsExport):
		return nil, fmt.Sprintf("%s is exported; other modules may use it", name)
	}
	scope := stmt.Parent
	if where := l.Assigns(scope, sym, c); where != "" {
		return nil, fmt.Sprintf("%s is assigned at %s after its declaration", name, where)
	}
	var refs []*ast.Node
	Identifiers(scope, func(id *ast.Node) {
		if id != d.Name() && symbolOf(c, id) == sym {
			refs = append(refs, id)
		}
	})
	ef := newEffects(c, l)
	ie := ef.Of(init)
	idx := slices.Index(scope.Statements(), stmt)
	src := l.Source(init)
	edits := []Edit{l.dropDeclaration(d)}
	if len(refs) == 0 && !ie.CanDrop() {
		return nil, fmt.Sprintf("nothing uses %s, but its value %s would no longer be evaluated: %s", name, src, ie.Why)
	}
	for _, r := range refs {
		if r.Pos() < stmt.End() {
			return nil, fmt.Sprintf("%s is used at %s, before or inside its declaration", name, l.Where(r))
		}
		// What lies between the declaration and the use.
		var at *ast.Node
		for x := r; x != nil; x = x.Parent {
			if x.Parent == scope {
				at = x
				break
			}
		}
		times, place, why := l.timesIn(r, at, ef.runsDuring)
		if why != "" {
			if !ie.CanRepeat() || ie.Reads || len(ie.Locals) > 0 {
				return nil, fmt.Sprintf("%s is used at %s, and %s, so %s would be evaluated then", name, l.Where(r), why, src)
			}
			times = Several
		} else {
			past := slices.Clone(scope.Statements()[idx+1 : slices.Index(scope.Statements(), at)])
			if times == Shared {
				past = append(past, at) // the loop runs its other parts between evaluations
			} else {
				past = append(past, parts(at, l.Pos(r), nil)...)
			}
			if len(refs) > 1 {
				times = Several
			} else if times == Shared {
				times = Several
			}
			if why := ef.Check(Move{Expr: init, Past: past, Times: times}); why != "" {
				if place != "" {
					why += " (the use at " + l.Where(r) + " is in " + place + ")"
				}
				return nil, why
			}
		}
		if why := seenAlike(c, l, free(c, init), r); why != "" {
			return nil, fmt.Sprintf("%s cannot be used at %s: %s", src, l.Where(r), why)
		}
		text := src
		if ast.IsShorthandPropertyAssignment(r.Parent) {
			text = name + ": " + src
		} else if needsParens(r, init) {
			text = "(" + src + ")"
		}
		edits = append(edits, Edit{File: l.File.FileName(), Start: l.Pos(r), End: r.End(), NewText: text})
	}
	return edits, ""
}

// dropDeclaration returns the edit that removes the declaration d: its
// statement, with the lines it is on, when it is the only one there.
func (l *Lined) dropDeclaration(d *ast.Node) Edit {
	list := d.Parent.AsVariableDeclarationList().Declarations
	stmt := d.Parent.Parent
	if len(list.Nodes) > 1 {
		return l.removeAt(list, slices.Index(list.Nodes, d))
	}
	if a, b, ok := l.lineRange(stmt); ok {
		return Edit{File: l.File.FileName(), Start: a, End: b}
	}
	return Edit{File: l.File.FileName(), Start: l.Pos(stmt), End: stmt.End()}
}

// MoveStatement returns the edits that swap the statement starting on
// line with the one after it (down) or before it, each with the comments
// directly above it. With pastTry, one of the two must be a try, and they
// swap whatever their side effects, and note is set to what that changes.
func (p *Project) MoveStatement(ctx context.Context, path string, line int, down, pastTry bool, note *string) ([]Edit, string) {
	l, why := p.Lined(path)
	if why != "" {
		return nil, why
	}
	if line < 1 || line > l.Count() {
		return nil, fmt.Sprintf("line %d out of range: the file has %d lines", line, l.Count())
	}
	var stmt *ast.Node
	l.lists(func(_ *ast.Node, list []*ast.Node) {
		for _, s := range list {
			if stmt == nil && l.Line(l.Pos(s)) == line {
				stmt = s
			}
		}
	})
	if stmt == nil {
		return nil, fmt.Sprintf("no statement starts on line %d: %s", line, strings.TrimSpace(l.Text[l.Start(line):l.End(line)]))
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
	c, done := p.prog.GetProgram().GetTypeCheckerForFile(ctx, l.File)
	defer done()
	first, second := other, stmt // in their order now
	if down {
		first, second = stmt, other
	}
	// What the first declares, the second may not use once before it;
	// a function declaration is hoisted, so it may.
	if !ast.IsFunctionDeclaration(first) {
		if u := Uses(c, second, Symbols(c, Declared(first))); u != nil {
			return nil, fmt.Sprintf("%s uses %s, which %s declares", l.part(second), u.Text(), l.part(first))
		}
	}
	if !ast.IsFunctionDeclaration(second) {
		if u := Uses(c, first, Symbols(c, Declared(second))); u != nil {
			return nil, fmt.Sprintf("%s uses %s, which %s declares", l.part(first), u.Text(), l.part(second))
		}
	}
	var aTry *ast.Node
	switch {
	case ast.IsTryStatement(first) && !ast.IsTryStatement(second):
		aTry = first
	case ast.IsTryStatement(second) && !ast.IsTryStatement(first):
		aTry = second
	}
	if pastTry && aTry == nil {
		return nil, "past_try applies when one of the two statements is a try and the other is not; leave it out here"
	}
	if pastTry {
		if first != aTry && isJump(first) || second != aTry && isJump(second) {
			return nil, "a return, throw, break or continue leaves the block where it is; move the statements around it instead"
		}
	}
	if !pastTry {
		if why := newEffects(c, l).Check(Move{Expr: stmt, Past: []*ast.Node{other}}); why != "" {
			return nil, why
		}
	}
	a0, a1, ok1 := l.lineRange(first)
	b0, b1, ok2 := l.lineRange(second)
	if !ok1 || !ok2 {
		return nil, fmt.Sprintf("%s or %s shares its lines with other code", l.part(first), l.part(second))
	}
	text := l.Text[b0:b1] + l.Text[a1:b0] + l.Text[a0:a1]
	if pastTry {
		*note = l.pastTryNote(aTry, aTry == second)
	}
	return []Edit{{File: l.File.FileName(), Start: a0, End: b1, NewText: text}}, ""
}

// ifAt returns the if statement whose keyword is on line, at column when
// given.
func (l *Lined) ifAt(line, column int) (*ast.Node, string) {
	if line < 1 || line > l.Count() {
		return nil, fmt.Sprintf("line %d out of range: the file has %d lines", line, l.Count())
	}
	var found []*ast.Node
	Inspect(l.File.AsNode(), func(n *ast.Node) bool {
		if ast.IsIfStatement(n) && l.Line(l.Pos(n)) == line && (column == 0 || l.Pos(n)-l.Start(line)+1 == column) {
			found = append(found, n)
		}
		return true
	})
	switch len(found) {
	case 0:
		return nil, fmt.Sprintf("no if starts on line %d: %s", line, strings.TrimSpace(l.Text[l.Start(line):l.End(line)]))
	case 1:
		return found[0], ""
	}
	return nil, fmt.Sprintf("line %d has %d ifs; give column", line, len(found))
}

// Terminates reports whether running s never goes on to what follows it:
// it ends with a return, throw, break or continue on every path.
func Terminates(s *ast.Node) bool {
	switch {
	case s == nil:
		return false
	case ast.IsReturnStatement(s), ast.IsThrowStatement(s), ast.IsBreakOrContinueStatement(s):
		return true
	case ast.IsBlock(s):
		st := s.Statements()
		return len(st) > 0 && Terminates(st[len(st)-1])
	case ast.IsIfStatement(s):
		is := s.AsIfStatement()
		return Terminates(is.ThenStatement) && Terminates(is.ElseStatement)
	}
	return false
}

// unnest returns the statements of branch, a block or one statement, as
// text re-indented to the if's own indentation; or why they cannot leave
// it: a name they declare is seen at the if already.
func (l *Lined) unnest(c *checker.Checker, ifs, branch *ast.Node) (string, string) {
	indent := l.Indent(l.Line(l.Pos(ifs)))
	stmts := []*ast.Node{branch}
	if ast.IsBlock(branch) {
		stmts = branch.Statements()
	}
	if len(stmts) == 0 {
		return "", ""
	}
	for _, s := range stmts {
		for _, n := range Declared(s) {
			if r := c.ResolveName(n.Text(), ifs, ast.SymbolFlagsValue, false); r != nil {
				return "", fmt.Sprintf("%s, declared at %s, would then be seen where %s already is", n.Text(), l.Where(n), n.Text())
			}
		}
	}
	first, last := stmts[0], stmts[len(stmts)-1]
	start := l.Start(l.Line(l.Pos(first)))
	if strings.TrimSpace(l.Text[start:l.Pos(first)]) != "" {
		return indent + l.Source(first), "" // on the else's line: if (c) {...} else foo();
	}
	end := l.End(l.Line(last.End() - 1))
	return strings.TrimRight(l.reindent(start, end, l.Indent(l.Line(l.Pos(first))), indent), "\n"), ""
}

// Negate returns the source of the negation of e, simplified where that
// is exact, with whether it is an || at its top.
func (l *Lined) Negate(c *checker.Checker, e *ast.Node) (string, bool) {
	for ast.IsParenthesizedExpression(e) {
		e = e.Expression()
	}
	switch {
	case ast.IsPrefixUnaryExpression(e) && e.AsPrefixUnaryExpression().Operator == ast.KindExclamationToken:
		inner := e.AsPrefixUnaryExpression().Operand
		for ast.IsParenthesizedExpression(inner) {
			inner = inner.Expression()
		}
		return l.Source(inner), ast.IsBinaryExpression(inner)
	case e.Kind == ast.KindTrueKeyword:
		return "false", false
	case e.Kind == ast.KindFalseKeyword:
		return "true", false
	case ast.IsBinaryExpression(e):
		b := e.AsBinaryExpression()
		left, right := l.Source(b.Left), l.Source(b.Right)
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
			nl, lor := l.Negate(c, b.Left)
			nr, ror := l.Negate(c, b.Right)
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
		return "!" + l.Source(e), false
	}
	return "!(" + l.Source(e) + ")", false
}

// ordered reports whether every value of e's type is a string or a
// bigint, for which < and >= are each other's negation (a number may be
// NaN).
func (l *Lined) ordered(c *checker.Checker, e *ast.Node) bool {
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
	l, why := p.Lined(path)
	if why != "" {
		return nil, why
	}
	ifs, why := l.ifAt(line, column)
	if why != "" {
		return nil, why
	}
	c, done := p.prog.GetProgram().GetTypeCheckerForFile(ctx, l.File)
	defer done()
	is := ifs.AsIfStatement()
	cond, _ := l.Negate(c, is.Expression)
	head := "if (" + cond + ") "
	then, els := is.ThenStatement, is.ElseStatement
	indent := l.Indent(l.Line(l.Pos(ifs)))
	edit := func(text string) []Edit {
		return []Edit{{File: l.File.FileName(), Start: l.Pos(ifs), End: ifs.End(), NewText: text}}
	}
	if els == nil {
		jump := ""
		owner := ifs.Parent
		switch {
		case !InList(ifs):
		case ast.IsBlock(owner) && ast.IsFunctionLike(owner.Parent):
			jump = "return" + l.Semi()
		case ast.IsBlock(owner) && ast.IsIterationStatement(owner.Parent, false):
			jump = "continue" + l.Semi()
		}
		if list := owner.Statements(); InList(ifs) && list[len(list)-1] != ifs {
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
		text := head + "{\n" + indent + l.Unit() + jump + "\n" + indent + "}"
		if rest != "" {
			text += "\n" + rest
		}
		return edit(text), ""
	}
	if ast.IsIfStatement(els) {
		return nil, fmt.Sprintf("the else is another if (%s); invert that one, or give it braces first", l.part(els))
	}
	if keepElse || !Terminates(els) {
		return edit(head + l.Source(els) + " else " + l.Source(then)), ""
	}
	rest, why := l.unnest(c, ifs, then)
	if why != "" {
		return nil, why
	}
	text := head + l.Source(els)
	if rest != "" {
		text += "\n" + rest
	}
	return edit(text), ""
}

// invertBefore returns the edits that invert ifs, which has no else and
// statements after it, R: its first branch T leaves, so if (c) { T } R
// becomes if (!c) { R } T. R must leave too, or end a function's or a
// loop's body, where jump ends it.
func (l *Lined) invertBefore(c *checker.Checker, ifs *ast.Node, head, jump string) ([]Edit, string) {
	then := ifs.AsIfStatement().ThenStatement
	list := ifs.Parent.Statements()
	rest := list[slices.Index(list, ifs)+1:]
	if !Terminates(then) {
		return nil, "an if without else is inverted, with what follows it as its new first branch, only when its branch ends with a return, throw, break or continue"
	}
	last := rest[len(rest)-1]
	if Terminates(last) {
		jump = ""
	} else if jump == "" {
		return nil, fmt.Sprintf("what follows the if can fall through (%s), and it would then go on to the if's branch", l.part(last))
	}
	var syms []*ast.Symbol
	for _, s := range rest {
		syms = append(syms, Symbols(c, Declared(s))...)
	}
	if u := Uses(c, then, syms); u != nil {
		return nil, fmt.Sprintf("the if's branch uses %s, which what follows it declares", u.Text())
	}
	indent := l.Indent(l.Line(l.Pos(ifs)))
	unit := l.Unit()
	a := l.Start(l.Line(l.Pos(rest[0])))
	_, endLine := l.Lines(last)
	if strings.TrimSpace(l.Text[a:l.Pos(rest[0])]) != "" || !quiet(l.Text[last.End():l.End(endLine)]) {
		return nil, "what follows the if shares its lines with other code"
	}
	body := l.reindent(a, l.Start(endLine+1), indent, indent+unit)
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
	return []Edit{{File: l.File.FileName(), Start: l.Pos(ifs), End: l.End(endLine), NewText: text}}, ""
}

// pastTryNote says what moving a statement past aTry changes: below it
// (below), it runs after everything the try does and only when the try
// completes normally; above it, before and also when it does not.
func (l *Lined) pastTryNote(aTry *ast.Node, below bool) string {
	exit := ""
	Inspect(aTry, func(n *ast.Node) bool {
		if exit != "" || ast.IsFunctionLike(n) {
			return false
		}
		kind := ""
		switch {
		case ast.IsReturnStatement(n):
			kind = "returns"
		case ast.IsThrowStatement(n):
			kind = "throws"
		case ast.IsBreakStatement(n):
			kind = "breaks"
		case ast.IsContinueStatement(n):
			kind = "continues"
		}
		if kind != "" {
			exit = " (" + l.Where(n) + " " + kind + ")"
		}
		return exit == ""
	})
	if below {
		return "it now runs after everything the try does, and only when the try completes normally: not when it returns, throws, breaks or continues" + exit
	}
	return "it now runs before everything the try does, and also when the try does not complete normally" + exit
}

// reindentSteps is reindent, also turning each step of fromStep in the
// indentation after from into one of toStep, for text moved into a class
// that indents by another step.
func (l *Lined) reindentSteps(start, end int, from, to, fromStep, toStep string) string {
	out := l.reindent(start, end, from, to)
	if fromStep == toStep || fromStep == "" {
		return out
	}
	inside := l.literalLines(start, end)
	lines := strings.Split(out, "\n")
	first := l.Line(start)
	for i, line := range lines {
		if inside[first+i] || !strings.HasPrefix(line, to) {
			continue
		}
		rest, n := textutil.CutSteps(line, to, fromStep)
		lines[i] = to + strings.Repeat(toStep, n) + rest
	}
	return strings.Join(lines, "\n")
}

// RemoveElse returns the edits that un-nest the else of the if on line,
// whose first branch cannot fall through: its statements follow the if.
func (p *Project) RemoveElse(ctx context.Context, path string, line, column int) ([]Edit, string) {
	l, why := p.Lined(path)
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
	case !Terminates(is.ThenStatement):
		return nil, fmt.Sprintf("the if's first branch can fall through to what follows it (it does not end with a return, throw, break or continue on every path); %s", map[bool]string{true: "the else does, so use invert_if", false: "neither branch can be un-nested"}[Terminates(is.ElseStatement)])
	case !InList(ifs):
		return nil, fmt.Sprintf("the if is %s's only statement; give it braces first", l.part(ifs.Parent))
	}
	c, done := p.prog.GetProgram().GetTypeCheckerForFile(ctx, l.File)
	defer done()
	els := is.ElseStatement
	var rest, why2 string
	if ast.IsIfStatement(els) {
		rest = l.Indent(l.Line(l.Pos(ifs))) + l.Source(els)
	} else {
		rest, why2 = l.unnest(c, ifs, els)
	}
	if why2 != "" {
		return nil, why2
	}
	text := l.Source(is.ThenStatement)
	if rest != "" {
		text += "\n" + rest
	}
	return []Edit{{File: l.File.FileName(), Start: l.Pos(is.ThenStatement), End: ifs.End(), NewText: text}}, ""
}

// WrapElse returns the edits that make the statements after the if on
// line (at column, when given), whose first branch cannot fall through,
// its else branch: the inverse of RemoveElse.
func (p *Project) WrapElse(ctx context.Context, path string, line, column int) ([]Edit, string) {
	l, why := p.Lined(path)
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
	case !Terminates(is.ThenStatement):
		return nil, "the if's branch can fall through to what follows it (it does not end with a return, throw, break or continue on every path), so in an else that code would no longer run after it"
	case !InList(ifs):
		return nil, fmt.Sprintf("the if is %s's only statement, so nothing follows it", l.part(ifs.Parent))
	}
	list := ifs.Parent.Statements()
	rest := list[slices.Index(list, ifs)+1:]
	if len(rest) == 0 {
		return nil, "nothing follows the if in its block"
	}
	for _, s := range rest {
		if ast.IsFunctionDeclaration(s) || ast.IsClassDeclaration(s) || ast.IsVariableStatement(s) && s.AsVariableStatement().DeclarationList.Flags&ast.NodeFlagsBlockScoped == 0 {
			return nil, fmt.Sprintf("%s declares what the code before the if may use, which in the else it could not", l.Where(s))
		}
	}
	f := l.File.FileName()
	edits := []Edit{{File: f, Start: is.ThenStatement.End(), End: is.ThenStatement.End(), NewText: " else {"}}
	end := rest[len(rest)-1].End()
	for _, n := range l.indentable(l.Line(l.Pos(rest[0])), l.Line(end)) {
		edits = append(edits, Edit{File: f, Start: l.Start(n), End: l.Start(n), NewText: l.Unit()})
	}
	edits = append(edits, Edit{File: f, Start: end, End: end, NewText: "\n" + l.Indent(l.Line(l.Pos(ifs))) + "}"})
	return edits, ""
}

// typedFunction returns the source of e, with the types its context gave
// an arrow function's or function expression's untyped parameters
// written on them: away from that context, as a variable's value, they
// would have none (java's #298).
func typedFunction(c *checker.Checker, l *Lined, e *ast.Node) string {
	src := l.Source(e)
	if !ast.IsArrowFunction(e) && !ast.IsFunctionExpression(e) {
		return src
	}
	params := e.Parameters()
	if len(params) == 0 {
		return src
	}
	start := l.Pos(e)
	var b strings.Builder
	at := start
	typed := false
	for _, p := range params {
		pd := p.AsParameterDeclaration()
		if p.Type() != nil || p.Initializer() != nil || pd.QuestionToken != nil {
			continue
		}
		t := c.GetTypeAtLocation(p.Name())
		if t == nil {
			continue
		}
		b.WriteString(l.Text[at:p.Name().End()])
		b.WriteString(": " + c.TypeToString(t))
		at = p.Name().End()
		typed = true
	}
	if !typed {
		return src
	}
	b.WriteString(l.Text[at:e.End()])
	out := b.String()
	first := l.Pos(params[0])
	if ast.IsArrowFunction(e) && strings.TrimSpace(l.Text[start:first]) == "" {
		// A lone parameter without parentheses gets them, to take a type.
		last := params[len(params)-1].End()
		head := out[:last-start+len(out)-(e.End()-start)]
		out = "(" + head + ")" + out[len(head):]
	}
	return out
}

// isJump reports whether s leaves its block: a return, throw, break or
// continue.
func isJump(s *ast.Node) bool {
	return ast.IsReturnStatement(s) || ast.IsThrowStatement(s) || ast.IsBreakStatement(s) || ast.IsContinueStatement(s)
}
