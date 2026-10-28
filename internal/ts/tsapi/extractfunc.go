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

// chosen returns the statements of one list that lines first-last hold
// whole, besides comments and blank lines, or why they do not.
func (l *lined) chosen(first, last int) ([]*ast.Node, string) {
	if first < 1 || last < first || last > l.count() {
		return nil, fmt.Sprintf("lines %d-%d are not a range of lines of the file, which has %d", first, last, l.count())
	}
	var chosen, over []*ast.Node
	l.lists(func(_ *ast.Node, list []*ast.Node) {
		var in, o []*ast.Node
		for _, s := range list {
			a, b := l.lines(s)
			if a >= first && b <= last {
				in = append(in, s)
			}
			if a <= last && b >= first {
				o = append(o, s)
			}
		}
		if chosen == nil && len(in) > 0 {
			chosen = in
		}
		if len(o) > 0 {
			over = o
		}
	})
	if !l.whole(chosen, first, last) {
		msg := fmt.Sprintf("lines %d-%d do not hold whole statements of one block", first, last)
		if len(over) > 0 {
			a, _ := l.lines(over[0])
			_, b := l.lines(over[len(over)-1])
			if (a != first || b != last) && l.whole(over, a, b) {
				msg += fmt.Sprintf("; lines %d-%d do", a, b)
			}
		}
		return nil, msg
	}
	return chosen, ""
}

// moduleLevel reports whether the declaration d is of the module, not of
// a function or block.
func moduleLevel(d *ast.Node) bool {
	for x := d; x.Parent != nil; x = x.Parent {
		p := x.Parent
		switch {
		case ast.IsSourceFile(p):
			return true
		case ast.IsBlock(p), ast.IsFunctionLike(p), ast.IsForStatement(p), ast.IsForInOrOfStatement(p), ast.IsCaseBlock(p), ast.IsCatchClause(p), ast.IsClassLike(p):
			return false
		}
	}
	return false
}

// assignedIn reports whether code in nodes assigns s.
func assignedIn(c *checker.Checker, nodes []*ast.Node, s *ast.Symbol) bool {
	found := false
	for _, n := range nodes {
		var visit func(x *ast.Node) bool
		visit = func(x *ast.Node) bool {
			var target *ast.Node
			switch {
			case ast.IsBinaryExpression(x) && ast.IsAssignmentOperator(x.AsBinaryExpression().OperatorToken.Kind):
				target = x.AsBinaryExpression().Left
			case ast.IsPrefixUnaryExpression(x) && isUpdate(x.AsPrefixUnaryExpression().Operator):
				target = x.AsPrefixUnaryExpression().Operand
			case ast.IsPostfixUnaryExpression(x):
				target = x.AsPostfixUnaryExpression().Operand
			case ast.IsForInOrOfStatement(x):
				target = x.AsForInOrOfStatement().Initializer
			}
			if target != nil && !ast.IsVariableDeclarationList(target) && slices.Contains(symbolsOf(c, target), s) {
				found = true
			}
			if !found {
				x.ForEachChild(visit)
			}
			return found
		}
		visit(n)
	}
	return found
}

// body is what a run of statements does that a function made of them
// must keep: awaits, this, returns, and jumps out of them.
type body struct {
	l                   *lined
	c                   *checker.Checker
	async, this         bool
	returns             []*ast.Node
	why                 string
	loops, switches     int
	labels              map[string]bool
	inFunction, inArrow int
}

func (b *body) visit(n *ast.Node) {
	if b.why != "" {
		return
	}
	plain := b.inFunction == 0 && b.inArrow == 0
	switch {
	case ast.IsArrowFunction(n):
		b.inArrow++
		n.ForEachChild(func(x *ast.Node) bool { b.visit(x); return false })
		b.inArrow--
		return
	case ast.IsFunctionLike(n), ast.IsClassLike(n):
		b.inFunction++
		n.ForEachChild(func(x *ast.Node) bool { b.visit(x); return false })
		b.inFunction--
		return
	case ast.IsAwaitExpression(n) || ast.IsForOfStatement(n) && n.AsForInOrOfStatement().AwaitModifier != nil:
		if plain {
			b.async = true
		}
	case ast.IsYieldExpression(n):
		if plain {
			b.why = fmt.Sprintf("%s yields, which a function of its own cannot do for the generator", b.l.part(n))
		}
	case n.Kind == ast.KindThisKeyword, n.Kind == ast.KindSuperKeyword:
		if b.inFunction == 0 {
			b.this = true
		}
	case ast.IsIdentifier(n) && n.Text() == "arguments" && b.inFunction == 0:
		if s := b.c.GetSymbolAtLocation(n); s == nil || s.ValueDeclaration == nil {
			b.why = fmt.Sprintf("%s reads arguments, which would then be the new function's", b.l.part(n))
		}
	case ast.IsReturnStatement(n):
		if plain {
			b.returns = append(b.returns, n)
		}
	case ast.IsBreakOrContinueStatement(n) && plain:
		label := n.Label()
		switch {
		case label != nil && b.labels[label.Text()]:
		case label == nil && n.Kind == ast.KindBreakStatement && b.loops+b.switches > 0:
		case label == nil && n.Kind == ast.KindContinueStatement && b.loops > 0:
		default:
			b.why = fmt.Sprintf("%s jumps out of the lines", b.l.part(n))
		}
	case ast.IsLabeledStatement(n):
		b.labels[n.AsLabeledStatement().Label.Text()] = true
	}
	loop, sw := ast.IsIterationStatement(n, false), ast.IsSwitchStatement(n)
	if loop && plain {
		b.loops++
	}
	if sw && plain {
		b.switches++
	}
	n.ForEachChild(func(x *ast.Node) bool { b.visit(x); return false })
	if loop && plain {
		b.loops--
	}
	if sw && plain {
		b.switches--
	}
}

// typeParamsUsed returns the type parameters of the functions (and the
// class, when the new code is not its method) around stmts that the
// statements or the new parameters' types name: the new function declares
// them.
func (l *lined) typeParamsUsed(c *checker.Checker, stmts []*ast.Node, params []string, member *ast.Node) []*ast.Node {
	var out []*ast.Node
	for n := stmts[0].Parent; n != nil; n = n.Parent {
		if !(ast.IsFunctionLike(n) || ast.IsClassLike(n)) || member != nil && n == member.Parent {
			continue
		}
		for _, tp := range n.TypeParameters() {
			name := tp.Name().Text()
			used := slices.ContainsFunc(params, func(p string) bool { return containsWord(p, name) })
			for _, s := range stmts {
				identifiers(s, func(id *ast.Node) {
					if !used && id.Text() == name {
						if sym := c.GetSymbolAtLocation(id); sym != nil && slices.Contains(sym.Declarations, tp) {
							used = true
						}
					}
				})
			}
			if used {
				out = append([]*ast.Node{tp}, out...)
			}
		}
	}
	return out
}

// containsWord reports whether s holds word as a whole identifier.
func containsWord(s, word string) bool {
	for i := strings.Index(s, word); i >= 0; {
		before := i == 0 || !scanner.IsIdentifierPart(rune(s[i-1]))
		after := i+len(word) == len(s) || !scanner.IsIdentifierPart(rune(s[i+len(word)]))
		if before && after {
			return true
		}
		j := strings.Index(s[i+1:], word)
		if j < 0 {
			return false
		}
		i += 1 + j
	}
	return false
}

// ExtractFunction returns the edits that move the statements on lines
// first-last into a new function called name, declared after the
// function they are in (a method of its class when they use this), and
// call it in their place. Locals they read become parameters; those they
// declare or assign that are used elsewhere come back as its result.
func (p *Project) ExtractFunction(ctx context.Context, path string, first, last int, name, doc string) ([]Edit, string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, why
	}
	stmts, why := l.chosen(first, last)
	if why != "" {
		return nil, why
	}
	c, done := p.prog.GetProgram().GetTypeCheckerForFile(ctx, l.f)
	defer done()
	start, end := l.pos(stmts[0]), stmts[len(stmts)-1].End()
	in := func(n *ast.Node) bool {
		return ast.GetSourceFileOfNode(n) == l.f && n.Pos() >= stmts[0].Pos() && n.End() <= end
	}
	b := &body{l: l, c: c, labels: map[string]bool{}}
	for _, s := range stmts {
		b.visit(s)
	}
	if b.why != "" {
		return nil, b.why
	}
	owner := stmts[0].Parent
	if ast.IsSourceFile(owner) {
		for _, s := range stmts {
			if names := declared(s); len(names) > 0 {
				return nil, fmt.Sprintf("%s declares %s at the module's top level, which a function would hide", l.part(s), names[0].Text())
			}
		}
	}
	fn := ast.GetContainingFunction(stmts[0])
	// Lines that end the function's body, or that end with a return, may
	// keep their returns: the call becomes return f(...).
	endsBody := fn != nil && owner == fn.Body() && owner.Statements()[len(owner.Statements())-1] == stmts[len(stmts)-1] || ast.IsReturnStatement(stmts[len(stmts)-1])
	// Lines that return before the function's end come back as a value
	// the call returns unless it is undefined (each return has one that
	// cannot be), or as whether they returned (each return is bare).
	early := ""
	if len(b.returns) > 0 && !endsBody {
		for _, r := range b.returns {
			switch e := r.Expression(); {
			case e == nil && early != "value":
				early = "bare"
			case e != nil && early != "bare":
				if mayBeUndefined(c.GetTypeAtLocation(e)) {
					return nil, fmt.Sprintf("%s returns what may be undefined, so a call could not tell it from the lines going on; only lines that end the function's body may return it", l.part(r))
				}
				early = "value"
			default:
				return nil, fmt.Sprintf("the lines return a value in some places and nothing in others (%s)", l.part(r))
			}
		}
	}

	// Where the new function goes: after the class member, for a method;
	// else after the top-level statement the lines are in.
	var member, top *ast.Node
	if b.this {
		member = ast.GetThisContainer(stmts[0], false, false)
		if member == nil || member.Parent == nil || !ast.IsClassLike(member.Parent) || !(ast.IsMethodDeclaration(member) || ast.IsConstructorDeclaration(member) || ast.IsAccessor(member)) {
			return nil, "the lines use this or super outside a method of a class, which a new method could not take"
		}
		top = member
	} else {
		for top = stmts[len(stmts)-1]; top.Parent != nil && !ast.IsSourceFile(top.Parent); top = top.Parent {
		}
	}
	if member != nil {
		for _, m := range member.Parent.Members() {
			if m.Name() != nil && m.Name().Text() == name {
				return nil, fmt.Sprintf("%s already has a member %s (%s)", member.Parent.Name().Text(), name, l.where(m))
			}
		}
		if !scanner.IsIdentifierText(name, 0) {
			return nil, fmt.Sprintf("%q is not a valid name", name)
		}
	} else if why := taken(c, l, name, top); why != "" {
		return nil, why
	}

	// The locals the lines use, in the order they first do.
	type local struct {
		sym                  *ast.Symbol
		first                *ast.Node
		declared, assigned   bool
		usedOutside, keyword string
	}
	var locals []*local
	byS := map[*ast.Symbol]*local{}
	for _, s := range stmts {
		var bad string
		identifiers(s, func(id *ast.Node) {
			if bad != "" {
				return
			}
			sym := symbolOf(c, id)
			if sym == nil || len(sym.Declarations) == 0 {
				return
			}
			d := sym.ValueDeclaration
			if d == nil {
				d = sym.Declarations[0]
			}
			if ast.GetSourceFileOfNode(d) != l.f || moduleLevel(d) || ast.IsPropertyDeclaration(d) || ast.IsMethodDeclaration(d) || ast.IsPropertySignatureDeclaration(d) ||
				ast.IsPropertyAssignment(d) || ast.IsShorthandPropertyAssignment(d) || ast.IsEnumMember(d) || ast.IsAccessor(d) {
				return
			}
			if sym.Flags&ast.SymbolFlagsTypeParameter != 0 {
				return // declared on the new function, below
			}
			if sym.Flags&ast.SymbolFlagsValue == 0 {
				if !in(d) {
					bad = fmt.Sprintf("the lines use the type %s declared in the function (%s)", id.Text(), l.where(d))
				}
				return
			}
			v := byS[sym]
			if v == nil {
				v = &local{sym: sym, first: id, declared: in(d)}
				byS[sym] = v
				locals = append(locals, v)
			}
		})
		if bad != "" {
			return nil, bad
		}
	}
	var params, args []string
	var outs []*local
	for _, v := range locals {
		d := v.sym.ValueDeclaration
		if d == nil {
			d = v.sym.Declarations[0]
		}
		// Where the local is used outside the lines.
		scope := d.Parent
		for scope != nil && !(ast.IsBlock(scope) || ast.IsFunctionLike(scope) || ast.IsSourceFile(scope) || ast.IsForStatement(scope) || ast.IsForInOrOfStatement(scope) || ast.IsCaseBlock(scope)) {
			scope = scope.Parent
		}
		identifiers(scope, func(id *ast.Node) {
			if v.usedOutside == "" && !in(id) && id != d.Name() && symbolOf(c, id) == v.sym {
				v.usedOutside = l.where(id)
			}
		})
		v.assigned = !v.declared && assignedIn(c, stmts, v.sym)
		if !v.declared {
			t := c.GetTypeAtLocation(v.first)
			if v.assigned {
				t = c.GetTypeOfSymbol(v.sym)
			}
			params = append(params, v.first.Text()+": "+c.TypeToString(t))
			args = append(args, v.first.Text())
		}
		// After lines that end with a return, nothing of theirs is read.
		if v.usedOutside != "" && (v.declared || v.assigned) && !ast.IsReturnStatement(stmts[len(stmts)-1]) {
			if v.declared {
				if !slices.ContainsFunc(stmts, func(s *ast.Node) bool { return s == d.Parent.Parent || s == d }) && !(ast.IsVariableDeclaration(d) && d.Parent.Flags&ast.NodeFlagsBlockScoped == 0) {
					return nil, fmt.Sprintf("%s is declared inside a block of the lines (%s) and used after them (%s)", v.first.Text(), l.where(d), v.usedOutside)
				}
				v.keyword = "const"
				if ast.IsVariableDeclaration(d) {
					switch {
					case d.Parent.Flags&ast.NodeFlagsLet != 0:
						v.keyword = "let"
					case d.Parent.Flags&ast.NodeFlagsConst == 0:
						v.keyword = "var"
					}
				}
			}
			outs = append(outs, v)
		}
	}
	if len(outs) > 0 && len(b.returns) > 0 {
		return nil, fmt.Sprintf("the lines return, and set %s, which is used after them", outs[0].first.Text())
	}
	declaredOuts, assignedOuts := 0, 0
	for _, o := range outs {
		if o.declared {
			declaredOuts++
		} else {
			assignedOuts++
		}
	}
	if declaredOuts > 0 && assignedOuts > 0 {
		return nil, fmt.Sprintf("the lines declare %s and assign %s, which are both used after them; extract fewer lines", outs[slices.IndexFunc(outs, func(o *local) bool { return o.declared })].first.Text(), outs[slices.IndexFunc(outs, func(o *local) bool { return !o.declared })].first.Text())
	}

	// The call, in place of the lines.
	indent := l.indent(l.line(start))
	call := name + "(" + strings.Join(args, ", ") + ")"
	if member != nil {
		call = "this." + call
	}
	if b.async {
		call = "await " + call
	}
	result := ""
	var names []string
	for _, o := range outs {
		names = append(names, o.first.Text())
	}
	keyword := "const"
	for _, o := range outs {
		if o.keyword == "let" || o.keyword == "var" && keyword == "const" {
			keyword = o.keyword
		}
	}
	var rs []repl // what changes in the lines
	switch {
	case early == "value":
		r := ""
		for _, cand := range []string{"result", name + "Result", "returned"} {
			if taken(c, l, cand, stmts[0]) == "" && !strings.Contains(l.text[start:end], cand) {
				r = cand
				break
			}
		}
		if r == "" {
			return nil, "no name is free for what the lines return; rename a local result first"
		}
		call = "const " + r + " = " + call + l.semi() + "\n" + indent + "if (" + r + " !== undefined) {\n" + indent + l.unit() + "return " + r + l.semi() + "\n" + indent + "}"
		result = "return undefined" + l.semi()
	case early == "bare":
		call = "if (" + call + ") {\n" + indent + l.unit() + "return" + l.semi() + "\n" + indent + "}"
		result = "return false" + l.semi()
		for _, r := range b.returns {
			rs = append(rs, repl{l.pos(r), l.pos(r) + len("return"), "return true"})
		}
	case len(b.returns) > 0:
		call = "return " + call + l.semi()
	case len(outs) == 0:
		call += l.semi()
	case len(outs) == 1:
		result = "return " + names[0] + l.semi()
		if declaredOuts == 1 {
			call = keyword + " " + names[0] + " = " + call + l.semi()
		} else {
			call = names[0] + " = " + call + l.semi()
		}
	default:
		result = "return { " + strings.Join(names, ", ") + " }" + l.semi()
		if declaredOuts > 0 {
			call = keyword + " { " + strings.Join(names, ", ") + " } = " + call + l.semi()
		} else {
			call = "({ " + strings.Join(names, ", ") + " } = " + call + ")" + l.semi()
		}
	}
	lineStart, lineEnd := l.start(first), l.start(last+1)
	edits := []Edit{{File: l.f.FileName(), Start: lineStart, End: lineEnd, NewText: indent + call + "\n"}}

	// The new function.
	ind := l.indent(l.line(l.pos(top)))
	unit := l.unit()
	text := l.reindent(lineStart, lineEnd, indent, ind+unit, rs...)
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if result != "" {
		text += ind + unit + result + "\n"
	}
	head := "function " + name
	if tps := l.typeParamsUsed(c, stmts, params, member); len(tps) > 0 {
		var srcs []string
		for _, tp := range tps {
			srcs = append(srcs, l.source(tp))
		}
		name += "<" + strings.Join(srcs, ", ") + ">"
		head += "<" + strings.Join(srcs, ", ") + ">"
	}
	if member != nil {
		head = "private " + name
		if ast.IsStatic(member) {
			head = "private static " + name
		}
	}
	if b.async {
		head = strings.Replace(head, name, "async "+name, 1)
		if member == nil {
			head = "async " + "function " + name
		}
	}
	decl := ""
	if doc != "" {
		doc = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(doc), "/**"), "*/"))
		if strings.Contains(doc, "\n") {
			decl += ind + "/**\n"
			for _, line := range strings.Split(doc, "\n") {
				decl += strings.TrimRight(ind+" * "+strings.TrimSpace(line), " ") + "\n"
			}
			decl += ind + " */\n"
		} else {
			decl += ind + "/** " + doc + " */\n"
		}
	}
	decl += ind + head + "(" + strings.Join(params, ", ") + ") {\n" + text + ind + "}\n"
	_, topEnd := l.lines(top)
	at := l.start(topEnd + 1)
	if at == lineEnd || at > lineStart && at < lineEnd {
		edits[0].NewText += "\n" + decl
		return edits, ""
	}
	prefix := "\n"
	if at == len(l.text) && !strings.HasSuffix(l.text, "\n") {
		prefix = "\n\n"
	}
	edits = append(edits, Edit{File: l.f.FileName(), Start: at, End: at, NewText: prefix + decl})
	slices.SortFunc(edits, func(a, b Edit) int { return a.Start - b.Start })
	return edits, ""
}
