package tsapi

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/checker"

	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
)

// MoveMethod returns the edits that move the method name ("Soil.acid") of
// a class in the file at path, with the methods of that class named in
// also, down to the class to, which extends it: their text goes to the end
// of to's body. Every use must reach them through a to (or a class that
// extends it), so that each still finds them there.
func (p *Project) MoveMethod(ctx context.Context, path, name, to string, also []string) ([]Edit, string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, why
	}
	owner, _, ok := strings.Cut(name, ".")
	if !ok {
		return nil, fmt.Sprintf("name the method as Class.method, not %q", name)
	}
	var moved []*ast.Node
	for _, n := range append([]string{name}, also...) {
		if !strings.Contains(n, ".") {
			n = owner + "." + n
		}
		if o, _, _ := strings.Cut(n, "."); o != owner {
			return nil, fmt.Sprintf("also names %s, not a method of %s", n, owner)
		}
		fn, ov, _, why := l.function(n)
		switch {
		case why != "":
			return nil, why
		case !ast.IsMethodDeclaration(fn):
			return nil, fmt.Sprintf("%s is not a method", n)
		case len(ov) > 0:
			return nil, fmt.Sprintf("%s has overloads", n)
		case ast.IsStatic(fn):
			return nil, fmt.Sprintf("%s is static; its calls name %s", n, owner)
		}
		moved = append(moved, fn)
	}
	from := moved[0].Parent
	c, done := p.prog.GetProgram().GetTypeChecker(ctx)
	defer done()
	// The class to: one of the solution's that extends from.
	var targets []*ast.Node
	for _, f := range p.sources() {
		inspect(f.AsNode(), func(n *ast.Node) bool {
			if ast.IsClassDeclaration(n) && n.Name() != nil && n.Name().Text() == to && n != from && extends(c, n, from) {
				targets = append(targets, n)
			}
			return true
		})
	}
	switch len(targets) {
	case 0:
		return nil, fmt.Sprintf("no class %s in the solution extends %s", to, owner)
	case 1:
	default:
		return nil, fmt.Sprintf("%d classes %s extend %s; rename one first", len(targets), to, owner)
	}
	target := targets[0]
	tl := newLined(ast.GetSourceFileOfNode(target))
	syms := map[*ast.Symbol]bool{}
	for _, m := range moved {
		mname := m.Name().Text()
		if slices.ContainsFunc(target.Members(), func(x *ast.Node) bool { return x.Name() != nil && tl.source(x.Name()) == mname }) {
			return nil, fmt.Sprintf("%s already has a member %s", to, mname)
		}
		family, why := p.family(c, m)
		if why != "" {
			return nil, why
		}
		if len(family) > 0 {
			return nil, fmt.Sprintf("%s changes with %s, which implements, overrides or is overridden by it; it could not move alone", memberName(m), memberName(family[0]))
		}
		syms[c.GetSymbolAtLocation(m.Name())] = true
	}
	// Every use reaches the methods through a to.
	reaches := func(cls *ast.Node) bool { return cls == target || cls != nil && extends(c, cls, target) }
	for _, f := range p.sources() {
		fl := newLined(f)
		why := ""
		identifiers(f.AsNode(), func(id *ast.Node) {
			if why != "" || ast.IsDeclarationName(id) || !syms[symbolOf(c, id)] {
				return
			}
			acc := id.Parent
			if !ast.IsPropertyAccessExpression(acc) || acc.Name() != id {
				why = fmt.Sprintf("%s at %s is not reached through a %s", id.Text(), fl.where(id), to)
				return
			}
			recv := ast.SkipParentheses(acc.Expression())
			if recv.Kind == ast.KindThisKeyword || recv.Kind == ast.KindSuperKeyword {
				m := ast.GetContainingFunction(id)
				for m != nil && ast.IsArrowFunction(m) {
					m = ast.GetContainingFunction(m)
				}
				switch {
				case m != nil && slices.Contains(moved, m):
				case m != nil && m.Parent != nil && ast.IsClassDeclaration(m.Parent) && reaches(m.Parent):
				case m != nil && m.Parent != nil && ast.IsClassLike(m.Parent) && m.Parent.Name() != nil:
					why = fmt.Sprintf("%s at %s reaches %s in %s, which does not move; name it in also", fl.source(acc.Parent), fl.where(id), id.Text(), memberName(m))
				default:
					why = fmt.Sprintf("%s at %s is not reached through a %s", fl.source(acc), fl.where(id), to)
				}
				return
			}
			t := c.GetTypeAtLocation(recv)
			var cls *ast.Node
			if t != nil && t.Symbol() != nil {
				for _, d := range t.Symbol().Declarations {
					if ast.IsClassDeclaration(d) {
						cls = d
					}
				}
			}
			if !reaches(cls) {
				what := "a value of another type"
				if t != nil {
					what = "a " + c.TypeToString(t)
				}
				why = fmt.Sprintf("the call at %s reaches %s through %s, not a %s", fl.where(id), id.Text(), what, to)
			}
		})
		if why != "" {
			return nil, why
		}
	}
	// The methods go, and their text goes to the end of to's body.
	pl, _, why := p.MemberPlace(tl.f.FileName(), to, "", "")
	if why != "" {
		return nil, why
	}
	var edits []Edit
	var texts []string
	for _, m := range moved {
		a, b, ok := l.lineRange(m)
		if !ok {
			return nil, fmt.Sprintf("%s shares a line with other code; put it on lines of its own first", memberName(m))
		}
		texts = append(texts, strings.TrimSuffix(textutil.Reindent(l.text[a:b], l.indent(l.line(l.pos(m))), pl.Indent), "\n"))
		edits = append(edits, Edit{File: l.f.FileName(), Start: a, End: b})
	}
	text := strings.Join(texts, "\n\n")
	if pl.Prefix == "" {
		text = "\n" + text
	}
	edits = append(edits, Edit{File: tl.f.FileName(), Start: pl.At, End: pl.End, NewText: pl.Prefix + text + pl.Suffix})
	return edits, ""
}

// extends reports whether the class cls extends base, directly or through
// other classes.
func extends(c *checker.Checker, cls, base *ast.Node) bool {
	seen := map[*ast.Node]bool{}
	for cls != nil && !seen[cls] {
		seen[cls] = true
		var next *ast.Node
		for _, e := range ast.GetExtendsHeritageClauseElements(cls) {
			if s := c.GetSymbolAtLocation(e.Expression()); s != nil {
				for _, d := range c.SkipAlias(s).Declarations {
					if ast.IsClassDeclaration(d) || ast.IsClassExpression(d) {
						next = d
					}
				}
			}
		}
		if next == base {
			return true
		}
		cls = next
	}
	return false
}
