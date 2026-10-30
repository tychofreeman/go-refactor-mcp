package tsapi

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/scanner"
)

// IntroduceParameter returns the edits that replace the expression
// written expr in the body of the function or method called name with a
// new parameter called param, its last but for a rest parameter, and pass
// the expression at every call in the call's own terms: a parameter of the
// function becomes the call's argument, this the call's receiver. Or why
// it cannot: the expression is then evaluated once, at the call, so it
// must have no effects, mean the same there, and read nothing the body
// changes before it reads it.
func (p *Project) IntroduceParameter(ctx context.Context, path, name, expr, param string) ([]Edit, string) {
	t, done, why := p.target(ctx, path, name)
	if why != "" {
		return nil, why
	}
	defer done()
	l, c, fn := t.l, t.c, t.fn
	switch {
	case !scanner.IsIdentifierText(param, 0):
		return nil, fmt.Sprintf("%q is not a valid parameter name", param)
	case t.index(param) >= 0:
		return nil, fmt.Sprintf("%s already has a parameter %s", name, param)
	}
	if other := l.reads(c, fn.Body(), param, nil); other != "" {
		return nil, fmt.Sprintf("the body of %s reads another %s (%s), which the parameter would hide; choose another name", name, param, other)
	}
	want := tokens(expr)
	var occurrences []*ast.Node
	var visit func(n *ast.Node) bool
	visit = func(n *ast.Node) bool {
		if ast.IsExpressionNode(n) && !(ast.IsPropertyAccessExpression(n.Parent) && n.Parent.Name() == n) && slices.Equal(tokens(l.source(n)), want) {
			occurrences = append(occurrences, n)
			return false
		}
		n.ForEachChild(visit)
		return false
	}
	fn.Body().ForEachChild(visit)
	if len(occurrences) == 0 {
		return nil, fmt.Sprintf("the body of %s has no expression %s", name, strings.TrimSpace(expr))
	}
	for _, o := range occurrences {
		if written(o) {
			return nil, fmt.Sprintf("the body of %s assigns %s (%s), which a parameter cannot stand for", name, strings.TrimSpace(expr), l.where(o))
		}
	}
	e := occurrences[0]
	if why := t.findCalls(p); why != "" {
		return nil, why
	}
	texts, why := t.introduced(e, expr, len(occurrences), name)
	if why != "" {
		return nil, why
	}
	typ := c.TypeToString(c.GetWidenedType(c.GetBaseTypeOfLiteralType(c.GetTypeAtLocation(e))))
	n := len(t.params) - t.off
	at := n
	if t.isRest(n - 1) {
		at = n - 1
	}
	edits := []Edit{l.insertAt(t.fn.ParameterList(), t.off+at, param+": "+typ)}
	more, why := t.insertInOverloads(at, param+": "+typ)
	if why != "" {
		return nil, why
	}
	edits = append(edits, more...)
	meaning := func(x *ast.Node) bool {
		if !slices.Equal(tokens(l.source(x)), want) {
			return false
		}
		return slices.Equal(symbolsOf(c, x), symbolsOf(c, e))
	}
	for _, o := range occurrences {
		if meaning(o) {
			edits = append(edits, Edit{File: l.f.FileName(), Start: l.pos(o), End: o.End(), NewText: param})
		}
	}
	for i, call := range t.calls {
		cl := newLined(ast.GetSourceFileOfNode(call))
		args := call.ArgumentList()
		text := texts[i]
		m := len(args.Nodes)
		if m < at {
			text = strings.Repeat("undefined, ", at-m) + text
		}
		edits = append(edits, cl.insertAt(args, min(at, m), text))
	}
	return edits, ""
}

// introduced returns what each call of t (findCalls has found them)
// would pass for e, an expression written expr in its body that the body
// evaluates n times, written in the call's own terms: a parameter of the
// function becomes the call's argument, this the call's receiver. Or why
// it cannot: the expression is then evaluated once, at the call, so it
// must have no effects, mean the same there, and read nothing the body
// changes before it reads it.
func (t *target) introduced(e *ast.Node, expr string, n int, name string) ([]string, string) {
	l, c, fn := t.l, t.c, t.fn
	var why string
	if ef := newEffects(c, l).Of(e); !ef.CanDrop() || n > 1 && !ef.CanRepeat() {
		return nil, fmt.Sprintf("every call would evaluate %s instead of the body, and it may not be moved: %s", strings.TrimSpace(expr), ef.Why+map[bool]string{true: "it makes a new value, which the body's uses would then share"}[ef.Why == ""])
	}

	// What the expression reads: parameters (by position), this, and names
	// from outside the function, which each call must see alike.
	type read struct {
		node  *ast.Node
		param int // position of the parameter it reads, or -1
		outer *ast.Symbol
	}
	var reads []read
	mutable := false
	var scan func(n *ast.Node) bool
	scan = func(n *ast.Node) bool {
		if why != "" {
			return true
		}
		switch {
		case n.Kind == ast.KindThisKeyword:
			if t.ctor || !ast.IsMethodDeclaration(fn) {
				why = fmt.Sprintf("%s reads this, which a call of %s has no receiver for", strings.TrimSpace(expr), name)
				return true
			}
			reads = append(reads, read{node: n, param: -1})
			mutable = true
		case ast.IsPropertyAccessExpression(n):
			mutable = true
			scan(n.Expression())
			return false
		case ast.IsIdentifier(n) && !(ast.IsPropertyAssignment(n.Parent) && n.Parent.Name() == n):
			s := c.GetSymbolAtLocation(n)
			if s == nil {
				why = fmt.Sprintf("%s in %s refers to nothing known", n.Text(), strings.TrimSpace(expr))
				return true
			}
			d := s.ValueDeclaration
			switch {
			case d != nil && ast.IsParameterDeclaration(d) && d.Parent == fn:
				i := t.index(n.Text())
				if t.isRest(i) {
					why = fmt.Sprintf("%s reads the rest parameter %s", strings.TrimSpace(expr), n.Text())
					return true
				}
				if where := l.assigns(fn.Body(), s, c); where != "" {
					why = fmt.Sprintf("the body of %s assigns %s (%s), so %s may not have the value the call passed", name, n.Text(), where, strings.TrimSpace(expr))
					return true
				}
				reads = append(reads, read{node: n, param: i})
			case d != nil && d.Pos() >= fn.Pos() && d.End() <= fn.End():
				why = fmt.Sprintf("%s reads %s, a local of %s, which a call cannot see", strings.TrimSpace(expr), n.Text(), name)
				return true
			default:
				if s.Flags&(ast.SymbolFlagsBlockScopedVariable|ast.SymbolFlagsFunctionScopedVariable) != 0 && !isConst(d) {
					mutable = true
				}
				reads = append(reads, read{node: n, param: -1, outer: c.SkipAlias(s)})
			}
		}
		n.ForEachChild(scan)
		return false
	}
	scan(e)
	if why != "" {
		return nil, why
	}
	if mutable {
		if where := l.changesBefore(fn.Body(), e); where != "" {
			return nil, fmt.Sprintf("%s reads state the body may change before it reads it (at %s); a call would read it before", strings.TrimSpace(expr), where)
		}
		if where := l.repeated(fn, e); where != "" {
			return nil, fmt.Sprintf("%s is in %s, which may evaluate it more than once, maybe to different values", strings.TrimSpace(expr), where)
		}
	}

	var texts []string
	for _, call := range t.calls {
		cl := newLined(ast.GetSourceFileOfNode(call))
		args := call.ArgumentList()
		// The expression in the call's terms.
		type sub struct {
			start, end int
			text       string
		}
		var subs []sub
		for _, r := range reads {
			switch {
			case r.outer != nil:
				s := c.ResolveName(r.node.Text(), call, ast.SymbolFlagsValue, false)
				if s == nil || c.SkipAlias(s) != r.outer {
					return nil, fmt.Sprintf("%s in %s is not the same at the call at %s; pass it yourself", r.node.Text(), strings.TrimSpace(expr), cl.where(call))
				}
			case r.param >= 0:
				text := "undefined"
				if r.param < len(args.Nodes) {
					a := args.Nodes[r.param]
					if ef := newEffects(c, cl).Of(a); !ef.CanRepeat() {
						return nil, fmt.Sprintf("the argument %s at %s would be evaluated twice: %s", cl.source(a), cl.where(call), ef.Why+map[bool]string{true: "it makes a new value each time"}[ef.Why == ""])
					}
					text = operand(cl, a)
				}
				subs = append(subs, sub{l.pos(r.node), r.node.End(), text})
			default: // this
				callee := call.Expression()
				if !ast.IsPropertyAccessExpression(callee) || !newEffects(c, cl).Of(callee.Expression()).CanRepeat() {
					return nil, fmt.Sprintf("the call at %s has no plain receiver to stand for this", cl.where(call))
				}
				subs = append(subs, sub{l.pos(r.node), r.node.End(), operand(cl, callee.Expression())})
			}
		}
		sort.Slice(subs, func(i, j int) bool { return subs[i].start > subs[j].start })
		text := l.source(e)
		base := l.pos(e)
		for _, s := range subs {
			text = text[:s.start-base] + s.text + text[s.end-base:]
		}
		texts = append(texts, text)
	}
	return texts, ""
}

// operand is the source of e, parenthesized unless it is a primary
// expression that stays one operand anywhere.
func operand(l *lined, e *ast.Node) string {
	switch {
	case ast.IsIdentifier(e), ast.IsLiteralExpression(e), ast.IsPropertyAccessExpression(e), ast.IsParenthesizedExpression(e), e.Kind == ast.KindThisKeyword:
		return l.source(e)
	}
	return "(" + l.source(e) + ")"
}

// isConst reports whether the declaration d is of a const.
func isConst(d *ast.Node) bool {
	if d == nil || !ast.IsVariableDeclaration(d) {
		return true // a function, class, enum or import
	}
	return d.Parent != nil && d.Parent.Flags&ast.NodeFlagsConst != 0
}

// symbolsOf returns the symbols of the identifiers of n, in order.
func symbolsOf(c interface {
	GetSymbolAtLocation(*ast.Node) *ast.Symbol
}, n *ast.Node) []*ast.Symbol {
	var out []*ast.Symbol
	var visit func(x *ast.Node) bool
	visit = func(x *ast.Node) bool {
		if ast.IsIdentifier(x) {
			out = append(out, c.GetSymbolAtLocation(x))
		}
		return x.ForEachChild(visit)
	}
	visit(n)
	return out
}

// assigns returns where body assigns the variable s, or "".
func (l *lined) assigns(body *ast.Node, s *ast.Symbol, c interface {
	GetSymbolAtLocation(*ast.Node) *ast.Symbol
}) string {
	where := ""
	var visit func(n *ast.Node) bool
	visit = func(n *ast.Node) bool {
		if where != "" {
			return true
		}
		var target *ast.Node
		switch {
		case ast.IsBinaryExpression(n) && ast.IsAssignmentOperator(n.AsBinaryExpression().OperatorToken.Kind):
			target = n.AsBinaryExpression().Left
		case ast.IsPrefixUnaryExpression(n):
			target = n.AsPrefixUnaryExpression().Operand
		case ast.IsPostfixUnaryExpression(n):
			target = n.AsPostfixUnaryExpression().Operand
		}
		if target != nil && ast.IsIdentifier(target) && c.GetSymbolAtLocation(target) == s {
			where = l.where(n)
		}
		return n.ForEachChild(visit)
	}
	visit(body)
	return where
}

// changesBefore returns where body may change state before it evaluates
// e: a call, a new, an assignment, an update or a delete that ends before
// e starts; or "".
func (l *lined) changesBefore(body *ast.Node, e *ast.Node) string {
	where := ""
	start := l.pos(e)
	var visit func(n *ast.Node) bool
	visit = func(n *ast.Node) bool {
		if where != "" || l.pos(n) >= start {
			return true
		}
		if ast.IsFunctionLike(n) {
			return false
		}
		changes := ast.IsCallExpression(n) || ast.IsNewExpression(n) || ast.IsDeleteExpression(n) || ast.IsAwaitExpression(n) ||
			ast.IsPrefixUnaryExpression(n) && (n.AsPrefixUnaryExpression().Operator == ast.KindPlusPlusToken || n.AsPrefixUnaryExpression().Operator == ast.KindMinusMinusToken) ||
			ast.IsPostfixUnaryExpression(n) ||
			ast.IsBinaryExpression(n) && ast.IsAssignmentOperator(n.AsBinaryExpression().OperatorToken.Kind)
		if changes && n.End() <= start {
			where = l.where(n)
			return true
		}
		n.ForEachChild(visit)
		return false
	}
	body.ForEachChild(visit)
	return where
}

// repeated returns the loop or function inside fn that e is in, which may
// evaluate it more than once; or "".
func (l *lined) repeated(fn, e *ast.Node) string {
	for n := e.Parent; n != nil && n != fn; n = n.Parent {
		switch {
		case ast.IsForStatement(n), ast.IsForInStatement(n), ast.IsForOfStatement(n), ast.IsWhileStatement(n), ast.IsDoStatement(n):
			return "a loop at " + l.where(n)
		case ast.IsFunctionLike(n):
			return "a function at " + l.where(n)
		}
	}
	return ""
}
