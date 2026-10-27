package tsapi

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/scanner"
)

// ReplaceExpression returns the edits that replace the expression written
// expr that starts on line (at column, when it is given) with newText,
// parenthesized where its place needs it. With all, every other
// expression of the file that means the same is replaced too: the same
// tokens, each name referring to the same thing. The edits come in the
// order of the file.
func (p *Project) ReplaceExpression(ctx context.Context, path string, line int, expr string, column int, newText string, all bool) (edits []Edit, refusal string) {
	l, refusal := p.lined(path)
	if refusal != "" {
		return nil, refusal
	}
	if line < 1 || line > l.count() {
		return nil, fmt.Sprintf("line %d out of range: the file has %d lines", line, l.count())
	}
	nw, refusal := parseExpression(newText)
	if refusal != "" {
		return nil, refusal
	}
	want := tokens(expr)
	if len(want) == 0 {
		return nil, "expression is empty"
	}
	same := func(n *ast.Node) bool { return slices.Equal(tokens(l.text[l.pos(n):n.End()]), want) }
	var found []*ast.Node
	l.expressions(func(n *ast.Node) {
		start := l.pos(n)
		if l.line(start) == line && (column == 0 || start-l.start(line)+1 == column) && same(n) &&
			!slices.ContainsFunc(found, func(f *ast.Node) bool { return l.pos(f) == start }) {
			found = append(found, n)
		}
	})
	switch len(found) {
	case 0:
		return nil, fmt.Sprintf("no expression %s starts on line %d: %s", strings.TrimSpace(expr), line, strings.TrimSpace(l.text[l.start(line):l.end(line)]))
	case 1:
	default:
		var cols []string
		for _, f := range found {
			cols = append(cols, fmt.Sprint(l.pos(f)-l.start(line)+1))
		}
		return nil, fmt.Sprintf("%s starts on line %d %d times; give column (%s)", strings.TrimSpace(expr), line, len(found), strings.Join(cols, ", "))
	}
	targets := found
	if all {
		c, done := p.prog.GetProgram().GetTypeCheckerForFile(ctx, l.f)
		defer done()
		symbols := func(n *ast.Node) []*ast.Symbol {
			var out []*ast.Symbol
			var visit func(n *ast.Node) bool
			visit = func(n *ast.Node) bool {
				if ast.IsIdentifier(n) {
					out = append(out, c.GetSymbolAtLocation(n))
				}
				return n.ForEachChild(visit)
			}
			visit(n)
			return out
		}
		meaning := symbols(found[0])
		targets = nil
		l.expressions(func(n *ast.Node) {
			if n.Kind == found[0].Kind && same(n) && slices.Equal(symbols(n), meaning) &&
				!slices.ContainsFunc(targets, func(t *ast.Node) bool { return t.Pos() <= n.Pos() && n.End() <= t.End() }) {
				targets = append(targets, n)
			}
		})
	}
	text := strings.TrimSpace(newText)
	for _, t := range targets {
		e := Edit{File: path, Start: l.pos(t), End: t.End(), NewText: text}
		if needsParens(t, nw) {
			e.NewText = "(" + text + ")"
		}
		edits = append(edits, e)
	}
	return edits, ""
}

// expressions calls fn with each expression of the file, outer before
// inner, leaving out names after a dot.
func (l *lined) expressions(fn func(*ast.Node)) {
	var visit func(n *ast.Node) bool
	visit = func(n *ast.Node) bool {
		if ast.IsExpressionNode(n) && !(ast.IsPropertyAccessExpression(n.Parent) && n.Parent.Name() == n) {
			fn(n)
		}
		n.ForEachChild(visit)
		return false
	}
	visit(l.f.AsNode())
}

// tokens returns the texts of the tokens of s.
func tokens(s string) []string {
	sc := scanner.NewScanner()
	sc.SetText(s)
	var out []string
	for k := sc.Scan(); k != ast.KindEndOfFile; k = sc.Scan() {
		out = append(out, sc.TokenText())
	}
	return out
}

// parseExpression parses text as one TypeScript expression.
func parseExpression(text string) (*ast.Node, string) {
	src := "(\n" + text + "\n);"
	f := parseText(src)
	if ds := f.Diagnostics(); len(ds) > 0 {
		return nil, "new is not a TypeScript expression: " + messages(ds)
	}
	if len(f.Statements.Nodes) == 1 && ast.IsExpressionStatement(f.Statements.Nodes[0]) {
		if paren := f.Statements.Nodes[0].Expression(); ast.IsParenthesizedExpression(paren) && paren.End() == len(src)-1 {
			return paren.Expression(), ""
		}
	}
	return nil, "new is not one TypeScript expression"
}

// needsParens reports whether nw, put in t's place, needs parentheses to
// be read as one operand there.
func needsParens(t, nw *ast.Node) bool {
	prec := ast.GetExpressionPrecedence(nw)
	parent := t.Parent
	comma := func(n *ast.Node) bool {
		return ast.IsBinaryExpression(n) && n.AsBinaryExpression().OperatorToken.Kind == ast.KindCommaToken
	}
	if parent == nil || ast.IsParenthesizedExpression(parent) {
		return false
	}
	if comma(nw) && !comma(parent) {
		return true
	}
	switch {
	case ast.IsBinaryExpression(parent):
		b := parent.AsBinaryExpression()
		op := b.OperatorToken.Kind
		switch {
		case op == ast.KindCommaToken:
			return false
		case ast.IsAssignmentOperator(op):
			return t == b.Left && prec < ast.OperatorPrecedenceLeftHandSide
		}
		if ast.IsBinaryExpression(nw) {
			inner := nw.AsBinaryExpression().OperatorToken.Kind
			coalesce := func(k ast.Kind) bool { return k == ast.KindQuestionQuestionToken }
			logical := func(k ast.Kind) bool { return k == ast.KindBarBarToken || k == ast.KindAmpersandAmpersandToken }
			if coalesce(op) && logical(inner) || logical(op) && coalesce(inner) {
				return true
			}
		}
		pp := ast.GetExpressionPrecedence(parent)
		exp := op == ast.KindAsteriskAsteriskToken
		if t == b.Left {
			return prec < pp || exp && (prec == pp || prec == ast.OperatorPrecedenceUnary)
		}
		return prec < pp || prec == pp && !exp
	case ast.IsPrefixUnaryExpression(parent), ast.IsAwaitExpression(parent), ast.IsTypeOfExpression(parent), ast.IsVoidExpression(parent), ast.IsDeleteExpression(parent):
		return prec < ast.OperatorPrecedenceUnary
	case ast.IsPostfixUnaryExpression(parent):
		return prec < ast.OperatorPrecedenceLeftHandSide
	case ast.IsNewExpression(parent):
		return t == parent.Expression() && prec < ast.OperatorPrecedenceMember
	case ast.IsPropertyAccessExpression(parent), ast.IsElementAccessExpression(parent), ast.IsCallExpression(parent), ast.IsTaggedTemplateExpression(parent), ast.IsNonNullExpression(parent):
		return t == parent.Expression() && prec < ast.OperatorPrecedenceLeftHandSide
	case ast.IsConditionalExpression(parent):
		return t == parent.AsConditionalExpression().Condition && prec <= ast.OperatorPrecedenceConditional
	case ast.IsAsExpression(parent), ast.IsSatisfiesExpression(parent):
		return prec <= ast.OperatorPrecedenceRelational
	case ast.IsArrowFunction(parent), ast.IsExpressionStatement(parent):
		return ast.IsObjectLiteralExpression(nw)
	}
	return false
}
