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

// typeDecl returns the class, interface or type alias of the file named
// name, or the one named on line (at column).
func (l *lined) typeDecl(c *checker.Checker, line int, name string, column int) (*ast.Node, string) {
	if line > 0 {
		var decl *ast.Node
		identifiers(l.f.AsNode(), func(id *ast.Node) {
			start := l.pos(id)
			if decl == nil && id.Text() == name && l.line(start) == line && (column == 0 || start-l.start(line)+1 == column) {
				if s := symbolOf(c, id); s != nil && len(s.Declarations) > 0 {
					decl = s.Declarations[0]
				}
			}
		})
		if decl == nil {
			return nil, fmt.Sprintf("no name %s on line %d", name, line)
		}
		if ast.GetSourceFileOfNode(decl) != l.f {
			return nil, fmt.Sprintf("%s is declared in %s; give that file", name, ast.GetSourceFileOfNode(decl).FileName())
		}
		return decl, ""
	}
	for _, s := range l.f.Statements.Nodes {
		if (ast.IsClassDeclaration(s) || ast.IsInterfaceDeclaration(s) || ast.IsTypeAliasDeclaration(s)) && s.Name() != nil && s.Name().Text() == name {
			return s, ""
		}
	}
	return nil, fmt.Sprintf("the file declares no class, interface or type %s at its top level", name)
}

// abstractMembers returns members, but when it is one instance of an
// abstract class of the solution: the instances of the classes extending
// it directly, which are all its instances can be; and that class.
func (p *Project) abstractMembers(c *checker.Checker, members []*checker.Type) ([]*checker.Type, *ast.Node, string) {
	if len(members) != 1 {
		return members, nil, ""
	}
	base := classOfType(members[0])
	if base == nil || !ast.HasSyntacticModifier(base, ast.ModifierFlagsAbstract) {
		return members, nil, ""
	}
	sym := c.GetSymbolAtLocation(base.Name())
	var out []*checker.Type
	why := ""
	for _, f := range p.prog.GetProgram().GetSourceFiles() {
		if f.IsDeclarationFile {
			continue
		}
		inspect(f.AsNode(), func(n *ast.Node) bool {
			if ast.IsClassDeclaration(n) && n.Name() != nil {
				for _, h := range nodes(n.AsClassDeclaration().HeritageClauses) {
					if h.AsHeritageClause().Token != ast.KindExtendsKeyword {
						continue
					}
					for _, t := range h.AsHeritageClause().Types.Nodes {
						if e := t.Expression(); e != nil && ast.IsIdentifier(e) && symbolOf(c, e) == sym {
							if ast.HasSyntacticModifier(n, ast.ModifierFlagsAbstract) && why == "" {
								why = fmt.Sprintf("%s extends %s and is abstract too; only one level of subclasses is handled", n.Name().Text(), base.Name().Text())
							}
							out = append(out, c.GetTypeAtLocation(n.Name()))
						}
					}
				}
			}
			return true
		})
	}
	if why != "" {
		return nil, nil, why
	}
	if len(out) == 0 {
		return members, nil, ""
	}
	return out, base, ""
}

// nodes returns the nodes of a list that may be nil.
func nodes(list *ast.NodeList) []*ast.Node {
	if list == nil {
		return nil
	}
	return list.Nodes
}

// memberSignature returns the class member m as a member of an
// interface: a property with its type, a method with its parameters and
// result, an accessor as a property (readonly without a setter).
func (l *lined) memberSignature(c *checker.Checker, class, m *ast.Node) (string, string) {
	name := l.source(m.Name())
	typeOf := func(n, annotation *ast.Node) string {
		if annotation != nil {
			return l.source(annotation)
		}
		return c.TypeToString(c.GetWidenedType(c.GetTypeAtLocation(n)))
	}
	switch {
	case ast.IsPropertyDeclaration(m):
		opt := ""
		if m.AsPropertyDeclaration().PostfixToken != nil && m.AsPropertyDeclaration().PostfixToken.Kind == ast.KindQuestionToken {
			opt = "?"
		}
		ro := ""
		if ast.HasSyntacticModifier(m, ast.ModifierFlagsReadonly) {
			ro = "readonly "
		}
		return ro + name + opt + ": " + typeOf(m.Name(), m.Type()) + l.semi(), ""
	case ast.IsMethodDeclaration(m):
		var ps []string
		for _, prm := range m.Parameters() {
			opt := ""
			if prm.AsParameterDeclaration().QuestionToken != nil || prm.Initializer() != nil {
				opt = "?"
			}
			dots := ""
			if prm.AsParameterDeclaration().DotDotDotToken != nil {
				dots = "..."
			}
			ps = append(ps, dots+l.source(prm.Name())+opt+": "+typeOf(prm.Name(), prm.Type()))
		}
		ret := ""
		if t := m.Type(); t != nil {
			ret = l.source(t)
		} else if sig := c.GetSignatureFromDeclaration(m); sig != nil {
			ret = c.TypeToString(c.GetReturnTypeOfSignature(sig))
		}
		return name + "(" + strings.Join(ps, ", ") + "): " + ret + l.semi(), ""
	case ast.IsGetAccessorDeclaration(m):
		setter := slices.ContainsFunc(class.Members(), func(o *ast.Node) bool {
			return ast.IsSetAccessorDeclaration(o) && o.Name() != nil && l.source(o.Name()) == name
		})
		ret := ""
		if t := m.Type(); t != nil {
			ret = l.source(t)
		} else if sig := c.GetSignatureFromDeclaration(m); sig != nil {
			ret = c.TypeToString(c.GetReturnTypeOfSignature(sig))
		}
		if setter {
			return name + ": " + ret + l.semi(), ""
		}
		return "readonly " + name + ": " + ret + l.semi(), ""
	case ast.IsSetAccessorDeclaration(m):
		return "", "" // with its getter
	}
	return "", fmt.Sprintf("%s is not a property, method or accessor", name)
}

// ExtractInterface returns the edits that name members of the class,
// interface or type alias name as a new interface newType. From a class,
// the interface has the members' signatures and the class implements
// it. From an interface, the members move into the new one, which the
// old extends; from a type alias of an object type, into the new one,
// which the alias joins (newType & { the rest }).
func (p *Project) ExtractInterface(ctx context.Context, path string, line int, name string, column int, fields []string, newType string) ([]Edit, string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, why
	}
	c, done := p.prog.GetProgram().GetTypeChecker(ctx)
	defer done()
	decl, why := l.typeDecl(c, line, name, column)
	if why != "" {
		return nil, why
	}
	name = decl.Name().Text()
	if !scanner.IsIdentifierText(newType, 0) {
		return nil, fmt.Sprintf("%q is not a valid name", newType)
	}
	if why := taken(c, l, newType, decl); why != "" {
		return nil, why
	}
	if decl.TypeParameters() != nil {
		return nil, fmt.Sprintf("%s is generic", name)
	}
	var members []*ast.Node
	switch {
	case ast.IsClassDeclaration(decl), ast.IsInterfaceDeclaration(decl):
		members = decl.Members()
	case ast.IsTypeAliasDeclaration(decl) && ast.IsTypeLiteralNode(decl.Type()):
		members = decl.Type().Members()
	default:
		return nil, fmt.Sprintf("%s is a type alias of something other than an object type", name)
	}
	if len(fields) == 0 {
		return nil, "give fields: the members to name"
	}
	var chosen []*ast.Node
	for _, f := range fields {
		n := 0
		for _, m := range members {
			if m.Name() != nil && l.source(m.Name()) == f {
				chosen = append(chosen, m)
				n++
			}
		}
		if n == 0 {
			return nil, fmt.Sprintf("%s has no member %s", name, f)
		}
	}
	slices.SortFunc(chosen, func(a, b *ast.Node) int { return a.Pos() - b.Pos() })
	ind := l.indent(l.line(l.pos(decl)))
	unit := l.unit()
	export := ""
	if ast.HasSyntacticModifier(decl, ast.ModifierFlagsExport) {
		export = "export "
	}
	var body []string
	var edits []Edit
	if ast.IsClassDeclaration(decl) {
		for _, m := range chosen {
			if m.ModifierFlags()&(ast.ModifierFlagsPrivate|ast.ModifierFlagsProtected|ast.ModifierFlagsStatic) != 0 || ast.IsPrivateIdentifier(m.Name()) {
				return nil, fmt.Sprintf("%s is private, protected or static, which an interface cannot name", l.source(m.Name()))
			}
			sig, why := l.memberSignature(c, decl, m)
			if why != "" {
				return nil, why
			}
			if sig != "" && !slices.Contains(body, sig) {
				body = append(body, sig)
			}
		}
		// The class implements it.
		at, text := -1, ""
		for _, h := range nodes(decl.AsClassDeclaration().HeritageClauses) {
			if h.AsHeritageClause().Token == ast.KindImplementsKeyword {
				ts := h.AsHeritageClause().Types.Nodes
				at, text = ts[len(ts)-1].End(), ", "+newType
			}
		}
		if at < 0 {
			at = decl.Name().End()
			if hs := decl.AsClassDeclaration().HeritageClauses; hs != nil && len(hs.Nodes) > 0 {
				at = hs.Nodes[len(hs.Nodes)-1].End()
			}
			text = " implements " + newType
		}
		edits = append(edits, Edit{File: l.f.FileName(), Start: at, End: at, NewText: text})
	} else {
		for _, m := range chosen {
			a, b, ok := l.lineRange(m)
			if !ok {
				return nil, fmt.Sprintf("%s shares its line with other members; put each member on a line of its own", l.source(m.Name()))
			}
			body = append(body, strings.TrimSpace(l.text[a:b]))
			edits = append(edits, Edit{File: l.f.FileName(), Start: a, End: b})
		}
		if ast.IsInterfaceDeclaration(decl) {
			at, text := -1, ""
			for _, h := range nodes(decl.AsInterfaceDeclaration().HeritageClauses) {
				ts := h.AsHeritageClause().Types.Nodes
				at, text = ts[len(ts)-1].End(), ", "+newType
			}
			if at < 0 {
				at, text = decl.Name().End(), " extends "+newType
			}
			edits = append(edits, Edit{File: l.f.FileName(), Start: at, End: at, NewText: text})
		} else {
			lit := decl.Type()
			if len(chosen) == len(members) {
				edits = []Edit{{File: l.f.FileName(), Start: l.pos(lit), End: lit.End(), NewText: newType}}
			} else {
				edits = append(edits, Edit{File: l.f.FileName(), Start: l.pos(lit), End: l.pos(lit), NewText: newType + " & "})
			}
		}
	}
	a, _, ok := l.lineRange(decl)
	if !ok {
		a = l.start(l.line(l.pos(decl)))
	}
	text := ind + export + "interface " + newType + " {\n"
	for _, b := range body {
		for _, line := range strings.Split(b, "\n") {
			text += ind + unit + strings.TrimSpace(line) + "\n"
		}
	}
	text += ind + "}\n\n"
	edits = append(edits, Edit{File: l.f.FileName(), Start: a, End: a, NewText: text})
	slices.SortFunc(edits, func(x, y Edit) int { return x.Start - y.Start })
	return edits, ""
}

// UnionSwitchToMethod returns the edits that turn a switch on a
// discriminant (switch (s.kind)) or an instanceof chain into a method of
// each class of the union it tells apart: each case's body becomes that
// class's method, the subject becoming this, and the switch one call.
func (p *Project) UnionSwitchToMethod(ctx context.Context, path string, line int, function, method string) ([]Edit, string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, why
	}
	if !scanner.IsIdentifierText(method, 0) {
		return nil, fmt.Sprintf("%q is not a valid name", method)
	}
	c, done := p.prog.GetProgram().GetTypeChecker(ctx)
	defer done()
	// The statement: a switch, or an if whose condition is instanceof.
	var stmt *ast.Node
	isSubject := func(n *ast.Node) bool {
		if ast.IsSwitchStatement(n) {
			return ast.IsPropertyAccessExpression(n.AsSwitchStatement().Expression)
		}
		if ast.IsIfStatement(n) && !(n.Parent != nil && ast.IsIfStatement(n.Parent) && n.Parent.AsIfStatement().ElseStatement == n) {
			e := n.AsIfStatement().Expression
			return ast.IsBinaryExpression(e) && e.AsBinaryExpression().OperatorToken.Kind == ast.KindInstanceOfKeyword
		}
		return false
	}
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
		if isSubject(n) && (line == 0 || l.line(l.pos(n)) == line) {
			found = append(found, n)
		}
		n.ForEachChild(visit)
		return false
	}
	visit(scope)
	switch {
	case len(found) == 0 && line > 0:
		return nil, fmt.Sprintf("no switch on a property, or if on instanceof, starts on line %d", line)
	case len(found) == 0:
		return nil, fmt.Sprintf("%s has no switch on a property or if on instanceof", function)
	case len(found) > 1:
		return nil, fmt.Sprintf("there are %d switches there; give line", len(found))
	}
	stmt = found[0]
	fn := ast.GetContainingFunction(stmt)
	if fn == nil {
		return nil, "the switch is in no function"
	}
	// The subject, and each case's class and body.
	type arm struct {
		class *ast.Node
		body  []*ast.Node
		at    *ast.Node
	}
	var arms []arm
	var subject *ast.Node
	var base *ast.Node // an abstract class the subject is typed with, whose subclasses are the arms
	var why3 string
	classOf := func(t *checker.Type) *ast.Node {
		if t == nil || t.Symbol() == nil {
			return nil
		}
		for _, d := range t.Symbol().Declarations {
			if ast.IsClassDeclaration(d) && !ast.GetSourceFileOfNode(d).IsDeclarationFile {
				return d
			}
		}
		return nil
	}
	if ast.IsSwitchStatement(stmt) {
		disc := stmt.AsSwitchStatement().Expression
		subject = disc.Expression()
		prop := disc.Name().Text()
		t := c.GetTypeAtLocation(subject)
		members := []*checker.Type{t}
		if t.Flags()&checker.TypeFlagsUnion != 0 {
			members = t.Types()
		}
		if members, base, why3 = p.abstractMembers(c, members); why3 != "" {
			return nil, why3
		}
		clauses := stmt.AsSwitchStatement().CaseBlock.AsCaseBlock().Clauses.Nodes
		for _, cl := range clauses {
			if ast.IsDefaultClause(cl) {
				return nil, fmt.Sprintf("the switch has a default (%s); give each class a case instead", l.where(cl))
			}
			if len(cl.Statements()) == 0 {
				return nil, fmt.Sprintf("the case at %s falls through to the next; give it its own body", l.where(cl))
			}
			want := c.TypeToString(c.GetTypeAtLocation(cl.Expression()))
			var class *ast.Node
			for _, m := range members {
				if ps := c.GetPropertyOfType(m, prop); ps != nil && c.TypeToString(c.GetTypeOfSymbol(ps)) == want {
					class = classOf(m)
					if class == nil {
						return nil, fmt.Sprintf("%s's case %s is of %s, which is not a class of the solution", prop, l.source(cl.Expression()), c.TypeToString(m))
					}
				}
			}
			if class == nil {
				return nil, fmt.Sprintf("no class of %s has %s %s", c.TypeToString(t), prop, l.source(cl.Expression()))
			}
			body := cl.Statements()
			if last := body[len(body)-1]; last.Kind == ast.KindBreakStatement && last.Label() == nil {
				body = body[:len(body)-1]
			}
			arms = append(arms, arm{class, body, cl})
		}
		for _, m := range members {
			if cls := classOf(m); cls == nil || !slices.ContainsFunc(arms, func(a arm) bool { return a.class == cls }) {
				return nil, fmt.Sprintf("%s has no case; give it one first", c.TypeToString(m))
			}
		}
	} else {
		for s := stmt; s != nil; {
			is := s.AsIfStatement()
			e := is.Expression.AsBinaryExpression()
			if subject == nil {
				subject = e.Left
			} else if !slices.Equal(tokens(l.source(subject)), tokens(l.source(e.Left))) {
				return nil, fmt.Sprintf("%s tests %s, not %s", l.where(s), l.source(e.Left), l.source(subject))
			}
			class := classOf(c.GetTypeAtLocation(e.Right))
			if s := c.GetSymbolAtLocation(e.Right); s != nil {
				for _, d := range c.SkipAlias(s).Declarations {
					if ast.IsClassDeclaration(d) && !ast.GetSourceFileOfNode(d).IsDeclarationFile {
						class = d
					}
				}
			}
			if class == nil {
				return nil, fmt.Sprintf("%s is not a class of the solution", l.source(e.Right))
			}
			body := []*ast.Node{is.ThenStatement}
			if ast.IsBlock(is.ThenStatement) {
				body = is.ThenStatement.Statements()
			}
			arms = append(arms, arm{class, body, s})
			switch next := is.ElseStatement; {
			case next == nil:
				s = nil
			case ast.IsIfStatement(next) && ast.IsBinaryExpression(next.AsIfStatement().Expression) && next.AsIfStatement().Expression.AsBinaryExpression().OperatorToken.Kind == ast.KindInstanceOfKeyword:
				s = next
			default:
				return nil, fmt.Sprintf("the chain ends with an else (%s); give each class a branch instead", l.where(next))
			}
		}
		t := c.GetTypeAtLocation(subject)
		members := []*checker.Type{t}
		if t.Flags()&checker.TypeFlagsUnion != 0 {
			members = t.Types()
		}
		if members, base, why3 = p.abstractMembers(c, members); why3 != "" {
			return nil, why3
		}
		for _, m := range members {
			if cls := classOf(m); cls == nil || !slices.ContainsFunc(arms, func(a arm) bool { return a.class == cls }) {
				return nil, fmt.Sprintf("%s has no branch; give it one first", c.TypeToString(m))
			}
		}
	}
	if re := newEffects(c, l).Of(subject); !re.CanRepeat() {
		return nil, fmt.Sprintf("the subject %s %s; name it first", l.source(subject), re.Why)
	}
	// Every arm returns, or none does.
	returning := 0
	for _, a := range arms {
		b := &body{l: l, c: c, labels: map[string]bool{}}
		for _, s := range a.body {
			b.visit(s)
		}
		if b.why != "" {
			return nil, b.why
		}
		if b.this {
			return nil, fmt.Sprintf("the body at %s uses this, which would then be the class's", l.where(a.at))
		}
		if len(b.returns) > 0 {
			if len(a.body) == 0 || !terminates(a.body[len(a.body)-1]) {
				return nil, fmt.Sprintf("the body at %s returns on some paths only", l.where(a.at))
			}
			returning++
		}
	}
	if returning != 0 && returning != len(arms) {
		return nil, "some bodies return and some do not; make them all return, or none"
	}
	// The locals the bodies use become parameters, by first use.
	inFn := within(fn)
	type param struct {
		sym  *ast.Symbol
		name string
		typ  string
	}
	var params []param
	var edits []Edit
	var why2 string
	for _, a := range arms {
		inBody := func(n *ast.Node) bool {
			return slices.ContainsFunc(a.body, func(s *ast.Node) bool { return n.Pos() >= s.Pos() && n.End() <= s.End() })
		}
		for _, s := range a.body {
			identifiers(s, func(id *ast.Node) {
				if why2 != "" || propertyName(id) || ast.IsDeclarationName(id) {
					return
				}
				sym := symbolOf(c, id)
				if sym == nil || !inFn(sym) || slices.ContainsFunc(sym.Declarations, inBody) {
					return
				}
				if ast.IsIdentifier(subject) && sym == symbolOf(c, subject) {
					return // this
				}
				if assignedIn(c, a.body, sym) {
					why2 = fmt.Sprintf("the body at %s assigns %s, which a method could not", l.where(a.at), id.Text())
					return
				}
				if !slices.ContainsFunc(params, func(p param) bool { return p.sym == sym }) {
					params = append(params, param{sym, id.Text(), c.TypeToString(c.GetWidenedType(c.GetTypeAtLocation(id)))})
				}
			})
		}
	}
	if why2 != "" {
		return nil, why2
	}
	ret := "void"
	if returning > 0 {
		if t := fn.Type(); t != nil {
			ret = l.source(t)
		} else if sig := c.GetSignatureFromDeclaration(fn); sig != nil {
			ret = c.TypeToString(c.GetReturnTypeOfSignature(sig))
		}
	}
	var ps, args []string
	for _, p := range params {
		ps = append(ps, p.name+": "+p.typ)
		args = append(args, p.name)
	}
	subjText := l.source(subject)
	for _, a := range arms {
		cl := newLined(ast.GetSourceFileOfNode(a.class))
		for _, m := range a.class.Members() {
			if m.Name() != nil && cl.source(m.Name()) == method {
				return nil, fmt.Sprintf("%s already has a member %s", a.class.Name().Text(), method)
			}
		}
		// The body, with the subject as this.
		var rs []repl
		for _, s := range a.body {
			inspect(s, func(n *ast.Node) bool {
				if ast.IsExpression(n) && slices.Equal(tokens(l.source(n)), tokens(subjText)) && slices.Equal(symbolsOf(c, n), symbolsOf(c, subject)) && !propertyName(n) {
					rs = append(rs, repl{l.pos(n), n.End(), "this"})
					return false
				}
				return true
			})
		}
		for _, s := range a.body {
			for _, id := range free(c, s) {
				if inFn(symbolOf(c, id)) || ast.IsIdentifier(subject) && symbolOf(c, id) == symbolOf(c, subject) {
					continue
				}
				if r := c.ResolveName(id.Text(), a.class, ast.SymbolFlagsValue, false); r == nil || c.SkipAlias(r) != symbolOf(c, id) {
					return nil, fmt.Sprintf("%s, which the body at %s uses, is not seen in %s", id.Text(), l.where(a.at), a.class.Name().Text())
				}
			}
		}
		mind := cl.indent(cl.line(cl.pos(a.class))) + cl.unit()
		text := ""
		if len(a.body) > 0 {
			first, last := a.body[0], a.body[len(a.body)-1]
			start := l.start(l.line(l.pos(first)))
			end := l.start(l.line(last.End()-1) + 1)
			from := l.indent(l.line(l.pos(first)))
			for _, ln := range strings.SplitAfter(l.substitute(start, end, rs), "\n") {
				if strings.HasPrefix(ln, from) {
					ln = mind + cl.unit() + ln[len(from):]
				}
				text += ln
			}
		}
		head := method
		if base != nil && p.prog.GetProgram().Options().NoImplicitOverride.IsTrue() {
			head = "override " + method
		}
		decl := "\n" + mind + head + "(" + strings.Join(ps, ", ") + "): " + ret + " {\n" + text + mind + "}\n"
		closing := a.class.End() - 1
		at := cl.start(cl.line(closing))
		if strings.TrimSpace(cl.text[at:closing]) != "" {
			return nil, fmt.Sprintf("%s's closing brace shares its line; put it on a line of its own", a.class.Name().Text())
		}
		if len(a.class.Members()) == 0 {
			decl = strings.TrimPrefix(decl, "\n")
		}
		edits = append(edits, Edit{File: cl.f.FileName(), Start: at, End: at, NewText: decl})
	}
	if base != nil {
		// The base declares the method its subclasses now have.
		bl := newLined(ast.GetSourceFileOfNode(base))
		for _, m := range base.Members() {
			if m.Name() != nil && bl.source(m.Name()) == method {
				return nil, fmt.Sprintf("%s already has a member %s", base.Name().Text(), method)
			}
		}
		closing := base.End() - 1
		at := bl.start(bl.line(closing))
		if strings.TrimSpace(bl.text[at:closing]) != "" {
			return nil, fmt.Sprintf("%s's closing brace shares its line; put it on a line of its own", base.Name().Text())
		}
		mind := bl.indent(bl.line(bl.pos(base))) + bl.unit()
		edits = append(edits, Edit{File: bl.f.FileName(), Start: at, End: at, NewText: mind + "abstract " + method + "(" + strings.Join(ps, ", ") + "): " + ret + bl.semi() + "\n"})
	}
	call := operand(l, subject) + "." + method + "(" + strings.Join(args, ", ") + ")"
	if returning > 0 {
		call = "return " + call
	}
	edits = append(edits, Edit{File: l.f.FileName(), Start: l.pos(stmt), End: stmt.End(), NewText: call + l.semi()})
	return edits, ""
}
