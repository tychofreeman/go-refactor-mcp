package tsapi

import (
	"context"
	"fmt"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/scanner"
)

// AddResult returns the edits that add a result to the function or method
// called name: its result type becomes an object holding the old result as
// kept and the new one as field, of type typ; every return gives value for
// it; and every call whose result is used reads .kept, so that it gets
// what it got before. A call that is a statement stays.
func (p *Project) AddResult(ctx context.Context, path, name, field, typ, value, kept string) ([]Edit, string) {
	t, done, why := p.target(ctx, path, name)
	if why != "" {
		return nil, why
	}
	defer done()
	l, c, fn := t.l, t.c, t.fn
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
	case fn.ModifierFlags()&ast.ModifierFlagsAsync != 0:
		return nil, fmt.Sprintf("%s is async; its calls would need (await %s(...)).%s", name, name, kept)
	case ast.GetFunctionFlags(fn)&ast.FunctionFlagsGenerator != 0:
		return nil, fmt.Sprintf("%s is a generator", name)
	}
	v, why := parseExpression(value)
	if why != "" {
		return nil, strings.Replace(why, "new is", "value is", 1)
	}
	if !simple(v) {
		return nil, fmt.Sprintf("value %s could have effects, and every return would evaluate it", strings.TrimSpace(value))
	}
	old := ""
	if rt := fn.Type(); rt != nil {
		old = l.source(rt)
	} else if sig := c.GetSignatureFromDeclaration(fn); sig != nil {
		old = c.TypeToString(c.GetReturnTypeOfSignature(sig))
	}
	if old == "" || old == "void" || old == "never" {
		return nil, fmt.Sprintf("%s returns %s, so it has no result to keep; give it one first", name, map[bool]string{true: "nothing", false: old}[old == ""])
	}
	if why := t.findCalls(p); why != "" {
		return nil, why
	}
	val := strings.TrimSpace(value)
	// inner is the source of e with .kept after each call of the
	// function inside it (a recursive call), which then needs no edit of
	// its own.
	inside := map[*ast.Node]bool{}
	inner := func(e *ast.Node) string {
		src := l.source(e)
		base := l.pos(e)
		for i := len(t.calls) - 1; i >= 0; i-- {
			call := t.calls[i]
			if ast.GetSourceFileOfNode(call) == l.f && l.pos(call) >= base && call.End() <= e.End() && !ast.IsExpressionStatement(call.Parent) {
				inside[call] = true
				at := call.End() - base
				src = src[:at] + "." + kept + src[at:]
			}
		}
		return src
	}
	obj := func(e string) string { return fmt.Sprintf("{ %s: %s, %s: %s }", kept, e, field, val) }
	newType := fmt.Sprintf("{ %s: %s; %s: %s }", kept, old, field, strings.TrimSpace(typ))
	var edits []Edit
	if rt := fn.Type(); rt != nil {
		edits = append(edits, Edit{File: l.f.FileName(), Start: l.pos(rt), End: rt.End(), NewText: newType})
	} else {
		params := fn.ParameterList()
		at := params.End() + strings.IndexByte(l.text[params.End():], ')')
		if at < params.End() || ast.IsArrowFunction(fn) && l.text[l.pos(fn)] != '(' && l.text[l.pos(fn)] != '<' {
			return nil, fmt.Sprintf("give %s's parameter parentheses first, to write its result type after them", name)
		}
		edits = append(edits, Edit{File: l.f.FileName(), Start: at + 1, End: at + 1, NewText: ": " + newType})
	}
	for _, o := range t.overloads {
		rt := o.Type()
		if rt == nil {
			return nil, fmt.Sprintf("the overload at %s has no result type written; write it first", l.where(o))
		}
		edits = append(edits, Edit{File: l.f.FileName(), Start: l.pos(rt), End: rt.End(), NewText: fmt.Sprintf("{ %s: %s; %s: %s }", kept, l.source(rt), field, strings.TrimSpace(typ))})
	}
	body := fn.Body()
	if !ast.IsBlock(body) {
		edits = append(edits, Edit{File: l.f.FileName(), Start: l.pos(body), End: body.End(), NewText: "(" + obj(inner(body)) + ")"})
	} else {
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
	for _, call := range t.calls {
		if inside[call] || call.Parent != nil && ast.IsExpressionStatement(call.Parent) {
			continue
		}
		cl := newLined(ast.GetSourceFileOfNode(call))
		edits = append(edits, Edit{File: cl.f.FileName(), Start: call.End(), End: call.End(), NewText: "." + kept})
	}
	return edits, ""
}
