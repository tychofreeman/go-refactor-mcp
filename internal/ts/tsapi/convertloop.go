package tsapi

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/checker"
)

// looped is a loop being converted: the file, the loop, the loop with its
// labels (whole) and the checker.
type looped struct {
	*lined
	c     *checker.Checker
	loop  *ast.Node
	whole *ast.Node
	notes []string
}

// ConvertLoop returns the edits that rewrite the loop on line as the
// statement form to (for_each, for, while or do_while), with what it
// became and notes; or why it cannot.
func (p *Project) ConvertLoop(ctx context.Context, path string, line, column int, to string) ([]Edit, string, string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, "", why
	}
	if line < 1 || line > l.count() {
		return nil, "", fmt.Sprintf("line %d out of range: the file has %d lines", line, l.count())
	}
	var found []*ast.Node
	inspect(l.f.AsNode(), func(n *ast.Node) bool {
		if ast.IsIterationStatement(n, false) && l.line(l.pos(n)) == line && (column == 0 || l.pos(n)-l.start(line)+1 == column) {
			found = append(found, n)
		}
		return true
	})
	switch len(found) {
	case 0:
		return nil, "", fmt.Sprintf("no loop starts on line %d: %s", line, strings.TrimSpace(l.text[l.start(line):l.end(line)]))
	case 1:
	default:
		return nil, "", fmt.Sprintf("line %d has %d loops; give column", line, len(found))
	}
	c, done := p.prog.GetProgram().GetTypeCheckerForFile(ctx, l.f)
	defer done()
	x := &looped{lined: l, c: c, loop: found[0], whole: found[0]}
	for ast.IsLabeledStatement(x.whole.Parent) {
		x.whole = x.whole.Parent
	}
	n := x.loop
	var edits []Edit
	var said string
	switch {
	case ast.IsForStatement(n) && to == "for_each":
		edits, said, why = x.indexToEach()
	case ast.IsForStatement(n) && to == "while":
		edits, said, why = x.forToWhile()
	case ast.IsForOfStatement(n) && to == "for":
		edits, said, why = x.eachToIndex()
	case ast.IsWhileStatement(n) && to == "for":
		edits, said, why = x.whileToFor()
	case ast.IsWhileStatement(n) && to == "do_while":
		edits, said, why = x.whileToDo()
	case ast.IsDoStatement(n) && to == "while":
		edits, said, why = x.doToWhile()
	default:
		return nil, "", fmt.Sprintf("the loop on line %d is %s; convert_loop takes %s", line, loopKind(n), takes(n))
	}
	if why != "" {
		return nil, "", why
	}
	if len(x.notes) > 0 {
		said += "; " + strings.Join(x.notes, "; ")
	}
	return edits, said, ""
}

// loopKind names a loop's form.
func loopKind(n *ast.Node) string {
	switch {
	case ast.IsForStatement(n):
		return "a for"
	case ast.IsForOfStatement(n):
		return "a for...of"
	case ast.IsForInStatement(n):
		return "a for...in"
	case ast.IsWhileStatement(n):
		return "a while"
	}
	return "a do...while"
}

// takes says which forms a loop of n's form converts to.
func takes(n *ast.Node) string {
	switch {
	case ast.IsForStatement(n):
		return "a for to for_each (an index loop over an array) or while"
	case ast.IsForOfStatement(n):
		return "a for...of over an array to for"
	case ast.IsForInStatement(n):
		return "no for...in"
	case ast.IsWhileStatement(n):
		return "a while to for or do_while"
	}
	return "a do...while to while"
}

// indexToEach turns for (let i = 0; i < xs.length; i++) over an array
// into for (const x of xs): each xs[i] becomes the element.
func (x *looped) indexToEach() ([]Edit, string, string) {
	f := x.loop.AsForStatement()
	shape := "the loop is not an index loop over an array, for (let i = 0; i < xs.length; i++)"
	if f.Initializer == nil || !ast.IsVariableDeclarationList(f.Initializer) || f.Initializer.Flags&ast.NodeFlagsLet == 0 {
		return nil, "", shape
	}
	decls := f.Initializer.AsVariableDeclarationList().Declarations.Nodes
	if len(decls) != 1 || !ast.IsIdentifier(decls[0].Name()) || decls[0].Initializer() == nil || decls[0].Initializer().Text() != "0" {
		return nil, "", shape
	}
	i := x.c.GetSymbolAtLocation(decls[0].Name())
	cond := f.Condition
	if cond == nil || !ast.IsBinaryExpression(cond) || cond.AsBinaryExpression().OperatorToken.Kind != ast.KindLessThanToken || !x.is(cond.AsBinaryExpression().Left, i) {
		return nil, "", shape
	}
	bound := cond.AsBinaryExpression().Right
	if !ast.IsPropertyAccessExpression(bound) || bound.Name().Text() != "length" || !ast.IsIdentifier(bound.Expression()) {
		return nil, "", fmt.Sprintf("the loop is bounded by %s; to=for_each takes i < xs.length of a local array. Put it in a local first (extract_variable)", x.source(bound))
	}
	over := bound.Expression()
	if !x.array(over) {
		return nil, "", fmt.Sprintf("%s is not an array", x.source(over))
	}
	if !x.increments(f.Incrementor, i) {
		return nil, "", shape
	}
	body := f.Statement
	if why := x.stable(over, body); why != "" {
		return nil, "", why
	}
	if why := x.unchanged(over, body); why != "" {
		return nil, "", why
	}
	arr := x.c.GetSymbolAtLocation(over)
	var reads []*ast.Node
	why := ""
	identifiers(body, func(id *ast.Node) {
		if why != "" || x.c.GetSymbolAtLocation(id) != i {
			return
		}
		ea := id.Parent
		if ea == nil || !ast.IsElementAccessExpression(ea) || ea.AsElementAccessExpression().ArgumentExpression != id || !x.is(ea.AsElementAccessExpression().Expression, arr) {
			why = fmt.Sprintf("the loop reads i other than as %s[i] (%s), and a for...of has no index", over.Text(), x.where(id))
			return
		}
		reads = append(reads, ea)
	})
	if why != "" {
		return nil, "", why
	}
	var edits []Edit
	kw, name := "const", ""
	if ast.IsBlock(body) && len(body.Statements()) > 0 && ast.IsVariableStatement(body.Statements()[0]) {
		first := body.Statements()[0]
		list := first.AsVariableStatement().DeclarationList
		ds := list.AsVariableDeclarationList().Declarations.Nodes
		if len(ds) == 1 && ast.IsIdentifier(ds[0].Name()) && slices.Contains(reads, ds[0].Initializer()) {
			s := x.c.GetSymbolAtLocation(ds[0].Name())
			if list.Flags&ast.NodeFlagsConst != 0 || x.assigns(body, s, x.c) == "" {
				name = ds[0].Name().Text()
				if list.Flags&ast.NodeFlagsLet != 0 {
					kw = "let"
				}
				edits = append(edits, x.dropLine(first))
				reads = slices.DeleteFunc(reads, func(n *ast.Node) bool { return n == ds[0].Initializer() })
			}
		}
	}
	if name == "" {
		name = x.unused(singular(over.Text()))
	}
	for _, r := range reads {
		edits = append(edits, x.edit(x.pos(r), r.End(), name))
	}
	head := fmt.Sprintf("for (%s %s of %s)", kw, name, over.Text())
	edits = append(edits, x.edit(x.pos(x.loop), x.pos(body), head+" "))
	return edits, "a " + head, ""
}

// eachToIndex turns for (const x of xs) over an array into an index loop,
// the element assigned first in the body.
func (x *looped) eachToIndex() ([]Edit, string, string) {
	f := x.loop.AsForInOrOfStatement()
	if f.AwaitModifier != nil {
		return nil, "", "the loop is a for await...of, which has no index"
	}
	over := f.Expression
	if !x.array(over) {
		return nil, "", fmt.Sprintf("the loop walks %s, which is not an array or a tuple, so it has no index (a Map, a Set or a generator)", x.source(over))
	}
	if !ast.IsIdentifier(over) {
		return nil, "", fmt.Sprintf("the loop walks %s, which a for...of reads once and an index loop on each pass. Put it in a local first (extract_variable)", x.source(over))
	}
	body := f.Statement
	if why := x.stable(over, body); why != "" {
		return nil, "", why
	}
	if !ast.IsVariableDeclarationList(f.Initializer) {
		return nil, "", "the loop assigns a variable declared outside it; declare it in the loop first"
	}
	i := x.unused("i")
	head := fmt.Sprintf("for (let %s = 0; %s < %s.length; %s++)", i, i, over.Text(), i)
	first := x.source(f.Initializer) + " = " + over.Text() + "[" + i + "]" + x.semi()
	edits := x.first(body, first)
	edits = append(edits, x.edit(x.pos(x.loop), x.pos(body), head+" "))
	return edits, "a " + head, ""
}

// forToWhile turns for (init; cond; inc) into init followed by while
// (cond), inc last in the body and before each continue of the loop.
func (x *looped) forToWhile() ([]Edit, string, string) {
	f := x.loop.AsForStatement()
	body := f.Statement
	var declared []*ast.Node
	if f.Initializer != nil && ast.IsVariableDeclarationList(f.Initializer) {
		for _, d := range f.Initializer.AsVariableDeclarationList().Declarations.Nodes {
			identifiers(d.Name(), func(id *ast.Node) { declared = append(declared, id) })
		}
		if f.Initializer.Flags&ast.NodeFlagsBlockScoped != 0 {
			if why := x.notCaptured(declared, "each pass of a for has its own copy of a let it declares, and a while shares one"); why != "" {
				return nil, "", why
			}
		}
	}
	unit := x.unit()
	indent := x.indent(x.line(x.pos(x.whole)))
	inc := ""
	if f.Incrementor != nil {
		inc = x.source(f.Incrementor) + x.semi()
	}
	var inner []Edit
	if inc != "" {
		for _, c := range x.continues() {
			inner = append(inner, x.before(c, inc))
		}
	}
	cond := "true"
	if f.Condition != nil {
		cond = x.source(f.Condition)
	}
	inner = append(inner, x.edit(x.pos(x.loop), x.pos(body), "while ("+cond+") "))
	var text string
	if inc != "" && !terminates(body) {
		text = x.applied(x.pos(x.whole), x.pos(body), inner) + x.withLast(body, inc, inner)
	} else {
		text = x.applied(x.pos(x.whole), x.whole.End(), inner)
	}
	if f.Initializer != nil {
		init := x.source(f.Initializer)
		if !strings.HasSuffix(strings.TrimSpace(init), ";") {
			init += x.semi()
		}
		text = init + "\n" + indent + text
	}
	wrap := !inList(x.whole)
	for _, d := range declared {
		wrap = wrap || x.clashes(d.Text())
	}
	said := "a while (" + cond + ")"
	if wrap {
		lines := strings.Split(text, "\n")
		for k := 1; k < len(lines); k++ {
			if strings.TrimSpace(lines[k]) != "" {
				lines[k] = unit + lines[k]
			}
		}
		text = "{\n" + indent + unit + strings.Join(lines, "\n") + "\n" + indent + "}"
		said += ", in a block with its init"
	}
	return []Edit{x.edit(x.pos(x.whole), x.whole.End(), text)}, said, ""
}

// whileToFor turns a while into a for: the let declared just before it,
// which the condition reads and nothing after the loop does, becomes the
// init, and the body's last assignment or update the incrementor, unless
// a continue of the loop would skip it.
func (x *looped) whileToFor() ([]Edit, string, string) {
	w := x.loop.AsWhileStatement()
	body := w.Statement
	var edits []Edit
	init := ""
	if inList(x.whole) {
		list := x.whole.Parent.Statements()
		at := slices.Index(list, x.whole)
		if at > 0 && ast.IsVariableStatement(list[at-1]) {
			prev := list[at-1]
			dl := prev.AsVariableStatement().DeclarationList
			ds := dl.AsVariableDeclarationList().Declarations.Nodes
			if len(ds) == 1 && ast.IsIdentifier(ds[0].Name()) && ds[0].Initializer() != nil {
				s := x.c.GetSymbolAtLocation(ds[0].Name())
				later := false
				for _, st := range list[at+1:] {
					later = later || x.uses(st, s)
				}
				if x.uses(w.Expression, s) && !later {
					if dl.Flags&ast.NodeFlagsBlockScoped != 0 {
						if why := x.notCaptured([]*ast.Node{ds[0].Name()}, "a while shares one copy of a let declared before it, and each pass of a for has its own"); why != "" {
							return nil, "", why
						}
					}
					init = strings.TrimSuffix(strings.TrimSpace(x.source(prev)), ";")
					edits = append(edits, x.dropLine(prev))
				}
			}
		}
	}
	inc := ""
	if ast.IsBlock(body) && len(body.Statements()) > 0 {
		last := body.Statements()[len(body.Statements())-1]
		if ast.IsExpressionStatement(last) && updates(last.Expression()) {
			if len(x.continues()) == 0 {
				inc = x.source(last.Expression())
				edits = append(edits, x.dropLine(last))
			} else {
				x.notes = append(x.notes, x.source(last)+" stays last in the body: a continue of the loop skips it, and as the incrementor it would run")
			}
		}
	}
	if init == "" && inc == "" {
		return nil, "", "the while has nothing to move into a for's header: no let or const declared just before it that its condition reads and nothing after it does, and no assignment or update last in its body"
	}
	head := "for (" + init + "; " + x.source(w.Expression) + ";" + map[bool]string{true: "", false: " " + inc}[inc == ""] + ")"
	edits = append(edits, x.edit(x.pos(x.loop), x.pos(body), head+" "))
	return edits, "a " + head, ""
}

// doToWhile turns do { body } while (cond) into while (true) { body if
// (!cond) break }.
func (x *looped) doToWhile() ([]Edit, string, string) {
	d := x.loop.AsDoStatement()
	if cs := x.continues(); len(cs) > 0 {
		return nil, "", fmt.Sprintf("the do...while has a continue (%s), which goes to its condition; in a while (true) it would skip it. Rewrite the body without it first (invert_if, remove_else)", x.where(cs[0]))
	}
	neg, _ := x.negate(x.c, d.Expression)
	exit := "if (" + neg + ") break" + x.semi()
	body := d.Statement
	text := "while (true) "
	if terminates(body) {
		text += x.source(body)
	} else {
		text += x.withLast(body, exit, nil)
	}
	return []Edit{x.edit(x.pos(x.loop), x.loop.End(), text)}, "a while (true) that ends in " + exit, ""
}

// whileToDo turns while (cond) body into if (cond) { do body while
// (cond) }: the condition is read twice on the first pass, so it may only
// read locals.
func (x *looped) whileToDo() ([]Edit, string, string) {
	w := x.loop.AsWhileStatement()
	if n := x.notLocal(w.Expression); n != nil {
		return nil, "", fmt.Sprintf("the condition would be read twice on the first pass, by an if and by the do...while, and %s does more than read a local. Put the condition in a local first", x.source(n))
	}
	cond := x.source(w.Expression)
	unit := x.unit()
	indent := x.indent(x.line(x.pos(x.whole)))
	label := x.text[x.pos(x.whole):x.pos(x.loop)]
	inner := label + "do " + x.withLast(w.Statement, "", nil) + " while (" + cond + ")" + x.semi()
	lines := strings.Split(inner, "\n")
	for k := 1; k < len(lines); k++ {
		if strings.TrimSpace(lines[k]) != "" {
			lines[k] = unit + lines[k]
		}
	}
	text := "if (" + cond + ") {\n" + indent + unit + strings.Join(lines, "\n") + "\n" + indent + "}"
	return []Edit{x.edit(x.pos(x.whole), x.whole.End(), text)}, "a do...while under if (" + cond + ")", ""
}

// notLocal returns the first part of e that is not a read of a local, a
// literal or an operator on them, or nil.
func (x *looped) notLocal(e *ast.Node) *ast.Node {
	var bad *ast.Node
	inspect(e, func(n *ast.Node) bool {
		if bad != nil {
			return false
		}
		switch {
		case ast.IsIdentifier(n):
			s := x.c.GetSymbolAtLocation(n)
			if s == nil || s.ValueDeclaration == nil || !(ast.IsVariableDeclaration(s.ValueDeclaration) || ast.IsParameterDeclaration(s.ValueDeclaration)) || ast.IsSourceFile(ast.GetEnclosingBlockScopeContainer(s.ValueDeclaration)) {
				bad = n
			}
		case ast.IsBinaryExpression(n):
			if ast.IsAssignmentOperator(n.AsBinaryExpression().OperatorToken.Kind) {
				bad = n
			}
		case ast.IsPrefixUnaryExpression(n):
			if updates(n) {
				bad = n
			}
		case ast.IsParenthesizedExpression(n), ast.IsNumericLiteral(n), ast.IsStringLiteral(n), n.Kind == ast.KindTrueKeyword, n.Kind == ast.KindFalseKeyword, n.Kind == ast.KindNullKeyword:
		default:
			if ast.IsTokenKind(n.Kind) {
				return false
			}
			bad = n
		}
		return bad == nil
	})
	return bad
}

// updates reports whether e assigns or updates a variable.
func updates(e *ast.Node) bool {
	switch {
	case ast.IsBinaryExpression(e):
		return ast.IsAssignmentOperator(e.AsBinaryExpression().OperatorToken.Kind)
	case ast.IsPostfixUnaryExpression(e):
		return isUpdate(e.AsPostfixUnaryExpression().Operator)
	case ast.IsPrefixUnaryExpression(e):
		return isUpdate(e.AsPrefixUnaryExpression().Operator)
	}
	return false
}

// is reports whether e is an identifier referring to s.
func (x *looped) is(e *ast.Node, s *ast.Symbol) bool {
	return e != nil && ast.IsIdentifier(e) && s != nil && x.c.GetSymbolAtLocation(e) == s
}

// uses reports whether n refers to s.
func (x *looped) uses(n *ast.Node, s *ast.Symbol) bool {
	found := false
	identifiers(n, func(id *ast.Node) { found = found || x.c.GetSymbolAtLocation(id) == s })
	return found
}

// increments reports whether e is i++, ++i or i += 1.
func (x *looped) increments(e *ast.Node, i *ast.Symbol) bool {
	switch {
	case e == nil:
		return false
	case ast.IsPostfixUnaryExpression(e):
		u := e.AsPostfixUnaryExpression()
		return u.Operator == ast.KindPlusPlusToken && x.is(u.Operand, i)
	case ast.IsPrefixUnaryExpression(e):
		u := e.AsPrefixUnaryExpression()
		return u.Operator == ast.KindPlusPlusToken && x.is(u.Operand, i)
	case ast.IsBinaryExpression(e):
		b := e.AsBinaryExpression()
		return b.OperatorToken.Kind == ast.KindPlusEqualsToken && x.is(b.Left, i) && b.Right.Text() == "1"
	}
	return false
}

// array reports whether e's type is an array or a tuple.
func (x *looped) array(e *ast.Node) bool {
	t := x.c.GetTypeAtLocation(e)
	return t != nil && (x.c.IsArrayType(t) || checker.IsTupleType(t))
}

// stable refuses when over can be another array on a later pass: it must
// be a local the body does not assign.
func (x *looped) stable(over, body *ast.Node) string {
	s := x.c.GetSymbolAtLocation(over)
	if s == nil || s.ValueDeclaration == nil || !(ast.IsVariableDeclaration(s.ValueDeclaration) || ast.IsParameterDeclaration(s.ValueDeclaration)) {
		return fmt.Sprintf("the loop walks %s, which is not a local; a for...of reads it once and an index loop on each pass. Put it in a local first (extract_variable)", over.Text())
	}
	if w := x.assigns(body, s, x.c); w != "" {
		return fmt.Sprintf("the loop assigns %s (%s); a for...of reads it once and an index loop on each pass", over.Text(), w)
	}
	return ""
}

// mutators are the array methods that change it in place.
var mutators = map[string]bool{"push": true, "pop": true, "shift": true, "unshift": true, "splice": true, "sort": true, "reverse": true, "fill": true, "copyWithin": true}

// unchanged refuses a body that changes the elements of over: an index
// loop reads xs[i] where it is used, a for...of at the start of the pass.
func (x *looped) unchanged(over, body *ast.Node) string {
	s := x.c.GetSymbolAtLocation(over)
	why := ""
	inspect(body, func(n *ast.Node) bool {
		if why != "" {
			return false
		}
		var target *ast.Node
		switch {
		case ast.IsBinaryExpression(n) && ast.IsAssignmentOperator(n.AsBinaryExpression().OperatorToken.Kind):
			target = n.AsBinaryExpression().Left
		case ast.IsPrefixUnaryExpression(n) && updates(n):
			target = n.AsPrefixUnaryExpression().Operand
		case ast.IsPostfixUnaryExpression(n) && updates(n):
			target = n.AsPostfixUnaryExpression().Operand
		case ast.IsDeleteExpression(n):
			target = n.Expression()
		case ast.IsCallExpression(n) && ast.IsPropertyAccessExpression(n.Expression()):
			pa := n.Expression()
			if mutators[pa.Name().Text()] && x.is(pa.Expression(), s) {
				why = fmt.Sprintf("the loop changes %s (%s); a for...of reads each element at the start of its pass, an index loop where it is used", over.Text(), x.where(n))
			}
		}
		if target != nil && (ast.IsElementAccessExpression(target) || ast.IsPropertyAccessExpression(target)) && x.is(target.Expression(), s) {
			why = fmt.Sprintf("the loop writes %s (%s); a for...of reads each element at the start of its pass, an index loop where it is used", x.source(target), x.where(n))
		}
		return why == ""
	})
	return why
}

// notCaptured refuses when a function in the loop refers to one of the
// names: the two forms then differ in how many copies there are (why).
func (x *looped) notCaptured(names []*ast.Node, why string) string {
	syms := symbols(x.c, names)
	out := ""
	inspect(x.loop, func(n *ast.Node) bool {
		if out != "" {
			return false
		}
		if ast.IsFunctionLike(n) {
			if u := uses(x.c, n, syms); u != nil {
				out = fmt.Sprintf("a function (%s) captures %s; %s. Copy it into a local in the body first", x.where(n), u.Text(), why)
			}
			return false
		}
		return true
	})
	return out
}

// continues returns the continue statements that go to the next pass of
// the loop: unlabeled ones in no inner loop, and those naming its labels.
func (x *looped) continues() []*ast.Node {
	labels := map[string]bool{}
	for n := x.loop.Parent; n != nil && ast.IsLabeledStatement(n); n = n.Parent {
		labels[n.AsLabeledStatement().Label.Text()] = true
	}
	var out []*ast.Node
	var walk func(n *ast.Node, inner bool)
	walk = func(n *ast.Node, inner bool) {
		n.ForEachChild(func(m *ast.Node) bool {
			switch {
			case ast.IsFunctionLike(m) || ast.IsClassLike(m):
			case ast.IsIterationStatement(m, false):
				walk(m, true)
			case ast.IsContinueStatement(m):
				lab := m.AsContinueStatement().Label
				if (lab == nil && !inner) || (lab != nil && labels[lab.Text()]) {
					out = append(out, m)
				}
			default:
				walk(m, inner)
			}
			return false
		})
	}
	walk(x.loop.Statement(), false)
	return out
}

// before returns the edit that runs stmt just before c, in braces when c
// is not in a block.
func (x *looped) before(c *ast.Node, stmt string) Edit {
	if inList(c) {
		return x.edit(x.pos(c), x.pos(c), stmt+"\n"+x.indent(x.line(x.pos(c))))
	}
	outer := x.indent(x.line(x.pos(c.Parent)))
	inner := outer + x.unit()
	return x.edit(x.pos(c), c.End(), "{\n"+inner+stmt+"\n"+inner+x.source(c)+"\n"+outer+"}")
}

// withLast returns body's source with edits, which fall inside it,
// applied and stmt (when not empty) last: before the closing brace of a
// block that has it on a line of its own, else in a new block.
func (x *looped) withLast(body *ast.Node, stmt string, edits []Edit) string {
	var in []Edit
	for _, e := range edits {
		if e.Start >= x.pos(body) && e.End <= body.End() {
			in = append(in, e)
		}
	}
	text := x.applied(x.pos(body), body.End(), in)
	indent := x.indent(x.line(x.pos(x.whole)))
	unit := x.unit()
	if ast.IsBlock(body) && len(body.Statements()) > 0 {
		last := body.Statements()[len(body.Statements())-1]
		if x.line(body.End()-1) != x.line(last.End()-1) {
			if stmt == "" {
				return text
			}
			close := strings.LastIndex(text, "}")
			lineStart := strings.LastIndex(text[:close], "\n") + 1
			at := x.indent(x.line(x.pos(last)))
			return text[:lineStart] + at + stmt + "\n" + text[lineStart:]
		}
	}
	rest := strings.TrimSpace(text)
	if ast.IsBlock(body) {
		rest = strings.TrimSpace(rest[1 : len(rest)-1])
	}
	var b strings.Builder
	b.WriteString("{\n")
	if rest != "" {
		b.WriteString(indent + unit + rest + "\n")
	}
	if stmt != "" {
		b.WriteString(indent + unit + stmt + "\n")
	}
	b.WriteString(indent + "}")
	return b.String()
}

// first returns the edits that put stmt first in body: on a line of its
// own after the brace of a block, or with the body in a new block.
func (x *looped) first(body *ast.Node, stmt string) []Edit {
	indent := x.indent(x.line(x.pos(x.whole)))
	unit := x.unit()
	if ast.IsBlock(body) && x.line(x.pos(body)) != x.line(body.End()-1) {
		st := body.Statements()
		if len(st) == 0 || x.line(x.pos(st[0])) != x.line(x.pos(body)) {
			at := indent + unit
			if len(st) > 0 {
				at = x.indent(x.line(x.pos(st[0])))
			}
			after := x.pos(body) + 1
			return []Edit{x.edit(after, after, "\n"+at+stmt)}
		}
	}
	rest := strings.TrimSpace(x.source(body))
	if ast.IsBlock(body) {
		rest = strings.TrimSpace(rest[1 : len(rest)-1])
	}
	text := "{\n" + indent + unit + stmt + "\n"
	if rest != "" {
		text += indent + unit + rest + "\n"
	}
	return []Edit{x.edit(x.pos(body), body.End(), text+indent+"}")}
}

// clashes reports whether declaring name before the loop, in its list,
// would redeclare a name of the list or reach a statement after the loop
// that names it.
func (x *looped) clashes(name string) bool {
	if !inList(x.whole) {
		return true
	}
	after := false
	for _, s := range x.whole.Parent.Statements() {
		if s == x.whole {
			after = true
			continue
		}
		for _, d := range declared(s) {
			if d.Text() == name {
				return true
			}
		}
		if after {
			hit := false
			identifiers(s, func(id *ast.Node) { hit = hit || id.Text() == name })
			if hit {
				return true
			}
		}
	}
	return false
}

// unused returns name, or name with a number, that no identifier in the
// file uses.
func (x *looped) unused(name string) string {
	taken := map[string]bool{}
	identifiers(x.f.AsNode(), func(id *ast.Node) { taken[id.Text()] = true })
	if !taken[name] {
		return name
	}
	for k := 2; ; k++ {
		if n := fmt.Sprintf("%s%d", name, k); !taken[n] {
			return n
		}
	}
}

// singular names an element of xs: a plural's singular, a List suffix
// dropped, else item.
func singular(xs string) string {
	s := ""
	switch {
	case strings.HasSuffix(xs, "List") && len(xs) > 4:
		s = xs[:len(xs)-4]
	case strings.HasSuffix(xs, "ies") && len(xs) > 3:
		s = xs[:len(xs)-3] + "y"
	case strings.HasSuffix(xs, "s") && !strings.HasSuffix(xs, "ss") && len(xs) > 1:
		s = xs[:len(xs)-1]
	}
	if s == "" || scannerKeyword(s) {
		return "item"
	}
	return s
}

// scannerKeyword reports whether s is a reserved word.
func scannerKeyword(s string) bool {
	switch s {
	case "break", "case", "catch", "class", "const", "continue", "debugger", "default", "delete", "do", "else", "enum", "export", "extends", "false", "finally", "for", "function", "if", "import", "in", "instanceof", "new", "null", "return", "super", "switch", "this", "throw", "true", "try", "typeof", "var", "void", "while", "with", "let", "yield":
		return true
	}
	return false
}

// dropLine returns the edit that removes the lines of statement s.
func (x *looped) dropLine(s *ast.Node) Edit {
	a, b := x.lines(s)
	return x.edit(x.start(a), x.start(b+1), "")
}

// edit is an edit of this file.
func (x *looped) edit(start, end int, text string) Edit {
	return Edit{File: x.f.FileName(), Start: start, End: end, NewText: text}
}

// applied returns the text from start to end with edits, those inside it,
// applied.
func (x *looped) applied(start, end int, edits []Edit) string {
	var in []Edit
	for _, e := range edits {
		if e.Start >= start && e.End <= end {
			in = append(in, e)
		}
	}
	sort.Slice(in, func(i, j int) bool { return in[i].Start > in[j].Start })
	s := x.text[start:end]
	for _, e := range in {
		s = s[:e.Start-start] + e.NewText + s[e.End-start:]
	}
	return s
}
