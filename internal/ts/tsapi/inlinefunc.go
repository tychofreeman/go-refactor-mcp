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
	sort.Slice(rs, func(i, j int) bool { // from the end; at one place, what is inserted there last
		if rs[i].start != rs[j].start {
			return rs[i].start > rs[j].start
		}
		return rs[i].end > rs[j].end
	})
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
	expr   *ast.Node   // the value of a body that is one expression or one return, or the last return's of guards
	conds  []*ast.Node // guards: a body of if (conds[i]) return vals[i]; and a last return
	vals   []*ast.Node
	early  []*ast.Node // returns before the last, for a call that is a return's whole value
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
			if call := isCallee(r); call != nil {
				calls = append(calls, call)
			}
		}
		if len(calls) == 0 {
			return nil, false, fmt.Sprintf("%s has no calls", name)
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
	// Another file's import of the function goes when its calls do.
	gone := map[*ast.Node]bool{}
	files := map[*ast.SourceFile]bool{}
	for _, call := range calls {
		callee := call.Expression()
		if ast.IsPropertyAccessExpression(callee) {
			callee = callee.Name()
		}
		gone[callee] = true
		if f := ast.GetSourceFileOfNode(call); f != in.fl.f {
			files[f] = true
		}
	}
	for f := range files {
		edits = append(edits, dropImports(c, newLined(f), func(n *ast.Node) bool { return gone[n] }, true)...)
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
				// Its overload signatures go with it.
				for _, o := range in.sym.Declarations {
					if o != decl && ast.GetSourceFileOfNode(o) == in.fl.f {
						if oa, ob, ok := in.fl.lineRange(o); ok {
							edits = append(edits, Edit{File: in.fl.f.FileName(), Start: oa, End: ob})
						}
					}
				}
			}
		}
	}
	return edits, deleted, ""
}

// newInliner returns the inliner of the function sym names, or why its
// calls cannot be inlined.
func (p *Project) newInliner(c *checker.Checker, ef *effects, sym *ast.Symbol, name string) (*inliner, string) {
	// Of overloads, the implementation is what runs.
	var impl []*ast.Node
	for _, d := range sym.Declarations {
		if (ast.IsFunctionDeclaration(d) || ast.IsMethodDeclaration(d)) && d.Body() == nil {
			continue
		}
		impl = append(impl, d)
	}
	if len(impl) != 1 {
		return nil, fmt.Sprintf("%s has %d declarations; inline the implementation by hand", name, len(sym.Declarations))
	}
	d := impl[0]
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
	}
	in := &inliner{p: p, c: c, ef: ef, fl: fl, fn: fn, sym: sym, params: fn.Parameters()}
	for _, prm := range in.params {
		switch {
		case ast.IsThisParameter(prm):
			return nil, fmt.Sprintf("%s declares this", name)
		case prm.AsParameterDeclaration().DotDotDotToken != nil && !ast.IsIdentifier(prm.Name()):
			return nil, fmt.Sprintf("%s destructures its rest parameter (%s)", name, fl.where(prm))
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
	// A generic function is inlined when its body does not name its type
	// parameters, which the call would not see.
	for _, id := range identifiersOf(root) {
		if in.typeParam(id) {
			return nil, fmt.Sprintf("%s's body uses its type parameter %s (%s)", name, id.Text(), fl.where(id))
		}
	}
	switch {
	case !ast.IsBlock(root):
		in.expr = root
	case len(root.Statements()) == 1 && ast.IsReturnStatement(root.Statements()[0]) && root.Statements()[0].Expression() != nil:
		in.expr = root.Statements()[0].Expression()
	default:
		if conds, vals, last, ok := guards(root.Statements()); ok {
			in.conds, in.vals, in.expr = conds, vals, last
			break
		}
		in.stmts = root.Statements()
		if n := len(in.stmts); n > 0 && ast.IsReturnStatement(in.stmts[n-1]) {
			in.ret = in.stmts[n-1].Expression()
			in.stmts = in.stmts[:n-1]
		}
		for _, r := range b.returns {
			if r.Parent != root || r != root.Statements()[len(root.Statements())-1] {
				in.early = append(in.early, r)
			}
		}
	}
	return in, ""
}

// guards splits statements of the form if (c) return a; ... return d; (no
// else, one return a branch) into the conditions, their values and d.
func guards(stmts []*ast.Node) (conds, vals []*ast.Node, last *ast.Node, ok bool) {
	n := len(stmts)
	if n < 2 || !ast.IsReturnStatement(stmts[n-1]) || stmts[n-1].Expression() == nil {
		return nil, nil, nil, false
	}
	for _, s := range stmts[:n-1] {
		if !ast.IsIfStatement(s) || s.AsIfStatement().ElseStatement != nil {
			return nil, nil, nil, false
		}
		then := s.AsIfStatement().ThenStatement
		if ast.IsBlock(then) {
			if len(then.Statements()) != 1 {
				return nil, nil, nil, false
			}
			then = then.Statements()[0]
		}
		if !ast.IsReturnStatement(then) || then.Expression() == nil {
			return nil, nil, nil, false
		}
		conds, vals = append(conds, s.AsIfStatement().Expression), append(vals, then.Expression())
	}
	return conds, vals, stmts[n-1].Expression(), true
}

// identifiersOf returns the identifiers in n.
func identifiersOf(n *ast.Node) []*ast.Node {
	var out []*ast.Node
	identifiers(n, func(id *ast.Node) { out = append(out, id) })
	return out
}

// typeParam reports whether id names one of the function's type
// parameters.
func (in *inliner) typeParam(id *ast.Node) bool {
	s := in.c.GetSymbolAtLocation(id)
	if s == nil {
		return false
	}
	for _, tp := range in.fn.TypeParameters() {
		if slices.Contains(s.Declarations, tp) {
			return true
		}
	}
	return false
}

// bindingNames returns the names a binding pattern declares.
func bindingNames(pattern *ast.Node) []*ast.Node {
	var out []*ast.Node
	identifiers(pattern, func(id *ast.Node) {
		if id.Parent != nil && ast.IsBindingElement(id.Parent) && id.Parent.Name() == id {
			out = append(out, id)
		}
	})
	return out
}

// ternaryPart returns the text of e as a part of a conditional
// expression: its condition, or one of its values.
func ternaryPart(fl *lined, e *ast.Node, rs []repl, cond bool) string {
	s := fl.substitute(fl.pos(e), e.End(), rs)
	prec := ast.GetExpressionPrecedence(e)
	if cond && prec <= ast.OperatorPrecedenceConditional || prec <= ast.OperatorPrecedenceComma {
		s = "(" + s + ")"
	}
	return s
}

// binding is what a parameter becomes at a call: the argument in its
// place, or a const declared before the call's statement.
type binding struct {
	param *ast.Node
	arg   *ast.Node // nil for undefined
	argL  *lined
	text  string // the argument's source, or "undefined"
	bound bool
	name  string      // when bound under a name other than the parameter's
	rest  []*ast.Node // for a rest parameter, the arguments its array holds
}

// identifierTexts returns the names of the identifiers in n.
func identifierTexts(n *ast.Node) []string {
	var out []string
	identifiers(n, func(id *ast.Node) { out = append(out, id.Text()) })
	return out
}

// mayBeUndefined reports whether a value of t may be undefined (or is not
// known: any, unknown, void).
func mayBeUndefined(t *checker.Type) bool {
	if t == nil {
		return true
	}
	types := []*checker.Type{t}
	if t.Flags()&checker.TypeFlagsUnion != 0 {
		types = t.Types()
	}
	for _, u := range types {
		if u.Flags()&(checker.TypeFlagsUndefined|checker.TypeFlagsVoid|checker.TypeFlagsAnyOrUnknown) != 0 {
			return true
		}
	}
	return false
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
	if n := len(in.params); len(args) > n && (n == 0 || in.params[n-1].AsParameterDeclaration().DotDotDotToken == nil) {
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
		if prm.AsParameterDeclaration().DotDotDotToken != nil {
			// A rest parameter is the array of what is left.
			var texts []string
			b.rest = args[min(i, len(args)):]
			for _, a := range b.rest {
				texts = append(texts, l.source(a))
			}
			b.arg, b.argL, b.text, b.bound = nil, l, "["+strings.Join(texts, ", ")+"]", true
			continue
		}
		if !ast.IsIdentifier(prm.Name()) {
			b.bound = true // a pattern: declared as it is
			continue
		}
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
		if len(in.conds) > 0 {
			past = append(past, bodyRoot)
		} else {
			past = append(past, in.expr)
		}
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
			moved := b.rest
			if b.arg != nil && b.argL == l {
				moved = []*ast.Node{b.arg}
			}
			for _, arg := range moved {
				var past []*ast.Node
				past = append(past, before...)
				for j, a := range argNodes {
					if j < i && !bs[j].bound {
						past = append(past, a)
					}
				}
				if why := in.ef.Check(Move{Expr: arg, Past: past}); why != "" {
					return nil, fmt.Sprintf("the argument %s at %s cannot be declared before its statement: %s", l.source(arg), l.where(call), why)
				}
			}
			pn := fl.source(b.param.Name())
			syms := []*ast.Symbol{c.GetSymbolAtLocation(b.param.Name())}
			if !ast.IsIdentifier(b.param.Name()) {
				syms = nil
				for _, id := range bindingNames(b.param.Name()) {
					if why := taken(c, l, id.Text(), stmt); why != "" {
						return nil, fmt.Sprintf("the parameter %s would declare %s at the call: %s", pn, id.Text(), why)
					}
					syms = append(syms, c.GetSymbolAtLocation(id))
				}
			} else if taken(c, l, pn, stmt) != "" {
				// Taken at the call: another name, which neither the call's
				// statement sees nor the function uses.
				free := ""
				for i := 2; i < 10 && free == ""; i++ {
					cand := fmt.Sprintf("%s%d", pn, i)
					if taken(c, l, cand, stmt) == "" && !slices.ContainsFunc(identifierTexts(in.fn), func(s string) bool { return s == cand }) {
						free = cand
					}
				}
				if free == "" {
					return nil, fmt.Sprintf("the parameter %s would be declared at the call, where %s and the names after it are taken", pn, pn)
				}
				b.name, pn = free, free
			}
			kw := "const"
			for _, s := range syms {
				if s != nil && assignedIn(c, []*ast.Node{bodyRoot}, s) {
					kw = "let"
				}
			}
			typ := ""
			if t := b.param.Type(); t != nil && !slices.ContainsFunc(identifiersOf(t), in.typeParam) {
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
		if b.bound && b.name == "" {
			continue
		}
		ps := c.GetSymbolAtLocation(b.param.Name())
		identifiers(bodyRoot, func(id *ast.Node) {
			if symbolOf(c, id) != ps {
				return
			}
			if b.bound { // declared under another name
				text := b.name
				if ast.IsShorthandPropertyAssignment(id.Parent) {
					text = id.Text() + ": " + b.name
				}
				rs = append(rs, repl{fl.pos(id), id.End(), text})
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
	if exprForm && len(in.conds) > 0 {
		// Guards: if (c) return a; ... return d; is c ? a : d.
		text := ""
		for i := range in.conds {
			text += ternaryPart(fl, in.conds[i], rs, true) + " ? " + ternaryPart(fl, in.vals[i], rs, false) + " : "
		}
		text += ternaryPart(fl, in.expr, rs, false)
		if p := call.Parent; !(ast.IsReturnStatement(p) || ast.IsVariableDeclaration(p) || ast.IsExpressionStatement(p) || ast.IsParenthesizedExpression(p) || ast.IsCallExpression(p) && p.Expression() != call) {
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
	// A body that returns early keeps its returns where they return from
	// the caller too: a call that is a return's whole value. Elsewhere each
	// return assigns the call's value instead (early).
	earlyMode := len(in.early) > 0 && !(ast.IsReturnStatement(stmt) && stmt.Expression() == call)
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
	body := in.stmts
	var pre []string
	if earlyMode {
		var why string
		if body, pre, why = in.earlyBody(l, call, stmt, &rs); why != "" {
			return nil, fmt.Sprintf("%s returns early (%s), and %s", name, fl.where(in.early[0]), why)
		}
	}
	a, b, ok := l.lineRange(stmt)
	if !ok {
		return nil, fmt.Sprintf("the call's statement at %s shares its lines with other code", l.where(stmt))
	}
	var out strings.Builder
	out.WriteString(l.text[a:l.start(l.line(l.pos(stmt)))]) // its comments
	for _, d := range append(decls, pre...) {
		out.WriteString(indent + d + "\n")
	}
	if len(body) > 0 {
		s0, s1 := fl.start(fl.line(fl.pos(body[0]))), fl.start(fl.line(body[len(body)-1].End()-1)+1)
		from := fl.indent(fl.line(fl.pos(body[0])))
		for _, line := range strings.SplitAfter(fl.substitute(s0, s1, rs), "\n") {
			if strings.HasPrefix(line, from) {
				line = indent + line[len(from):]
			}
			out.WriteString(line)
		}
	}
	if in.ret != nil && !earlyMode {
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

// earlyBody plans a body that returns early for the call's statement
// stmt (see early): the statements to put in its place, the declarations
// to put before them, and rs with the body's edits added; or why not.
func (in *inliner) earlyBody(l *lined, call, stmt *ast.Node, rs *[]repl) ([]*ast.Node, []string, string) {
	c, fl := in.c, in.fl
	if stmt.Parent == nil || ast.IsSourceFile(stmt.Parent) {
		return nil, nil, fmt.Sprintf("its call at %s is at the top of a module, where the variable each return assigns would be one of the module's", l.where(call))
	}
	caller := stmt
	for caller != nil && !ast.IsFunctionLike(caller) {
		caller = caller.Parent
	}
	e := &early{in: in, used: map[string]bool{}, exits: map[*ast.Node]string{}, flagged: map[*ast.Node]bool{}, breaks: map[*ast.Node]bool{}, hoisted: map[*ast.Node]*ast.Node{}, params: *rs, unit: fl.unit(),
		taken: func(name string) bool { return taken(c, l, name, stmt) != "" }}
	for _, n := range identifierTexts(in.fn) {
		e.used[n] = true
	}
	if caller != nil {
		for _, n := range identifierTexts(caller) {
			e.used[n] = true
		}
	}
	decl := ""
	switch {
	case ast.IsVariableStatement(stmt):
		d := stmt.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes[0]
		if !ast.IsIdentifier(d.Name()) {
			return nil, nil, fmt.Sprintf("its value at %s is destructured", l.where(call))
		}
		typ := ""
		if t := d.Type(); t != nil {
			typ = l.source(t)
		} else {
			typ = c.TypeToString(c.GetWidenedType(c.GetTypeAtLocation(d.Name())))
		}
		e.target, decl, e.decl = d.Name().Text(), ": "+typ, true
	case ast.IsExpressionStatement(stmt) && ast.IsBinaryExpression(stmt.Expression()):
		e.target = l.source(stmt.Expression().AsBinaryExpression().Left)
	}
	if _, why := e.plan(in.fn.Body().Statements(), true, ""); why != "" {
		return nil, nil, why
	}
	e.finish()
	*rs = append(*rs, e.rs...)
	var pre []string
	if decl != "" {
		switch {
		case e.declInit != "":
			pre = append(pre, "let "+e.target+decl+" = "+e.declInit+fl.semi())
		case len(e.flagged) > 0: // a flag hides from the checker that every path assigns
			pre = append(pre, "let "+e.target+"!"+decl+fl.semi())
		default:
			pre = append(pre, "let "+e.target+decl+fl.semi())
		}
	}
	if e.flag != "" {
		pre = append(pre, "let "+e.flag+" = false"+fl.semi())
	}
	return in.fn.Body().Statements(), pre, ""
}
