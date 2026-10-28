package tsapi

import (
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/checker"
)

// A hole may stand for text that is not one node of the code: constant
// text of a template, or a run of operands of a + concatenation.

// bindText records that hole stands for the text, with the code nodes it
// is made of (none for constant text), unless it stands for something
// else already.
func (m *matcher) bindText(p *ast.Node, hole int, text string, run []*ast.Node, at *ast.Node) bool {
	m.uses[hole] = append(m.uses[hole], p)
	if m.args[hole] != nil {
		if m.texts[hole] != text {
			m.why = "the same parameter stands for different text"
			return false
		}
		return true
	}
	m.args[hole], m.texts[hole], m.runs[hole] = at, text, run
	return true
}

// stringHole returns the hole p is when it is a parameter of string
// type, or -1.
func (m *matcher) stringHole(p *ast.Node) int {
	if !ast.IsIdentifier(p) || propertyName(p) {
		return -1
	}
	i, ok := m.holes[symbolOf(m.c, p)]
	if !ok || !isString(m.c, m.c.GetTypeAtLocation(p)) {
		return -1
	}
	return i
}

func isString(c *checker.Checker, t *checker.Type) bool {
	return t != nil && t.Flags()&checker.TypeFlagsStringLike != 0
}

// operands returns the operands of a chain of + (a + b + c), left to
// right; x alone when it is not one.
func operands(x *ast.Node) []*ast.Node {
	if ast.IsBinaryExpression(x) && x.AsBinaryExpression().OperatorToken.Kind == ast.KindPlusToken {
		b := x.AsBinaryExpression()
		return append(operands(b.Left), b.Right)
	}
	return []*ast.Node{x}
}

// matchConcat matches the chains of + p and x, operand by operand, a
// string parameter of p standing for a run of x's operands whose first
// is a string, so that the run concatenates alone as it did in x.
func (m *matcher) matchConcat(p, x *ast.Node) bool {
	ps, xs := operands(p), operands(x)
	var try func(m *matcher, i, j int) *matcher
	try = func(m *matcher, i, j int) *matcher {
		if i == len(ps) {
			if j == len(xs) {
				return m
			}
			return nil
		}
		if j == len(xs) {
			return nil
		}
		if h := m.stringHole(ps[i]); h >= 0 {
			for k := len(xs); k > j+1; k-- { // a run of two or more
				if !isString(m.c, m.c.GetTypeAtLocation(xs[j])) {
					break
				}
				n := m.fork()
				text := m.cl.text[m.cl.pos(xs[j]):xs[k-1].End()]
				if n.bindText(ps[i], h, text, xs[j:k], xs[j]) {
					if r := try(n, i+1, k); r != nil {
						return r
					}
				}
			}
		}
		n := m.fork()
		if !n.match(ps[i], xs[j]) {
			return nil
		}
		return try(n, i+1, j+1)
	}
	if r := try(m, 0, 0); r != nil {
		*m = *r
		return true
	}
	return false
}

// piece is a part of a template: literal text, or a substitution.
type piece struct {
	text string
	expr *ast.Node
}

// pieces returns the parts of a template or string literal, its literal
// text as it is written (escapes kept).
func pieces(l *lined, n *ast.Node) ([]piece, bool) {
	raw := func(s string, cut int) string { return s[1 : len(s)-cut] }
	switch {
	case ast.IsStringLiteral(n) || n.Kind == ast.KindNoSubstitutionTemplateLiteral:
		s := l.source(n)
		if ast.IsStringLiteral(n) && strings.ContainsAny(s, "`$") {
			return nil, false
		}
		return []piece{{text: raw(s, 1)}}, true
	case ast.IsTemplateExpression(n):
		te := n.AsTemplateExpression()
		out := []piece{{text: raw(l.source(te.Head), 2)}}
		for _, sp := range te.TemplateSpans.Nodes {
			s := sp.AsTemplateSpan()
			out = append(out, piece{expr: s.Expression})
			lit := l.source(s.Literal)
			cut := 2
			if s.Literal.Kind == ast.KindTemplateTail {
				cut = 1
			}
			out = append(out, piece{text: raw(lit, cut)})
		}
		return out, true
	}
	return nil, false
}

// matchTemplate matches the template p with the template or string x: a
// string parameter of p's may stand for constant text of x as well as for
// a substitution.
func (m *matcher) matchTemplate(p, x *ast.Node) bool {
	pp, ok1 := pieces(m.pl, p)
	xp, ok2 := pieces(m.cl, x)
	if !ok1 || !ok2 {
		return false
	}
	// x's literal text a character at a time, and its substitutions.
	type item struct {
		ch   byte
		expr *ast.Node
	}
	var xs []item
	for _, pc := range xp {
		if pc.expr != nil {
			xs = append(xs, item{expr: pc.expr})
			continue
		}
		for i := 0; i < len(pc.text); i++ {
			xs = append(xs, item{ch: pc.text[i]})
		}
	}
	quote := m.cl.quote()
	var try func(m *matcher, i, off, j int) *matcher // pattern piece i, at offset off of its text; x item j
	try = func(m *matcher, i, off, j int) *matcher {
		if i == len(pp) {
			if j == len(xs) {
				return m
			}
			return nil
		}
		pc := pp[i]
		if pc.expr == nil {
			if off == len(pc.text) {
				return try(m, i+1, 0, j)
			}
			if j < len(xs) && xs[j].expr == nil && xs[j].ch == pc.text[off] {
				return try(m, i, off+1, j+1)
			}
			return nil
		}
		if h := m.stringHole(pc.expr); h >= 0 {
			// Constant text: the longest first.
			k := j
			for k < len(xs) && xs[k].expr == nil {
				k++
			}
			for e := k; e >= j; e-- {
				var b strings.Builder
				for _, it := range xs[j:e] {
					b.WriteByte(it.ch)
				}
				text := b.String()
				if strings.Contains(text, quote) || strings.Contains(text, "\\") {
					continue
				}
				n := m.fork()
				if n.bindText(pc.expr, h, quote+text+quote, nil, x) {
					if r := try(n, i+1, 0, e); r != nil {
						return r
					}
				}
			}
		}
		if j < len(xs) && xs[j].expr != nil {
			n := m.fork()
			if n.match(pc.expr, xs[j].expr) {
				return try(n, i+1, 0, j+1)
			}
		}
		return nil
	}
	if r := try(m, 0, 0, 0); r != nil {
		*m = *r
		return true
	}
	return false
}
