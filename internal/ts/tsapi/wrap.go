package tsapi

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"

	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
)

// Hole is the line of a wrap template where the statements go.
const Hole = "$body"

// Wrap returns the text that replaces lines first-last of the file at
// path, whole statements of one block, [start, end), when they are put as
// they are at the line $body of template, statements around them: an if,
// a loop, a function passed to a call. notes say what their return, break
// and continue do once wrapped.
func (p *Project) Wrap(ctx context.Context, path string, first, last int, template string) (start, end int, text string, notes []string, why string) {
	l, why := p.lined(path)
	if why != "" {
		return 0, 0, "", nil, why
	}
	lines := strings.Split(strings.Trim(template, "\n"), "\n")
	at := slices.IndexFunc(lines, func(s string) bool { return strings.Contains(s, Hole) })
	if strings.Count(template, Hole) != 1 || strings.TrimSpace(lines[at]) != Hole {
		return 0, 0, "", nil, fmt.Sprintf("wrap must have %s on a line of its own, once, where the statements go: \"if (ok) {\\n%s\\n}\"", Hole, Hole)
	}
	_, raw, why := ParseStatements(template)
	if why != "" {
		return 0, 0, "", nil, strings.Replace(why, "content", "wrap", 1)
	}
	chosen, why := l.chosen(first, last)
	if why != "" {
		return 0, 0, "", nil, why
	}
	if why := p.usedAfter(ctx, l, chosen); why != "" {
		return 0, 0, "", nil, why
	}
	start, end = l.start(first), l.start(last+1)
	indent := l.indent(l.line(l.pos(chosen[0])))
	var out []string
	for _, s := range strings.Split(gocode.Reindent(template, indent, raw), "\n") {
		if strings.TrimSpace(s) != Hole {
			out = append(out, s)
			continue
		}
		inner := s[:len(s)-len(strings.TrimLeft(s, " \t"))]
		if len(out) > 0 {
			// A hole written at its block's indent goes a step in.
			prev := out[len(out)-1]
			pi := prev[:len(prev)-len(strings.TrimLeft(prev, " \t"))]
			if t := strings.TrimSpace(prev); t != "" && len(inner) <= len(pi) && strings.ContainsAny(t[len(t)-1:], "{(:>") {
				inner = pi + l.unit()
			}
		}
		out = append(out, strings.TrimSuffix(textutil.Reindent(l.text[start:end], indent, inner), "\n"))
	}
	return start, end, strings.Join(out, "\n") + "\n", wrapNotes(l, template, chosen), ""
}

// BodyLines returns the lines of the statements of the function body whose
// braces are at [blockStart, blockEnd) of the file at path.
func (p *Project) BodyLines(path string, blockStart, blockEnd int) (first, last int, why string) {
	l, why := p.lined(path)
	if why != "" {
		return 0, 0, why
	}
	var stmts []*ast.Node
	l.lists(func(owner *ast.Node, list []*ast.Node) {
		if ast.IsBlock(owner) && l.pos(owner) == blockStart {
			stmts = list
		}
	})
	if len(stmts) == 0 {
		return 0, 0, "the body has no statements to wrap"
	}
	first, _ = l.lines(stmts[0])
	_, last = l.lines(stmts[len(stmts)-1])
	if first == l.line(blockStart) || last == l.line(blockEnd-1) {
		return 0, 0, "the body's statements share a line with its braces; give them lines of their own first"
	}
	return first, last, ""
}

// usedAfter says which name the statements stmts declare is used after
// them, where, wrapped, it would be out of scope.
func (p *Project) usedAfter(ctx context.Context, l *lined, stmts []*ast.Node) string {
	c, done := p.prog.GetProgram().GetTypeChecker(ctx)
	defer done()
	tail := stmts[len(stmts)-1].End()
	var why string
	for _, s := range stmts {
		for _, n := range declared(s) {
			sym := c.GetSymbolAtLocation(n)
			identifiers(s.Parent, func(id *ast.Node) {
				if why == "" && id.Pos() >= tail && symbolOf(c, id) == sym {
					why = fmt.Sprintf("%s is declared in the statements and used after them (%s); wrapped, it would be out of scope there, so declare it before them first", n.Text(), l.where(id))
				}
			})
		}
	}
	return why
}

// wrapNotes says what the return, break and continue of stmts do once at
// the hole of template: inside a function they act on it, inside a loop
// or switch on that.
func wrapNotes(l *lined, template string, stmts []*ast.Node) []string {
	f := parseText("function __wrap() {\n" + template + "\n}\n")
	var outer []*ast.Node // the template's nodes around the hole, outermost first
	var stack []*ast.Node
	var find func(n *ast.Node) bool
	find = func(n *ast.Node) bool {
		if ast.IsExpressionStatement(n) && ast.IsIdentifier(n.Expression()) && n.Expression().Text() == Hole {
			outer = slices.Clone(stack)
			return true
		}
		stack = append(stack, n)
		found := n.ForEachChild(find)
		stack = stack[:len(stack)-1]
		return found
	}
	f.Statements.Nodes[0].Body().ForEachChild(find)
	closure, loop, sw := false, false, false
	for _, n := range outer {
		switch {
		case ast.IsFunctionLike(n) || ast.IsClassLike(n):
			closure, loop, sw = true, false, false
		case ast.IsIterationStatement(n, false):
			loop = true
		case ast.IsSwitchStatement(n):
			sw = true
		}
	}
	var notes []string
	note := func(n *ast.Node, what string) {
		notes = append(notes, fmt.Sprintf("%s at line %d %s", what, l.line(l.pos(n)), map[string]string{
			"return":   "now leaves the new function, not the one it was in",
			"break":    "now leaves the new loop or switch",
			"continue": "now continues the new loop",
		}[what]))
	}
	var visit func(n *ast.Node, inLoop, inSwitch bool)
	visit = func(n *ast.Node, inLoop, inSwitch bool) {
		switch {
		case ast.IsFunctionLike(n) || ast.IsClassLike(n):
			return
		case ast.IsIterationStatement(n, false):
			inLoop = true
		case ast.IsSwitchStatement(n):
			inSwitch = true
		case ast.IsReturnStatement(n) && closure:
			note(n, "return")
		case n.Kind == ast.KindBreakStatement && n.AsBreakStatement().Label == nil && !inLoop && !inSwitch && (loop || sw):
			note(n, "break")
		case n.Kind == ast.KindContinueStatement && n.AsContinueStatement().Label == nil && !inLoop && loop:
			note(n, "continue")
		}
		n.ForEachChild(func(c *ast.Node) bool {
			visit(c, inLoop, inSwitch)
			return false
		})
	}
	for _, s := range stmts {
		visit(s, false, false)
	}
	return notes
}
