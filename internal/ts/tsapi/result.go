package tsapi

import (
	"context"
	"fmt"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/checker"
	"github.com/buke/typescript-go-internal/v7/pkg/scanner"
)

// AddResult returns the edits that add a result to the function or method
// called name, and to the methods of its family: its result type becomes
// an object holding the old result as kept and the new one as field, of
// type typ; every return gives value for it; and every call whose result
// is used reads .kept, so that it gets what it got before. A call that is
// a statement stays.
func (p *Project) AddResult(ctx context.Context, path, name, field, typ, value, kept string) ([]Edit, string) {
	t, done, why := p.target(ctx, path, name)
	if why != "" {
		return nil, why
	}
	defer done()
	fn := t.fn
	if kept == "" {
		kept = "value"
	}
	switch {
	case !scanner.IsIdentifierText(field, 0):
		return nil, fmt.Sprintf("%q is not a valid field name", field)
	case !scanner.IsIdentifierText(kept, 0):
		return nil, fmt.Sprintf("%q is not a valid field name", kept)
	case field == kept:
		return nil, fmt.Sprintf("the new result and the old one are both named %s", field)
	case t.ctor:
		return nil, "a constructor has no result to add to"
	}
	v, why := parseExpression(value)
	if why != "" {
		return nil, strings.Replace(why, "new is", "value is", 1)
	}
	if !simple(v) {
		return nil, fmt.Sprintf("value %s could have effects, and every return would evaluate it", strings.TrimSpace(value))
	}
	if why := t.findCalls(p); why != "" {
		return nil, why
	}
	r := &result{t: t, field: field, typ: strings.TrimSpace(typ), val: strings.TrimSpace(value), kept: kept, inside: map[*ast.Node]bool{}}
	var edits []Edit
	for _, m := range append([]*ast.Node{fn}, t.family...) {
		mname := name
		if m != fn {
			mname = memberName(m) + ", which changes with " + name + ","
		}
		es, why := r.edits(m, mname)
		if why != "" {
			return nil, why
		}
		edits = append(edits, es...)
	}
	for _, call := range t.calls {
		if r.inside[call] || call.Parent != nil && ast.IsExpressionStatement(call.Parent) {
			continue
		}
		cl := newLined(ast.GetSourceFileOfNode(call))
		edits = append(edits, Edit{File: cl.f.FileName(), Start: call.End(), End: call.End(), NewText: "." + kept})
	}
	return edits, ""
}

// result is a result being added by AddResult.
type result struct {
	t                     *target
	field, typ, val, kept string
	// inside are the calls of the function inside a return's expression,
	// which read kept there and need no edit of their own.
	inside map[*ast.Node]bool
}

// edits returns the edits that make m, the function or a method of its
// family (called name in refusals), return the object of the old result
// and the new one, with its overloads' result types.
func (r *result) edits(m *ast.Node, name string) ([]Edit, string) {
	l, c := newLined(ast.GetSourceFileOfNode(m)), r.t.c
	switch {
	case m.ModifierFlags()&ast.ModifierFlagsAsync != 0:
		return nil, fmt.Sprintf("%s is async; its calls would need (await %s(...)).%s", name, name, r.kept)
	case ast.GetFunctionFlags(m)&ast.FunctionFlagsGenerator != 0:
		return nil, fmt.Sprintf("%s is a generator", name)
	}
	old := l.returnType(m, c)
	if old == "" || old == "void" || old == "never" {
		return nil, fmt.Sprintf("%s returns %s, so it has no result to keep; give it one first", name, map[bool]string{true: "nothing", false: old}[old == ""])
	}
	// inner is the source of e with .kept after each call of the
	// function inside it (a recursive call), which then needs no edit of
	// its own.
	inner := func(e *ast.Node) string {
		src := l.source(e)
		base := l.pos(e)
		for i := len(r.t.calls) - 1; i >= 0; i-- {
			call := r.t.calls[i]
			if ast.GetSourceFileOfNode(call) == l.f && l.pos(call) >= base && call.End() <= e.End() && !ast.IsExpressionStatement(call.Parent) {
				r.inside[call] = true
				at := call.End() - base
				src = src[:at] + "." + r.kept + src[at:]
			}
		}
		return src
	}
	obj := func(e string) string { return fmt.Sprintf("{ %s: %s, %s: %s }", r.kept, e, r.field, r.val) }
	newType := fmt.Sprintf("{ %s: %s; %s: %s }", r.kept, old, r.field, r.typ)
	var edits []Edit
	if rt := m.Type(); rt != nil {
		edits = append(edits, Edit{File: l.f.FileName(), Start: l.pos(rt), End: rt.End(), NewText: newType})
	} else {
		params := m.ParameterList()
		at := params.End() + strings.IndexByte(l.text[params.End():], ')')
		if at < params.End() || ast.IsArrowFunction(m) && l.text[l.pos(m)] != '(' && l.text[l.pos(m)] != '<' {
			return nil, fmt.Sprintf("give %s's parameter parentheses first, to write its result type after them", name)
		}
		edits = append(edits, Edit{File: l.f.FileName(), Start: at + 1, End: at + 1, NewText: ": " + newType})
	}
	if m == r.t.fn {
		for _, o := range r.t.overloads {
			rt := o.Type()
			if rt == nil {
				return nil, fmt.Sprintf("the overload at %s has no result type written; write it first", l.where(o))
			}
			edits = append(edits, Edit{File: l.f.FileName(), Start: l.pos(rt), End: rt.End(), NewText: fmt.Sprintf("{ %s: %s; %s: %s }", r.kept, l.source(rt), r.field, r.typ)})
		}
	}
	body := m.Body()
	switch {
	case body == nil:
	case !ast.IsBlock(body):
		edits = append(edits, Edit{File: l.f.FileName(), Start: l.pos(body), End: body.End(), NewText: "(" + obj(inner(body)) + ")"})
	default:
		var visit func(n *ast.Node) bool
		visit = func(n *ast.Node) bool {
			if ast.IsFunctionLike(n) || ast.IsClassLike(n) {
				return false
			}
			if ast.IsReturnStatement(n) {
				e := n.Expression()
				if e == nil {
					edits = append(edits, Edit{File: l.f.FileName(), Start: n.End() - 1, End: n.End() - 1, NewText: " " + obj("undefined")})
					if l.text[n.End()-1] != ';' {
						edits[len(edits)-1].Start, edits[len(edits)-1].End = n.End(), n.End()
					}
				} else {
					edits = append(edits, Edit{File: l.f.FileName(), Start: l.pos(e), End: e.End(), NewText: obj(inner(e))})
				}
			}
			n.ForEachChild(visit)
			return false
		}
		body.ForEachChild(visit)
	}
	return edits, ""
}

// returnType is the type fn returns, as written or else as checked by c,
// or "" when neither is known.
func (l *lined) returnType(fn *ast.Node, c *checker.Checker) string {
	ret := ""
	if rt := fn.Type(); rt != nil {
		ret = l.source(rt)
	} else if sig := c.GetSignatureFromDeclaration(fn); sig != nil {
		ret = c.TypeToString(c.GetReturnTypeOfSignature(sig))
	}
	return ret
}
