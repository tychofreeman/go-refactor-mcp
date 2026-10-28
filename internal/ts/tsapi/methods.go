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

// FunctionToMethod returns the edits that make the function name a method
// of the class its first parameter is typed with, declared in the same
// file: the parameter becomes this, the function goes, and each call
// f(x, args) becomes x.method(args), which evaluates x and args in the
// same order. method is the method's name ("" keeps the function's).
func (p *Project) FunctionToMethod(ctx context.Context, path, name, method string) ([]Edit, string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, why
	}
	fn, overloads, nameNode, why := l.function(name)
	if why != "" {
		return nil, why
	}
	switch {
	case !ast.IsFunctionDeclaration(fn):
		return nil, fmt.Sprintf("%s is not a function declaration", name)
	case len(overloads) > 0:
		return nil, fmt.Sprintf("%s has overload signatures", name)
	case len(fn.TypeParameters()) > 0:
		return nil, fmt.Sprintf("%s is generic", name)
	case len(fn.Parameters()) == 0:
		return nil, fmt.Sprintf("%s has no parameter to become this", name)
	}
	if method == "" {
		method = name
	}
	if !scanner.IsIdentifierText(method, 0) {
		return nil, fmt.Sprintf("%q is not a valid name", method)
	}
	recv := fn.Parameters()[0]
	pd := recv.AsParameterDeclaration()
	if !ast.IsIdentifier(recv.Name()) || pd.DotDotDotToken != nil || pd.QuestionToken != nil || recv.Initializer() != nil {
		return nil, fmt.Sprintf("%s's first parameter %s is optional, rest, defaulted or destructured", name, l.source(recv))
	}
	c, done := p.prog.GetProgram().GetTypeChecker(ctx)
	defer done()
	rsym := c.GetSymbolAtLocation(recv.Name())
	rt := c.GetTypeAtLocation(recv.Name())
	class := classOfType(rt)
	if class == nil {
		return nil, fmt.Sprintf("%s's first parameter is of %s, not of a class of the solution", name, c.TypeToString(rt))
	}
	cname := class.Name().Text()
	if ast.GetSourceFileOfNode(class) != l.f {
		return nil, fmt.Sprintf("%s is declared in another file; move %s next to it first (move_declaration)", cname, name)
	}
	if why := p.memberTaken(c, class, method); why != "" {
		return nil, why
	}
	if assignedIn(c, []*ast.Node{fn.Body()}, rsym) {
		return nil, fmt.Sprintf("%s assigns its parameter %s, which this could not be", name, recv.Name().Text())
	}
	// The body, with the parameter as this.
	var rs []repl
	why = ""
	var walk func(n *ast.Node, nested bool)
	walk = func(n *ast.Node, nested bool) {
		if why != "" {
			return
		}
		switch {
		case n.Kind == ast.KindThisKeyword && !nested:
			why = fmt.Sprintf("%s uses this (%s), which would then be the instance", name, l.where(n))
		case ast.IsIdentifier(n) && !propertyName(n) && symbolOf(c, n) == rsym:
			if nested {
				why = fmt.Sprintf("%s uses %s inside a nested function or class (%s), where this is another", name, n.Text(), l.where(n))
			}
			text := "this"
			if ast.IsShorthandPropertyAssignment(n.Parent) {
				text = n.Text() + ": this"
			}
			rs = append(rs, repl{l.pos(n), n.End(), text})
		}
		inner := nested || (ast.IsFunctionLike(n) && !ast.IsArrowFunction(n)) || ast.IsClassLike(n)
		n.ForEachChild(func(ch *ast.Node) bool { walk(ch, inner); return false })
	}
	walk(fn.Body(), false)
	if why != "" {
		return nil, why
	}
	inFn := within(fn)
	for _, id := range free(c, fn.Body()) {
		if s := symbolOf(c, id); s != nil && !inFn(s) {
			if r := c.ResolveName(id.Text(), class, ast.SymbolFlagsValue, false); r == nil || c.SkipAlias(r) != s {
				return nil, fmt.Sprintf("%s, which %s uses, means something else in %s", id.Text(), name, cname)
			}
		}
	}
	// The calls.
	var edits []Edit
	gone := map[*ast.Node]bool{}
	files := map[*ast.SourceFile]bool{}
	fsym := c.GetSymbolAtLocation(nameNode)
	for _, id := range p.references(c, []*ast.Symbol{fsym}) {
		f := ast.GetSourceFileOfNode(id)
		fl := newLined(f)
		switch {
		case id == nameNode:
			continue
		case ast.IsImportSpecifier(id.Parent):
			files[f] = true
			continue
		case ast.IsExportSpecifier(id.Parent):
			return nil, fmt.Sprintf("%s is exported again at %s", name, fl.where(id))
		case f == l.f && id.Pos() >= fn.Pos() && id.End() <= fn.End():
			return nil, fmt.Sprintf("%s calls itself", name)
		}
		call, w := fl.callOf(id)
		if call == nil || call.Expression() != id {
			if w == "" {
				w = fmt.Sprintf("%s is used at %s other than as a call", name, fl.where(id))
			}
			return nil, w
		}
		args := call.Arguments()
		if len(args) == 0 {
			return nil, fmt.Sprintf("the call at %s passes nothing to become this", fl.where(call))
		}
		rest := ""
		if len(args) > 1 {
			rest = fl.text[fl.pos(args[1]):args[len(args)-1].End()]
		}
		edits = append(edits, Edit{File: f.FileName(), Start: fl.pos(call), End: call.End(), NewText: operand(fl, args[0]) + "." + method + "(" + rest + ")"})
		gone[id] = true
		files[f] = true
	}
	for f := range files {
		edits = append(edits, dropImports(c, newLined(f), func(n *ast.Node) bool { return gone[n] }, true)...)
	}
	// The method, at the end of the class; the function goes.
	a, b, ok := l.lineRange(fn)
	if !ok {
		return nil, fmt.Sprintf("%s shares its lines with other code", name)
	}
	head := method + "("
	if flags := ast.GetFunctionFlags(fn); flags&ast.FunctionFlagsGenerator != 0 {
		head = "*" + head
	}
	if ast.GetFunctionFlags(fn)&ast.FunctionFlagsAsync != 0 {
		head = "async " + head
	}
	if ps := fn.Parameters()[1:]; len(ps) > 0 {
		head += l.text[l.pos(ps[0]):ps[len(ps)-1].End()]
	}
	head += ")"
	if t := fn.Type(); t != nil {
		head += ": " + l.source(t)
	}
	mind := l.indent(l.line(l.pos(class))) + l.unit()
	text := l.rewritten(a, b, fn, fn.Body(), head+" ", l.indent(l.line(l.pos(fn))), mind, rs)
	closing := class.End() - 1
	at := l.start(l.line(closing))
	if strings.TrimSpace(l.text[at:closing]) != "" {
		return nil, fmt.Sprintf("%s's closing brace shares its line; put it on a line of its own", cname)
	}
	if len(class.Members()) > 0 {
		text = "\n" + text
	}
	a, b = l.withBlank(a, b)
	edits = append(edits, Edit{File: l.f.FileName(), Start: at, End: at, NewText: text}, Edit{File: l.f.FileName(), Start: a, End: b})
	return edits, ""
}

// MethodToFunction returns the edits that make the method name
// ("Class.method") a function after its class whose first parameter is
// the instance: this becomes that parameter, and each call x.m(args)
// becomes fn(x, args), which evaluates x and args in the same order. A
// file that calls it imports it, and it is exported when one does. fname
// is the function's name ("" keeps the method's).
func (p *Project) MethodToFunction(ctx context.Context, path, name, fname string) ([]Edit, string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, why
	}
	cname, mname, ok := strings.Cut(name, ".")
	if !ok {
		return nil, fmt.Sprintf("name the method as Class.method, not %q", name)
	}
	class, ms, why := l.member(cname, mname)
	if why != "" {
		return nil, why
	}
	switch {
	case !ast.IsClassDeclaration(class):
		return nil, fmt.Sprintf("%s is an interface", cname)
	case len(ms) != 1 || !ast.IsMethodDeclaration(ms[0]) || ms[0].Body() == nil:
		return nil, fmt.Sprintf("%s is not a method with one body (a property, an accessor, or overloaded)", name)
	case ast.HasSyntacticModifier(ms[0], ast.ModifierFlagsStatic):
		return nil, fmt.Sprintf("%s is static: it is called on the class, not an instance", name)
	case ast.IsPrivateIdentifier(ms[0].Name()):
		return nil, fmt.Sprintf("%s has a private name, which only the class sees", name)
	case ast.HasDecorators(ms[0]):
		return nil, fmt.Sprintf("%s has decorators", name)
	case len(class.TypeParameters()) > 0:
		return nil, fmt.Sprintf("%s is generic, so a function would need its type parameters", cname)
	}
	m := ms[0]
	if fname == "" {
		fname = mname
	}
	c, done := p.prog.GetProgram().GetTypeChecker(ctx)
	defer done()
	if why := p.overrides(c, l, class, m); why != "" {
		return nil, why
	}
	if sub := p.overriddenBy(c, class, mname); sub != "" {
		return nil, fmt.Sprintf("%s overrides %s, so which runs depends on the instance", sub, name)
	}
	if why := taken(c, l, fname, class); why != "" {
		return nil, why
	}
	// The instance's name: the class's, in lower case, free in the method.
	base := strings.ToLower(cname[:1]) + cname[1:]
	if strings.ToUpper(cname) == cname {
		base = strings.ToLower(cname)
	}
	recv := base
	names := identifierTexts(m)
	for i := 2; slices.Contains(names, recv) || recv == fname; i++ {
		recv = fmt.Sprintf("%s%d", base, i)
	}
	// The body, with this as the instance.
	var rs []repl
	why = ""
	var walk func(n *ast.Node, nested bool)
	walk = func(n *ast.Node, nested bool) {
		if why != "" {
			return
		}
		switch {
		case n.Kind == ast.KindThisKeyword && !nested:
			rs = append(rs, repl{l.pos(n), n.End(), recv})
		case n.Kind == ast.KindSuperKeyword && !nested:
			why = fmt.Sprintf("%s uses super (%s), which only a method has", name, l.where(n))
		case ast.IsPrivateIdentifier(n):
			why = fmt.Sprintf("%s uses the private name %s, which only the class sees", name, n.Text())
		}
		inner := nested || (ast.IsFunctionLike(n) && !ast.IsArrowFunction(n)) || ast.IsClassLike(n)
		n.ForEachChild(func(ch *ast.Node) bool { walk(ch, inner); return false })
	}
	walk(m.Body(), false)
	if why != "" {
		return nil, why
	}
	// The calls; a file that calls it imports it.
	var edits []Edit
	exported := false
	imported := map[*ast.SourceFile]bool{}
	for _, id := range p.references(c, []*ast.Symbol{c.GetSymbolAtLocation(m.Name())}) {
		f := ast.GetSourceFileOfNode(id)
		fl := newLined(f)
		if id == m.Name() {
			continue
		}
		access := id.Parent
		if !ast.IsPropertyAccessExpression(access) || access.Name() != id || access.Parent == nil || !ast.IsCallExpression(access.Parent) || access.Parent.Expression() != access {
			return nil, fmt.Sprintf("%s is used at %s other than as a call", name, fl.where(id))
		}
		call := access.Parent
		switch {
		case access.AsPropertyAccessExpression().QuestionDotToken != nil || call.AsCallExpression().QuestionDotToken != nil:
			return nil, fmt.Sprintf("the call at %s is optional (?.), which a function call is not", fl.where(call))
		case access.Expression().Kind == ast.KindSuperKeyword:
			return nil, fmt.Sprintf("the call at %s is through super", fl.where(call))
		case f == l.f && id.Pos() >= m.Pos() && id.End() <= m.End():
			return nil, fmt.Sprintf("%s calls itself", name)
		}
		if _, w := fl.callOf(id); w != "" {
			return nil, w
		}
		args := fl.source(access.Expression())
		if as := call.Arguments(); len(as) > 0 {
			args += ", " + fl.text[fl.pos(as[0]):as[len(as)-1].End()]
		}
		edits = append(edits, Edit{File: f.FileName(), Start: fl.pos(call), End: call.End(), NewText: fname + "(" + args + ")"})
		if f != l.f && !imported[f] {
			if w := taken(c, fl, fname, call); w != "" {
				return nil, w
			}
			imported[f], exported = true, true
			at, text := fl.importAt()
			edits = append(edits, Edit{File: f.FileName(), Start: at, End: at, NewText: text(importLine{spec: fl.specFor(l.f.FileName()), named: []string{fname}}.text(fl.quote(), fl.semi()))})
		}
	}
	// The function, after the class; the method goes.
	a, b, ok := l.lineRange(m)
	if !ok {
		return nil, fmt.Sprintf("%s shares its lines with other code", name)
	}
	head := "function "
	flags := ast.GetFunctionFlags(m)
	if flags&ast.FunctionFlagsGenerator != 0 {
		head = "function* "
	}
	if flags&ast.FunctionFlagsAsync != 0 {
		head = "async " + head
	}
	if exported {
		head = "export " + head
	}
	head += fname
	if tps := m.TypeParameterList(); tps != nil && len(tps.Nodes) > 0 {
		head += "<" + l.text[l.pos(tps.Nodes[0]):tps.Nodes[len(tps.Nodes)-1].End()] + ">"
	}
	head += "(" + recv + ": " + cname
	if ps := m.Parameters(); len(ps) > 0 {
		head += ", " + l.text[l.pos(ps[0]):ps[len(ps)-1].End()]
	}
	head += ")"
	if t := m.Type(); t != nil {
		head += ": " + l.source(t)
	}
	text := l.rewritten(a, b, m, m.Body(), head+" ", l.indent(l.line(l.pos(m))), l.indent(l.line(l.pos(class))), rs)
	end := l.end(l.line(class.End() - 1))
	a, b = l.withBlank(a, b)
	edits = append(edits, Edit{File: l.f.FileName(), Start: a, End: b}, Edit{File: l.f.FileName(), Start: end, End: end, NewText: "\n\n" + strings.TrimRight(text, "\n")})
	return edits, ""
}

// rewritten returns the whole lines [a, b) of decl, whose body is body,
// with what comes before body (from decl's first token) replaced by head,
// the lines' indentation from changed to to, and rs applied.
func (l *lined) rewritten(a, b int, decl, body *ast.Node, head, from, to string, rs []repl) string {
	declLine, openLine := l.line(l.pos(decl)), l.line(l.pos(body))
	text := l.reindent(a, l.start(declLine), from, to)
	text += to + head + l.substitute(l.pos(body), l.end(openLine), rs) + "\n"
	if l.line(b-1) > openLine {
		text += l.reindent(l.start(openLine+1), b, from, to, rs...)
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text
}

// classOfType returns the class of the solution that t is an instance
// of, or nil.
func classOfType(t *checker.Type) *ast.Node {
	if t == nil || t.Symbol() == nil {
		return nil
	}
	for _, d := range t.Symbol().Declarations {
		if ast.IsClassDeclaration(d) && d.Name() != nil && !ast.GetSourceFileOfNode(d).IsDeclarationFile {
			return d
		}
	}
	return nil
}

// memberTaken says why class cannot take a member called name: it, a
// class it extends or a class extending it has one.
func (p *Project) memberTaken(c *checker.Checker, class *ast.Node, name string) string {
	if c.GetPropertyOfType(c.GetTypeAtLocation(class.Name()), name) != nil {
		return fmt.Sprintf("%s already has a member %s", class.Name().Text(), name)
	}
	if sub := p.overriddenBy(c, class, name); sub != "" {
		return fmt.Sprintf("%s, which extends %s, already has a member %s", sub, class.Name().Text(), name)
	}
	return ""
}

// overriddenBy returns the name of a class of the program extending class,
// directly or not, that declares a member called name; "" if none does.
func (p *Project) overriddenBy(c *checker.Checker, class *ast.Node, name string) string {
	sym := c.GetSymbolAtLocation(class.Name())
	extends := func(cls *ast.Node) bool {
		for range 20 {
			hs := cls.AsClassDeclaration().HeritageClauses
			var next *ast.Node
			for _, h := range nodes(hs) {
				if h.AsHeritageClause().Token != ast.KindExtendsKeyword {
					continue
				}
				for _, t := range h.AsHeritageClause().Types.Nodes {
					if e := t.Expression(); e != nil && ast.IsIdentifier(e) {
						if s := symbolOf(c, e); s == sym {
							return true
						} else if s != nil {
							for _, d := range s.Declarations {
								if ast.IsClassDeclaration(d) {
									next = d
								}
							}
						}
					}
				}
			}
			if next == nil {
				return false
			}
			cls = next
		}
		return false
	}
	found := ""
	for _, f := range p.prog.GetProgram().GetSourceFiles() {
		if f.IsDeclarationFile || found != "" {
			continue
		}
		var visit func(n *ast.Node) bool
		visit = func(n *ast.Node) bool {
			if found != "" {
				return true
			}
			if ast.IsClassDeclaration(n) && n != class && extends(n) {
				fl := newLined(f)
				for _, m := range n.Members() {
					if m.Name() != nil && fl.source(m.Name()) == name {
						found = "a class at " + fl.where(n)
						if n.Name() != nil {
							found = n.Name().Text()
						}
					}
				}
			}
			return n.ForEachChild(visit)
		}
		visit(f.AsNode())
	}
	return found
}
