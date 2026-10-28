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

// repl replaces the bytes [start, end) of a text with text.
type repl struct {
	start, end int
	text       string
}

// substitute returns the bytes [start, end) of l's text with rs made.
func (l *lined) substitute(start, end int, rs []repl) string {
	rs = slices.Clone(rs)
	sort.Slice(rs, func(i, j int) bool { return rs[i].start > rs[j].start })
	text := l.text[start:end]
	for _, r := range rs {
		if r.start >= start && r.end <= end {
			text = text[:r.start-start] + r.text + text[r.end-start:]
		}
	}
	return text
}

// inliner inlines calls of one function.
type inliner struct {
	p      *Project
	c      *checker.Checker
	ef     *effects
	fl     *lined // the function's file
	fn     *ast.Node
	sym    *ast.Symbol
	params []*ast.Node
	expr   *ast.Node   // the value of a body that is one expression or one return
	stmts  []*ast.Node // otherwise, its statements, but for a last return
	ret    *ast.Node   // that return's value, if any
	this   []*ast.Node // this in the body, not in a function of its own
}

// InlineFunction returns the edits that replace the call of name on line
// (at column, when given) with the function's body, and delete the
// function when nothing else uses it, unless keep; with all, every call in
// the file, the function then named by name alone. deleted says whether
// the function goes.
func (p *Project) InlineFunction(ctx context.Context, path string, line int, name string, column int, keep, all bool) ([]Edit, bool, string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, false, why
	}
	c, done := p.prog.GetProgram().GetTypeChecker(ctx)
	defer done()
	ef := newEffects(c, l)
	var calls []*ast.Node
	isCallee := func(id *ast.Node) *ast.Node {
		x := id
		if ast.IsPropertyAccessExpression(id.Parent) && id.Parent.Name() == id {
			x = id.Parent
		}
		if ast.IsCallExpression(x.Parent) && x.Parent.Expression() == x {
			return x.Parent
		}
		return nil
	}
	var sym *ast.Symbol
	if line > 0 {
		if line > l.count() {
			return nil, false, fmt.Sprintf("line %d out of range: the file has %d lines", line, l.count())
		}
		identifiers(l.f.AsNode(), func(id *ast.Node) {
			start := l.pos(id)
			if id.Text() == name && l.line(start) == line && (column == 0 || start-l.start(line)+1 == column) {
				if call := isCallee(id); call != nil {
					calls = append(calls, call)
				} else if sym == nil && ast.IsDeclarationName(id) {
					sym = c.GetSymbolAtLocation(id)
				}
			}
		})
		switch {
		case len(calls) > 1:
			return nil, false, fmt.Sprintf("%s is called %d times on line %d; give column", name, len(calls), line)
		case len(calls) == 1:
			sym = symbolOf(c, calls[0].Expression())
			if ast.IsPropertyAccessExpression(calls[0].Expression()) {
				sym = symbolOf(c, calls[0].Expression().Name())
			}
		case sym == nil || !all:
			return nil, false, fmt.Sprintf("no call of %s on line %d: %s", name, line, strings.TrimSpace(l.text[l.start(line):l.end(line)]))
		}
	} else {
		if !all {
			return nil, false, "give the line of the call, or all_calls"
		}
		_, _, nameNode, why := l.function(name)
		if why != "" {
			return nil, false, why
		}
		sym = c.GetSymbolAtLocation(nameNode)
	}
	if sym == nil {
		return nil, false, fmt.Sprintf("%s refers to nothing known", name)
	}
	in, why := p.newInliner(c, ef, sym, name)
	if why != "" {
		return nil, false, why
	}
	// Every reference, to know whether the function is left unused.
	var refs []*ast.Node
	for _, f := range p.prog.GetProgram().GetSourceFiles() {
		if f.IsDeclarationFile {
			continue
		}
		identifiers(f.AsNode(), func(id *ast.Node) {
			if !ast.IsDeclarationName(id) && symbolOf(c, id) == sym {
				refs = append(refs, id)
			}
		})
	}
	if all {
		calls = nil
		for _, r := range refs {
			call := isCallee(r)
			if call == nil || ast.GetSourceFileOfNode(r) != l.f {
				continue
			}
			calls = append(calls, call)
		}
		if len(calls) == 0 {
			return nil, false, fmt.Sprintf("%s has no calls in this file", name)
		}
	}
	var edits []Edit
	for i, call := range calls {
		for _, other := range calls[:i] {
			if other.Pos() <= call.Pos() && call.End() <= other.End() || call.Pos() <= other.Pos() && other.End() <= call.End() {
				return nil, false, fmt.Sprintf("the call at %s is inside another (%s); inline one at a time", l.where(call), l.where(other))
			}
		}
		if call.Pos() >= in.fn.Pos() && call.End() <= in.fn.End() && ast.GetSourceFileOfNode(call) == in.fl.f {
			return nil, false, fmt.Sprintf("%s calls itself (%s)", name, l.where(call))
		}
		es, why := in.inline(newLined(ast.GetSourceFileOfNode(call)), call)
		if why != "" {
			return nil, false, why
		}
		edits = append(edits, es...)
	}
	deleted := false
	if !keep && len(refs) == len(calls) {
		decl := in.fn
		if ast.IsArrowFunction(decl) || ast.IsFunctionExpression(decl) {
			decl = decl.Parent.Parent.Parent // declaration, list, statement
		}
		if !ast.HasSyntacticModifier(decl, ast.ModifierFlagsExport) {
			if a, b, ok := in.fl.lineRange(decl); ok {
				a, b = in.fl.withBlank(a, b)
				edits = append(edits, Edit{File: in.fl.f.FileName(), Start: a, End: b})
				deleted = true
			}
		}
	}
	return edits, deleted, ""
}

// newInliner returns the inliner of the function sym names, or why its
// calls cannot be inlined.
func (p *Project) newInliner(c *checker.Checker, ef *effects, sym *ast.Symbol, name string) (*inliner, string) {
	if len(sym.Declarations) != 1 {
		return nil, fmt.Sprintf("%s has %d declarations (overloads); inline the implementation by hand", name, len(sym.Declarations))
	}
	d := sym.Declarations[0]
	fn := d
	if ast.IsVariableDeclaration(d) && d.Initializer() != nil && isConst(d) {
		fn = d.Initializer()
	}
	if !(ast.IsFunctionDeclaration(fn) || ast.IsArrowFunction(fn) || ast.IsFunctionExpression(fn) || ast.IsMethodDeclaration(fn)) || fn.Body() == nil {
		return nil, fmt.Sprintf("%s is not a function with a body", name)
	}
	fl := ef.lined(fn)
	switch {
	case fl.f.IsDeclarationFile:
		return nil, fmt.Sprintf("%s is declared in %s, which has no bodies", name, fl.f.FileName())
	case ast.GetFunctionFlags(fn)&(ast.FunctionFlagsAsync|ast.FunctionFlagsGenerator) != 0:
		return nil, fmt.Sprintf("%s is async or a generator; its body cannot run in the caller's", name)
	case ast.IsMethodDeclaration(fn) && fn.ModifierFlags()&(ast.ModifierFlagsStatic|ast.ModifierFlagsPrivate) == 0 && !ast.IsPrivateIdentifier(fn.Name()):
		return nil, fmt.Sprintf("%s is a method a subclass could override; only static and private methods are inlined", name)
	case fn.TypeParameters() != nil:
		return nil, fmt.Sprintf("%s is generic", name)
	}
	in := &inliner{p: p, c: c, ef: ef, fl: fl, fn: fn, sym: sym, params: fn.Parameters()}
	for _, prm := range in.params {
		switch {
		case ast.IsThisParameter(prm):
			return nil, fmt.Sprintf("%s declares this", name)
		case prm.AsParameterDeclaration().DotDotDotToken != nil:
			return nil, fmt.Sprintf("%s has a rest parameter", name)
		case !ast.IsIdentifier(prm.Name()):
			return nil, fmt.Sprintf("%s destructures a parameter (%s)", name, fl.where(prm))
		}
	}
	root := fn.Body()
	b := &body{l: fl, c: c, labels: map[string]bool{}}
	b.visit(root)
	if b.why != "" {
		return nil, b.why
	}
	var bad string
	var visit func(n *ast.Node, inFunc bool)
	visit = func(n *ast.Node, inFunc bool) {
		switch {
		case ast.IsArrowFunction(n):
		case ast.IsFunctionLike(n), ast.IsClassLike(n):
			inFunc = true
		case n.Kind == ast.KindThisKeyword && !inFunc:
			in.this = append(in.this, n)
		case n.Kind == ast.KindSuperKeyword && !inFunc:
			bad = fmt.Sprintf("%s uses super (%s)", name, fl.where(n))
		case ast.IsIdentifier(n) && symbolOf(c, n) == sym:
			bad = fmt.Sprintf("%s calls itself (%s)", name, fl.where(n))
		}
		n.ForEachChild(func(x *ast.Node) bool { visit(x, inFunc); return false })
	}
	visit(root, false)
	if bad != "" {
		return nil, bad
	}
	if len(in.this) > 0 && !ast.IsMethodDeclaration(fn) && !ast.IsArrowFunction(fn) {
		return nil, fmt.Sprintf("%s uses this, which a call does not give it a value for", name)
	}
	if ast.IsArrowFunction(fn) && len(in.this) > 0 {
		return nil, fmt.Sprintf("%s is an arrow that uses its context's this", name)
	}
	switch {
	case !ast.IsBlock(root):
		in.expr = root
	case len(root.Statements()) == 1 && ast.IsReturnStatement(root.Statements()[0]) && root.Statements()[0].Expression() != nil:
		in.expr = root.Statements()[0].Expression()
	default:
		in.stmts = root.Statements()
		if n := len(in.stmts); n > 0 && ast.IsReturnStatement(in.stmts[n-1]) {
			in.ret = in.stmts[n-1].Expression()
			in.stmts = in.stmts[:n-1]
		}
		for _, r := range b.returns {
			if r.Parent != root || r != root.Statements()[len(root.Statements())-1] {
				return nil, fmt.Sprintf("%s returns early (%s); only a last return can be inlined", name, fl.where(r))
			}
		}
	}
	return in, ""
}

// binding is what a parameter becomes at a call: the argument in its
// place, or a const declared before the call's statement.
type binding struct {
	param *ast.Node
	arg   *ast.Node // nil for undefined
	argL  *lined
	text  string // the argument's source, or "undefined"
	bound bool
}

// inline returns the edits that inline call, in the file l.
func (in *inliner) inline(l *lined, call *ast.Node) ([]Edit, string) {
	c, fl := in.c, in.fl
	name := in.sym.Name
	args := call.Arguments()
	for _, a := range args {
		if ast.IsSpreadElement(a) {
			return nil, fmt.Sprintf("the call at %s spreads its arguments", l.where(call))
		}
	}
	if len(args) > len(in.params) {
		return nil, fmt.Sprintf("the call at %s passes more arguments than %s has parameters", l.where(call), name)
	}
	bodyRoot := in.fn.Body()
	// The names the body sees from outside must mean the same at the call.
	var outer []*ast.Node
	for _, id := range free(c, bodyRoot) {
		if d := symbolOf(c, id).ValueDeclaration; d != nil && ast.GetSourceFileOfNode(d) == fl.f && d.Pos() >= in.fn.Pos() && d.End() <= in.fn.End() {
			continue
		}
		outer = append(outer, id)
	}
	if why := seenAlike(c, l, outer, call); why != "" {
		return nil, fmt.Sprintf("%s cannot be inlined at %s: %s", name, l.where(call), why)
	}
	// The receiver stands for this.
	recv := ""
	if len(in.this) > 0 {
		callee := call.Expression()
		if !ast.IsPropertyAccessExpression(callee) {
			return nil, fmt.Sprintf("the call at %s has no receiver to stand for this", l.where(call))
		}
		r := callee.Expression()
		if re := in.ef.Of(r); r.Kind != ast.KindThisKeyword && (len(in.this) > 1 && !re.CanRepeat() || !re.CanDrop()) {
			return nil, fmt.Sprintf("the receiver %s at %s would be evaluated for each this in the body: %s", l.source(r), l.where(call), re.Why)
		}
		recv = operand(l, r)
	}
	// What each parameter becomes.
	var bs []*binding
	var argNodes []*ast.Node
	for _, a := range args {
		argNodes = append(argNodes, a)
	}
	exprForm := in.expr != nil
	stmt := stmtOf(call)
	for i, prm := range in.params {
		b := &binding{param: prm, argL: l, text: "undefined"}
		if i < len(args) {
			b.arg, b.text = args[i], l.source(args[i])
		} else if init := prm.Initializer(); init != nil {
			if ids := free(c, init); slices.ContainsFunc(ids, func(id *ast.Node) bool {
				d := symbolOf(c, id).ValueDeclaration
				return d != nil && d.Parent == in.fn
			}) {
				return nil, fmt.Sprintf("the default of %s reads another parameter; pass it at %s", prm.Name().Text(), l.where(call))
			}
			b.arg, b.argL, b.text = init, fl, fl.source(init)
		}
		bs = append(bs, b)
		ps := c.GetSymbolAtLocation(prm.Name())
		var useNodes []*ast.Node
		identifiers(bodyRoot, func(id *ast.Node) {
			if symbolOf(c, id) == ps {
				useNodes = append(useNodes, id)
			}
		})
		assigned := assignedIn(c, []*ast.Node{bodyRoot}, ps)
		var ae Effects
		if b.arg != nil {
			ae = in.ef.Of(b.arg)
		}
		constant := ae.CanRepeat() && !ae.Reads && len(ae.Locals) == 0 && !strings.Contains(b.text, "\n")
		if !exprForm {
			// Statements: every parameter is declared first, but for a
			// constant or the same name passed on.
			b.bound = !(constant && !assigned) && !(b.text == prm.Name().Text() && !assigned)
			continue
		}
		if assigned {
			b.bound = true
			continue
		}
		times := Several
		switch {
		case len(useNodes) == 0:
			times = Unused
		case len(useNodes) == 1:
			t, _, why := fl.timesIn(useNodes[0], bodyRoot)
			switch {
			case why != "":
				times = Several
				if !constant {
					b.bound = true
					continue
				}
			case t == Shared:
				times = Several
			default:
				times = t
			}
		}
		var past []*ast.Node
		for j, a := range argNodes {
			if j != i {
				past = append(past, a)
			}
		}
		past = append(past, in.expr)
		if b.arg != nil && in.ef.Check(Move{Expr: b.arg, Past: past, Times: times}) != "" {
			b.bound = true
		}
	}
	// Declarations go before the call's statement, which must run the
	// call first and once.
	var decls []string
	needStmt := !exprForm
	for _, b := range bs {
		needStmt = needStmt || b.bound
	}
	if needStmt {
		if stmt == nil {
			return nil, fmt.Sprintf("the call at %s is in no statement of a block, before which its arguments could be declared", l.where(call))
		}
		if t, place, why := l.timesIn(call, stmt); why != "" || t != Once {
			return nil, fmt.Sprintf("the call at %s is evaluated only sometimes or more than once (in %s), so its arguments cannot be declared before it", l.where(call), place+why)
		}
		before := parts(stmt, l.pos(call), nil)
		for i, b := range bs {
			if !b.bound {
				continue
			}
			if b.arg != nil && b.argL == l {
				var past []*ast.Node
				past = append(past, before...)
				for j, a := range argNodes {
					if j < i && !bs[j].bound {
						past = append(past, a)
					}
				}
				if why := in.ef.Check(Move{Expr: b.arg, Past: past}); why != "" {
					return nil, fmt.Sprintf("the argument %s at %s cannot be declared before its statement: %s", b.text, l.where(call), why)
				}
			}
			pn := b.param.Name().Text()
			if why := taken(c, l, pn, stmt); why != "" {
				return nil, fmt.Sprintf("the parameter %s would be declared at the call: %s", pn, why)
			}
			kw := "const"
			if assignedIn(c, []*ast.Node{bodyRoot}, c.GetSymbolAtLocation(b.param.Name())) {
				kw = "let"
			}
			typ := ""
			if t := b.param.Type(); t != nil {
				typ = ": " + fl.source(t)
			}
			decls = append(decls, kw+" "+pn+typ+" = "+b.text+l.semi())
		}
		if !exprForm {
			for _, s := range in.stmts {
				for _, n := range declared(s) {
					if why := taken(c, l, n.Text(), stmt); why != "" {
						return nil, fmt.Sprintf("%s declares %s, which the call's statement already sees: %s", name, n.Text(), why)
					}
				}
			}
		}
	}
	// The replacements in the body: parameters and this.
	var rs []repl
	for _, b := range bs {
		if b.bound {
			continue
		}
		ps := c.GetSymbolAtLocation(b.param.Name())
		identifiers(bodyRoot, func(id *ast.Node) {
			if symbolOf(c, id) != ps {
				return
			}
			text := b.text
			if b.arg != nil && needsParens(id, b.arg) {
				text = "(" + text + ")"
			}
			if ast.IsShorthandPropertyAssignment(id.Parent) {
				text = id.Text() + ": " + b.text
			}
			rs = append(rs, repl{fl.pos(id), id.End(), text})
		})
	}
	for _, t := range in.this {
		if recv != "this" {
			rs = append(rs, repl{fl.pos(t), t.End(), recv})
		}
	}
	indent := ""
	if stmt != nil {
		indent = l.indent(l.line(l.pos(stmt)))
	}
	var edits []Edit
	if exprForm {
		text := fl.substitute(fl.pos(in.expr), in.expr.End(), rs)
		if needsParens(call, in.expr) || ast.IsObjectLiteralExpression(in.expr) && ast.IsArrowFunction(call.Parent) {
			text = "(" + text + ")"
		}
		edits = append(edits, Edit{File: l.f.FileName(), Start: l.pos(call), End: call.End(), NewText: text})
		if len(decls) > 0 {
			at := l.start(l.line(l.pos(stmt)))
			edits = append(edits, Edit{File: l.f.FileName(), Start: at, End: at, NewText: indent + strings.Join(decls, "\n"+indent) + "\n"})
		}
		slices.SortFunc(edits, func(a, b Edit) int { return a.Start - b.Start })
		return edits, ""
	}
	// A body of statements replaces a statement that is the call, declares
	// or assigns its value, or returns it.
	var shape string
	switch {
	case ast.IsExpressionStatement(stmt) && stmt.Expression() == call:
		shape = "statement"
	case ast.IsVariableStatement(stmt) && len(stmt.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes) == 1 &&
		stmt.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes[0].Initializer() == call,
		ast.IsReturnStatement(stmt) && stmt.Expression() == call,
		ast.IsExpressionStatement(stmt) && ast.IsBinaryExpression(stmt.Expression()) && stmt.Expression().AsBinaryExpression().OperatorToken.Kind == ast.KindEqualsToken &&
			stmt.Expression().AsBinaryExpression().Right == call && ast.IsIdentifier(stmt.Expression().AsBinaryExpression().Left):
		shape = "value"
	default:
		return nil, fmt.Sprintf("%s's body is several statements, so its call at %s must be a statement of its own, a declaration's or assignment's value, or a return", name, l.where(call))
	}
	if shape == "value" && in.ret == nil {
		return nil, fmt.Sprintf("%s returns nothing, but the call at %s uses its result", name, l.where(call))
	}
	a, b, ok := l.lineRange(stmt)
	if !ok {
		return nil, fmt.Sprintf("the call's statement at %s shares its lines with other code", l.where(stmt))
	}
	var out strings.Builder
	out.WriteString(l.text[a:l.start(l.line(l.pos(stmt)))]) // its comments
	for _, d := range decls {
		out.WriteString(indent + d + "\n")
	}
	if len(in.stmts) > 0 {
		s0, s1 := fl.start(fl.line(fl.pos(in.stmts[0]))), fl.start(fl.line(in.stmts[len(in.stmts)-1].End()-1)+1)
		from := fl.indent(fl.line(fl.pos(in.stmts[0])))
		for _, line := range strings.SplitAfter(fl.substitute(s0, s1, rs), "\n") {
			if strings.HasPrefix(line, from) {
				line = indent + line[len(from):]
			}
			out.WriteString(line)
		}
	}
	if in.ret != nil {
		value := fl.substitute(fl.pos(in.ret), in.ret.End(), rs)
		switch {
		case shape == "value":
			if needsParens(call, in.ret) {
				value = "(" + value + ")"
			}
			out.WriteString(indent + l.text[l.pos(stmt):l.pos(call)] + value + l.text[call.End():stmt.End()] + "\n")
		case !in.ef.Of(in.ret).CanDrop():
			if ast.IsObjectLiteralExpression(in.ret) || ast.IsFunctionExpression(in.ret) || ast.IsClassExpression(in.ret) {
				value = "(" + value + ")"
			}
			out.WriteString(indent + value + l.semi() + "\n")
		}
	}
	edits = append(edits, Edit{File: l.f.FileName(), Start: a, End: b, NewText: out.String()})
	return edits, ""
}
