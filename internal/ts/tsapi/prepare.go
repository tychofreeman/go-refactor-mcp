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

// PrepareSwitchForTypes returns the edits that prepare the switch on line
// of the file at path (or function's only switch, when line is 0) for its
// cases to move into classes one at a time: the interface iface gets
// method, whose parameters are the locals the cases use, and a guard
// before the switch hands a subject that has the method to it. No class
// implements it yet, so nothing changes. Cases sharing a body are split,
// each with a copy, so that each can move alone.
func (p *Project) PrepareSwitchForTypes(ctx context.Context, path string, line int, function, iface, method string) ([]Edit, string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, why
	}
	for _, n := range []string{iface, method} {
		if !scanner.IsIdentifierText(n, 0) {
			return nil, fmt.Sprintf("%q is not a valid name", n)
		}
	}
	c, done := p.prog.GetProgram().GetTypeChecker(ctx)
	defer done()
	var scope *ast.Node = l.f.AsNode()
	if function != "" {
		fn, _, _, why := l.function(function)
		if why != "" {
			return nil, why
		}
		scope = fn
	}
	var found []*ast.Node
	var visit func(n *ast.Node) bool
	visit = func(n *ast.Node) bool {
		if ast.IsSwitchStatement(n) && (line == 0 || l.line(l.pos(n)) == line) {
			found = append(found, n)
		}
		n.ForEachChild(visit)
		return false
	}
	visit(scope)
	switch {
	case len(found) == 0 && line > 0:
		return nil, fmt.Sprintf("no switch starts on line %d", line)
	case len(found) == 0:
		return nil, fmt.Sprintf("%s has no switch", function)
	case len(found) > 1:
		return nil, fmt.Sprintf("there are %d switches there; give line", len(found))
	}
	sw := found[0]
	fn := ast.GetContainingFunction(sw)
	if fn == nil {
		return nil, "the switch is in no function"
	}
	subject := sw.AsSwitchStatement().Expression
	if t := c.GetTypeAtLocation(subject); !mayHoldObject(t) {
		return nil, fmt.Sprintf("the subject %s is of %s, which holds no object; widen it first (change_field_type or change_parameter_type to unknown)", l.source(subject), c.TypeToString(t))
	}
	if re := newEffects(c, l).Of(subject); !re.CanRepeat() {
		return nil, fmt.Sprintf("the subject %s %s; name it first", l.source(subject), re.Why)
	}
	// The cases: each returns, or none does; a case without a body takes
	// a copy of the next one's.
	clauses := sw.AsSwitchStatement().CaseBlock.AsCaseBlock().Clauses.Nodes
	returning, bodies := 0, 0
	var splits []*ast.Node // the clauses that take a copy of the next body
	var stmts []*ast.Node  // every statement of every body
	for i, cl := range clauses {
		own := cl.Statements()
		if len(own) == 0 {
			if i == len(clauses)-1 {
				return nil, fmt.Sprintf("the last case (%s) has no body", l.where(cl))
			}
			splits = append(splits, cl)
			continue
		}
		bodies++
		b := &body{l: l, c: c, labels: map[string]bool{}, switches: 1}
		for _, s := range own {
			b.visit(s)
		}
		switch {
		case b.why != "":
			return nil, b.why
		case b.this:
			return nil, fmt.Sprintf("the case at %s uses this, which would then be the class's", l.where(cl))
		case len(b.returns) > 0:
			if !terminates(own[len(own)-1]) {
				return nil, fmt.Sprintf("the case at %s returns on some paths only", l.where(cl))
			}
			returning++
		}
		stmts = append(stmts, own...)
	}
	if returning != 0 && returning != bodies {
		return nil, "some cases return and some do not; make them all return, or none"
	}
	for _, cl := range splits {
		next := clauses[slices.Index(clauses, cl)+1:]
		for len(next[0].Statements()) == 0 {
			next = next[1:]
		}
		for _, s := range next[0].Statements() {
			if len(declared(s)) > 0 || ast.IsClassDeclaration(s) || ast.IsFunctionDeclaration(s) {
				return nil, fmt.Sprintf("the case at %s shares a body that declares %s, whose copies would clash", l.where(cl), l.part(s))
			}
		}
	}
	// The locals the cases use become the method's parameters.
	inFn := within(fn)
	inCases := func(n *ast.Node) bool {
		return slices.ContainsFunc(stmts, func(s *ast.Node) bool { return n.Pos() >= s.Pos() && n.End() <= s.End() })
	}
	type param struct {
		sym       *ast.Symbol
		name, typ string
	}
	var params []param
	why = ""
	for _, s := range stmts {
		identifiers(s, func(id *ast.Node) {
			if why != "" || propertyName(id) || ast.IsDeclarationName(id) {
				return
			}
			sym := symbolOf(c, id)
			if sym == nil || !inFn(sym) || slices.ContainsFunc(sym.Declarations, inCases) {
				return
			}
			if assignedIn(c, stmts, sym) {
				why = fmt.Sprintf("the cases assign %s, which a method could not", id.Text())
				return
			}
			if !slices.ContainsFunc(params, func(p param) bool { return p.sym == sym }) {
				params = append(params, param{sym, id.Text(), c.TypeToString(c.GetWidenedType(c.GetTypeAtLocation(id)))})
			}
		})
	}
	if why != "" {
		return nil, why
	}
	ret := "void"
	if returning > 0 {
		if t := l.returnType(fn, c); t != "" {
			ret = t
		}
	}
	var ps, args []string
	for _, p := range params {
		ps = append(ps, p.name+": "+p.typ)
		args = append(args, p.name)
	}
	sig := method + "(" + strings.Join(ps, ", ") + "): " + ret + l.semi()
	var edits []Edit
	// The interface: the file's, given the method, or a new one before
	// the function's statement.
	top := fn
	for top.Parent != nil && !ast.IsSourceFile(top.Parent) {
		top = top.Parent
	}
	var decl *ast.Node
	for _, s := range l.f.Statements.Nodes {
		if s.Name() != nil && s.Name().Text() == iface {
			if !ast.IsInterfaceDeclaration(s) {
				return nil, fmt.Sprintf("%s is declared at %s, and is not an interface", iface, l.where(s))
			}
			decl = s
		}
	}
	unit := l.unit()
	if decl != nil {
		for _, m := range decl.Members() {
			if m.Name() != nil && l.source(m.Name()) == method {
				return nil, fmt.Sprintf("%s already has %s", iface, method)
			}
		}
		closing := decl.End() - 1
		at := l.start(l.line(closing))
		if strings.TrimSpace(l.text[at:closing]) != "" {
			return nil, fmt.Sprintf("%s's closing brace shares its line; put it on a line of its own", iface)
		}
		edits = append(edits, Edit{File: l.f.FileName(), Start: at, End: at, NewText: l.indent(l.line(l.pos(decl))) + unit + sig + "\n"})
	} else {
		if r := c.ResolveName(iface, top, ast.SymbolFlagsType, false); r != nil {
			return nil, fmt.Sprintf("%s is already a type there", iface)
		}
		export := ""
		if ast.HasSyntacticModifier(top, ast.ModifierFlagsExport) {
			export = "export "
		}
		a, _, ok := l.lineRange(top)
		if !ok {
			a = l.start(l.line(l.pos(top)))
		}
		edits = append(edits, Edit{File: l.f.FileName(), Start: a, End: a, NewText: export + "interface " + iface + " {\n" + unit + sig + "\n}\n\n"})
	}
	// The guard, and the splits.
	s := l.source(subject)
	cond := "typeof " + s + " === \"object\" && " + s + " !== null && \"" + method + "\" in " + s
	call := "(" + s + " as " + iface + ")." + method + "(" + strings.Join(args, ", ") + ")"
	ind := l.indent(l.line(l.pos(sw)))
	extra := "" // what the switch's lines gain: a unit when it goes in the else
	if returning == 0 {
		extra = unit
	}
	var rs []repl
	for _, cl := range splits {
		next := clauses[slices.Index(clauses, cl)+1:]
		for len(next[0].Statements()) == 0 {
			next = next[1:]
		}
		body := next[0].Statements()
		a := l.start(l.line(l.pos(body[0])))
		b := l.end(l.line(body[len(body)-1].End() - 1))
		if strings.TrimSpace(l.text[a:l.pos(body[0])]) != "" || l.line(l.pos(cl)) == l.line(l.pos(body[0])) {
			return nil, fmt.Sprintf("the body after %s shares its lines with its case; put it on lines of its own", l.where(cl))
		}
		copied := ""
		for _, ln := range strings.Split(l.text[a:b], "\n") {
			if strings.TrimSpace(ln) != "" {
				ln = extra + ln
			}
			copied += "\n" + ln
		}
		at := l.end(l.line(cl.End() - 1))
		rs = append(rs, repl{at, at, copied})
	}
	if returning > 0 {
		for _, r := range rs {
			edits = append(edits, Edit{File: l.f.FileName(), Start: r.start, End: r.end, NewText: r.text})
		}
		guard := "if (" + cond + ") {\n" + ind + unit + "return " + call + l.semi() + "\n" + ind + "}\n" + ind
		edits = append(edits, Edit{File: l.f.FileName(), Start: l.pos(sw), End: l.pos(sw), NewText: guard})
		return edits, ""
	}
	a := l.start(l.line(l.pos(sw)))
	b := l.start(l.line(sw.End()-1) + 1)
	if strings.TrimSpace(l.text[a:l.pos(sw)]) != "" || !quiet(l.text[sw.End():l.end(l.line(sw.End()-1))]) {
		return nil, "the switch shares its lines with other code"
	}
	body := l.reindent(a, b, ind, ind+unit, rs...)
	text := ind + "if (" + cond + ") {\n" + ind + unit + call + l.semi() + "\n" + ind + "} else {\n" + body + ind + "}\n"
	edits = append(edits, Edit{File: l.f.FileName(), Start: a, End: b, NewText: text})
	return edits, ""
}

// mayHoldObject reports whether a value of t may be an object: t is any,
// unknown, object, an object type, or a union with one of them.
func mayHoldObject(t *checker.Type) bool {
	if t == nil {
		return false
	}
	types := []*checker.Type{t}
	if t.Flags()&checker.TypeFlagsUnion != 0 {
		types = t.Types()
	}
	for _, u := range types {
		if u.Flags()&(checker.TypeFlagsAnyOrUnknown|checker.TypeFlagsNonPrimitive|checker.TypeFlagsObject|checker.TypeFlagsIntersection|checker.TypeFlagsTypeParameter) != 0 {
			return true
		}
	}
	return false
}
