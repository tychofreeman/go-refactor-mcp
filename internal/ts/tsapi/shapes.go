package tsapi

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/checker"
)

// matcher compares code with a pattern, node for node: names mean the
// same, but for the pattern's own locals, which pair one to one with the
// code's, and its holes, which match any expression.
type matcher struct {
	c        *checker.Checker
	pl, cl   *lined
	inP, inC func(*ast.Symbol) bool // whether a symbol is declared in the pattern's, the code's function
	holes    map[*ast.Symbol]int    // the pattern's parameters that match any expression, by position
	this     bool                   // this in the pattern is a hole too, at position len(holes)
	args     []*ast.Node            // what each hole matched, first
	uses     [][]*ast.Node          // the pattern's uses of each hole
	pair     map[*ast.Symbol]*ast.Symbol
	back     map[*ast.Symbol]*ast.Symbol
	literals bool // literals may differ: each pair is recorded
	diffs    [][2]*ast.Node
	why      string // the first difference
}

func newMatcher(c *checker.Checker, pl, cl *lined) *matcher {
	return &matcher{c: c, pl: pl, cl: cl, pair: map[*ast.Symbol]*ast.Symbol{}, back: map[*ast.Symbol]*ast.Symbol{}, inP: func(*ast.Symbol) bool { return false }, inC: func(*ast.Symbol) bool { return false }}
}

// within returns whether a symbol is declared inside fn.
func within(fn *ast.Node) func(*ast.Symbol) bool {
	f := ast.GetSourceFileOfNode(fn)
	return func(s *ast.Symbol) bool {
		for _, d := range s.Declarations {
			if ast.GetSourceFileOfNode(d) == f && d.Pos() >= fn.Pos() && d.End() <= fn.End() {
				return true
			}
		}
		return false
	}
}

// fork returns a copy to try a match with, keeping m as it is.
func (m *matcher) fork() *matcher {
	n := *m
	n.pair, n.back = map[*ast.Symbol]*ast.Symbol{}, map[*ast.Symbol]*ast.Symbol{}
	for k, v := range m.pair {
		n.pair[k] = v
	}
	for k, v := range m.back {
		n.back[k] = v
	}
	n.args = slices.Clone(m.args)
	n.uses = make([][]*ast.Node, len(m.uses))
	for i := range m.uses {
		n.uses[i] = slices.Clone(m.uses[i])
	}
	n.diffs = slices.Clone(m.diffs)
	return &n
}

func (m *matcher) differ(p, x *ast.Node) bool {
	if m.why == "" {
		m.why = fmt.Sprintf("%s differs from %s", m.cl.part(x), m.pl.part(p))
	}
	return false
}

// propertyName reports whether id names a property, not a value.
func propertyName(id *ast.Node) bool {
	p := id.Parent
	return p != nil && (ast.IsPropertyAccessExpression(p) && p.Name() == id || ast.IsPropertyAssignment(p) && p.Name() == id ||
		ast.IsQualifiedName(p) && p.AsQualifiedName().Right == id || (ast.IsMethodDeclaration(p) || ast.IsPropertyDeclaration(p) ||
		ast.IsPropertySignatureDeclaration(p) || ast.IsMethodSignatureDeclaration(p)) && p.Name() == id || ast.IsBindingElement(p) && p.AsBindingElement().PropertyName == id)
}

func children(n *ast.Node) []*ast.Node {
	var out []*ast.Node
	n.ForEachChild(func(c *ast.Node) bool { out = append(out, c); return false })
	return out
}

// match reports whether the code x matches the pattern p.
func (m *matcher) match(p, x *ast.Node) bool {
	if p == nil || x == nil {
		if p != x {
			return m.differ(p, x)
		}
		return true
	}
	hole := -1
	if ast.IsIdentifier(p) && !propertyName(p) {
		if i, ok := m.holes[symbolOf(m.c, p)]; ok {
			hole = i
		}
	} else if p.Kind == ast.KindThisKeyword && m.this {
		hole = len(m.holes)
	}
	if hole >= 0 {
		if !ast.IsExpression(x) || ast.IsSpreadElement(x) {
			return m.differ(p, x)
		}
		m.uses[hole] = append(m.uses[hole], p)
		if a := m.args[hole]; a != nil {
			if !slices.Equal(tokens(m.cl.source(a)), tokens(m.cl.source(x))) || !slices.Equal(symbolsOf(m.c, a), symbolsOf(m.c, x)) {
				m.why = fmt.Sprintf("%s and %s stand for the same parameter but differ", m.cl.part(a), m.cl.part(x))
				return false
			}
			return true
		}
		m.args[hole] = x
		return true
	}
	if p.Kind != x.Kind {
		return m.differ(p, x)
	}
	switch {
	case ast.IsIdentifier(p):
		if propertyName(p) || propertyName(x) {
			if p.Text() != x.Text() || propertyName(p) != propertyName(x) {
				return m.differ(p, x)
			}
			return true
		}
		ps, xs := symbolOf(m.c, p), symbolOf(m.c, x)
		if ps == nil || xs == nil {
			if p.Text() != x.Text() {
				return m.differ(p, x)
			}
			return true
		}
		if m.inP(ps) {
			if !m.inC(xs) {
				return m.differ(p, x)
			}
			if q, ok := m.pair[ps]; ok {
				return q == xs || m.differ(p, x)
			}
			if _, ok := m.back[xs]; ok {
				return m.differ(p, x)
			}
			m.pair[ps], m.back[xs] = xs, ps
			return true
		}
		return ps == xs || m.differ(p, x)
	case ast.IsLiteralKind(p.Kind) || ast.IsTemplateLiteralKind(p.Kind):
		if m.pl.source(p) == m.cl.source(x) {
			return true
		}
		if m.literals && !ast.IsTemplateLiteralKind(p.Kind) {
			m.diffs = append(m.diffs, [2]*ast.Node{p, x})
			return true
		}
		return m.differ(p, x)
	case ast.IsPrefixUnaryExpression(p) && p.AsPrefixUnaryExpression().Operator != x.AsPrefixUnaryExpression().Operator,
		ast.IsPostfixUnaryExpression(p) && p.AsPostfixUnaryExpression().Operator != x.AsPostfixUnaryExpression().Operator,
		ast.IsVariableDeclarationList(p) && p.Flags&ast.NodeFlagsBlockScoped != x.Flags&ast.NodeFlagsBlockScoped,
		p.Flags&ast.NodeFlagsOptionalChain != x.Flags&ast.NodeFlagsOptionalChain:
		return m.differ(p, x)
	}
	pc, xc := children(p), children(x)
	if len(pc) == 0 {
		if !slices.Equal(tokens(m.pl.source(p)), tokens(m.cl.source(x))) {
			return m.differ(p, x)
		}
		return true
	}
	if len(pc) != len(xc) {
		return m.differ(p, x)
	}
	for i := range pc {
		if !m.match(pc[i], xc[i]) {
			return false
		}
	}
	return true
}

// dropImports returns the edits that remove the imports of l's file that
// only code gone used (gone reports whether a node is in it); with blank,
// the blank line after them too when they all go.
func dropImports(c *checker.Checker, l *lined, gone func(*ast.Node) bool, blank bool) []Edit {
	used := map[*ast.Node]bool{}
	usedGone := map[*ast.Node]bool{}
	identifiers(l.f.AsNode(), func(id *ast.Node) {
		if ast.IsDeclarationName(id) || id.Parent != nil && ast.IsImportSpecifier(id.Parent) {
			return
		}
		sym := c.GetSymbolAtLocation(id)
		if id.Parent != nil && ast.IsShorthandPropertyAssignment(id.Parent) {
			sym = c.GetShorthandAssignmentValueSymbol(id.Parent)
		}
		if sym == nil || len(sym.Declarations) == 0 {
			return
		}
		d := sym.Declarations[0]
		if !(ast.IsImportSpecifier(d) || ast.IsImportClause(d) || ast.IsNamespaceImport(d)) || ast.GetSourceFileOfNode(d) != l.f {
			return
		}
		if gone(id) {
			usedGone[d] = true
		} else {
			used[d] = true
		}
	})
	var edits []Edit
	imports, whole := 0, -1 // how many imports, and the edit of the last removed whole
	for _, s := range l.f.Statements.Nodes {
		if ast.IsImportDeclaration(s) {
			imports++
		}
		if !ast.IsImportDeclaration(s) || s.AsImportDeclaration().ImportClause == nil || gone(s) {
			continue
		}
		clause := s.AsImportDeclaration().ImportClause
		var all, unused []*ast.Node
		if clause.Name() != nil {
			all = append(all, clause)
		}
		if nb := clause.AsImportClause().NamedBindings; nb != nil {
			if ast.IsNamespaceImport(nb) {
				all = append(all, nb)
			} else {
				all = append(all, nb.AsNamedImports().Elements.Nodes...)
			}
		}
		for _, d := range all {
			if usedGone[d] && !used[d] {
				unused = append(unused, d)
			}
		}
		switch {
		case len(unused) == 0:
		case len(unused) == len(all):
			if a, b, ok := l.lineRange(s); ok {
				edits = append(edits, Edit{File: l.f.FileName(), Start: a, End: b})
				whole = len(edits) - 1
			}
		default:
			if nb := clause.AsImportClause().NamedBindings; nb != nil && ast.IsNamedImports(nb) {
				list := nb.AsNamedImports().Elements
				for i := len(list.Nodes) - 1; i >= 0; i-- {
					if slices.Contains(unused, list.Nodes[i]) {
						edits = append(edits, l.removeAt(list, i))
					}
				}
			}
		}
	}
	if blank && whole >= 0 && len(edits) == imports && whole == len(edits)-1 {
		// Every import goes: the blank line after them too.
		edits[whole].Start, edits[whole].End = l.withBlank(edits[whole].Start, edits[whole].End)
	}
	return edits
}

// references returns the identifiers of the program's code that refer to
// one of syms, but for declaration names other than an import's or
// export's.
func (p *Project) references(c *checker.Checker, syms []*ast.Symbol) []*ast.Node {
	var out []*ast.Node
	for _, f := range p.prog.GetProgram().GetSourceFiles() {
		if f.IsDeclarationFile {
			continue
		}
		identifiers(f.AsNode(), func(id *ast.Node) {
			spec := id.Parent != nil && (ast.IsImportSpecifier(id.Parent) || ast.IsExportSpecifier(id.Parent))
			if (!ast.IsDeclarationName(id) || spec) && slices.Contains(syms, symbolOf(c, id)) {
				out = append(out, id)
			}
		})
	}
	return out
}

// member returns the members of the class or interface named class in
// the file that are called name.
func (l *lined) member(class, name string) (*ast.Node, []*ast.Node, string) {
	for _, s := range l.f.Statements.Nodes {
		if (ast.IsClassDeclaration(s) || ast.IsInterfaceDeclaration(s)) && s.Name() != nil && s.Name().Text() == class {
			var out []*ast.Node
			for _, m := range s.Members() {
				if m.Name() != nil && l.text[l.pos(m.Name()):m.Name().End()] == name {
					out = append(out, m)
				}
			}
			if len(out) == 0 {
				return s, nil, fmt.Sprintf("%s has no member %s", class, name)
			}
			return s, out, ""
		}
	}
	return nil, nil, fmt.Sprintf("the file declares no class or interface %s", class)
}

// RemoveDeadCode returns the edits that delete the declarations named,
// which nothing uses but each other: top-level names, or members as
// Class.member; and the imports only they used. note says whether one
// was exported.
func (p *Project) RemoveDeadCode(ctx context.Context, path string, line int, name string, column int, also []string) (edits []Edit, note, refusal string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, "", why
	}
	c, done := p.prog.GetProgram().GetTypeChecker(ctx)
	defer done()
	var gone []*ast.Node // the statements or members that go
	var syms []*ast.Symbol
	var exported []string
	addNodes := func(label string, nodes []*ast.Node) string {
		for _, n := range nodes {
			switch {
			case ast.IsVariableStatement(n) && len(declared(n)) > 1:
				return fmt.Sprintf("%s is declared with %s in one statement (%s); split it first", label, joinNames(declared(n)), l.where(n))
			case ast.IsModuleDeclaration(n):
				return fmt.Sprintf("%s is a namespace", label)
			case ast.IsConstructorDeclaration(n):
				return "a constructor is not removed"
			}
			if ast.HasSyntacticModifier(n, ast.ModifierFlagsExport) && !slices.Contains(exported, label) {
				exported = append(exported, label)
			}
			var init *ast.Node
			if ast.IsVariableStatement(n) {
				init = n.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes[0].Initializer()
			} else if ast.IsPropertyDeclaration(n) {
				init = n.Initializer()
			}
			if init != nil {
				if e := newEffects(c, l).Of(init); !e.CanDrop() {
					return fmt.Sprintf("the value of %s would no longer be evaluated: %s", label, e.Why)
				}
			}
			if !slices.Contains(gone, n) {
				gone = append(gone, n)
			}
			for _, nm := range topNames(n) {
				if s := c.GetSymbolAtLocation(nm); s != nil && !slices.Contains(syms, s) {
					syms = append(syms, s)
				}
			}
			if !ast.IsVariableStatement(n) && n.Name() != nil {
				if s := c.GetSymbolAtLocation(n.Name()); s != nil && !slices.Contains(syms, s) {
					syms = append(syms, s)
				}
			}
		}
		return ""
	}
	byName := func(n string) string {
		if class, mem, ok := strings.Cut(n, "."); ok {
			owner, ms, why := l.member(class, mem)
			if why != "" {
				return why
			}
			if ast.IsClassDeclaration(owner) {
				for _, m := range ms {
					if why := p.overrides(c, l, owner, m); why != "" {
						return why
					}
				}
			}
			return addNodes(n, ms)
		}
		stmts := l.topLevel(n)
		if len(stmts) == 0 {
			return fmt.Sprintf("the file declares no %s at its top level", n)
		}
		return addNodes(n, stmts)
	}
	if line > 0 {
		var decl *ast.Node
		identifiers(l.f.AsNode(), func(id *ast.Node) {
			start := l.pos(id)
			if decl == nil && id.Text() == name && l.line(start) == line && (column == 0 || start-l.start(line)+1 == column) && ast.IsDeclarationName(id) {
				decl = id.Parent
			}
		})
		if decl == nil {
			return nil, "", fmt.Sprintf("no declaration of %s on line %d", name, line)
		}
		if decl.Parent != nil && (ast.IsClassDeclaration(decl.Parent) || ast.IsInterfaceDeclaration(decl.Parent)) && decl.Parent.Name() != nil {
			name = decl.Parent.Name().Text() + "." + name
		}
	}
	for _, n := range append([]string{name}, also...) {
		if why := byName(n); why != "" {
			return nil, "", why
		}
	}
	in := func(n *ast.Node) bool {
		return slices.ContainsFunc(gone, func(g *ast.Node) bool {
			return ast.GetSourceFileOfNode(g) == ast.GetSourceFileOfNode(n) && n.Pos() >= g.Pos() && n.End() <= g.End()
		})
	}
	for _, r := range p.references(c, syms) {
		if !in(r) {
			rl := newLined(ast.GetSourceFileOfNode(r))
			return nil, "", fmt.Sprintf("%s is still used at %s: %s", r.Text(), rl.where(r), strings.TrimSpace(rl.text[rl.start(rl.line(rl.pos(r))):rl.end(rl.line(rl.pos(r)))]))
		}
	}
	for _, g := range gone {
		a, b, ok := l.lineRange(g)
		if !ok {
			return nil, "", fmt.Sprintf("%s shares its lines with other code", l.part(g))
		}
		a, b = l.withBlank(a, b)
		edits = append(edits, Edit{File: l.f.FileName(), Start: a, End: b})
	}
	edits = append(edits, dropImports(c, l, in, true)...)
	if len(exported) > 0 {
		note = fmt.Sprintf("%s was exported: the module's API changed", strings.Join(exported, ", "))
	}
	return edits, note, ""
}

// overrides returns why the member m of class may not go: it overrides
// a member of a base class, or implements one of an interface.
func (p *Project) overrides(c *checker.Checker, l *lined, class, m *ast.Node) string {
	if m.Name() == nil {
		return ""
	}
	name := l.source(m.Name())
	hs := class.AsClassDeclaration().HeritageClauses
	if hs == nil {
		return ""
	}
	for _, h := range hs.Nodes {
		for _, t := range h.AsHeritageClause().Types.Nodes {
			if typ := c.GetTypeAtLocation(t); typ != nil && c.GetPropertyOfType(typ, name) != nil {
				return fmt.Sprintf("%s.%s implements or overrides a member of %s", class.Name().Text(), name, l.source(t))
			}
		}
	}
	return ""
}

// signature returns the types of fn's parameters and its result, as
// text, to compare functions by.
func signature(c *checker.Checker, fn *ast.Node) []string {
	var out []string
	for _, prm := range fn.Parameters() {
		t := c.TypeToString(c.GetTypeAtLocation(prm.Name()))
		if prm.AsParameterDeclaration().QuestionToken != nil || prm.Initializer() != nil {
			t += "?"
		}
		if prm.AsParameterDeclaration().DotDotDotToken != nil {
			t = "..." + t
		}
		out = append(out, t)
	}
	if sig := c.GetSignatureFromDeclaration(fn); sig != nil {
		out = append(out, "=> "+c.TypeToString(c.GetReturnTypeOfSignature(sig)))
	}
	return out
}

// pairParams pairs the parameters of the pattern function p with those of
// the code function x, by position.
func (m *matcher) pairParams(p, x *ast.Node) {
	pp, xp := p.Parameters(), x.Parameters()
	for i := range min(len(pp), len(xp)) {
		ps, xs := m.c.GetSymbolAtLocation(pp[i].Name()), m.c.GetSymbolAtLocation(xp[i].Name())
		if ps != nil && xs != nil {
			m.pair[ps], m.back[xs] = xs, ps
		}
	}
}

// functionDecl returns the declaration statement of fn: fn, or for a
// function bound to a const, its variable statement.
func functionDecl(fn *ast.Node) *ast.Node {
	if ast.IsArrowFunction(fn) || ast.IsFunctionExpression(fn) {
		return fn.Parent.Parent.Parent
	}
	return fn
}

// MergeDuplicate returns the edits that replace every use of the function
// name (named on line at column, or declared at the top of path) with the
// function keepName of keepPath, whose body is the same, and delete it.
func (p *Project) MergeDuplicate(ctx context.Context, path string, line int, name string, column int, keepPath, keepName string) ([]Edit, string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, why
	}
	c, done := p.prog.GetProgram().GetTypeChecker(ctx)
	defer done()
	if line > 0 {
		var sym *ast.Symbol
		identifiers(l.f.AsNode(), func(id *ast.Node) {
			start := l.pos(id)
			if sym == nil && id.Text() == name && l.line(start) == line && (column == 0 || start-l.start(line)+1 == column) {
				sym = symbolOf(c, id)
			}
		})
		if sym == nil || len(sym.Declarations) == 0 {
			return nil, fmt.Sprintf("no name %s on line %d", name, line)
		}
		l = newLined(ast.GetSourceFileOfNode(sym.Declarations[0]))
	}
	kl, why := p.lined(keepPath)
	if why != "" {
		return nil, why
	}
	if strings.Contains(name, ".") || strings.Contains(keepName, ".") {
		return nil, "methods are not merged; give two functions"
	}
	del, dov, delName, why := l.function(name)
	if why != "" {
		return nil, why
	}
	keep, kov, keepNode, why := kl.function(keepName)
	if why != "" {
		return nil, why
	}
	switch {
	case del == keep:
		return nil, "the function to delete and the one to keep are the same"
	case len(dov) > 0 || len(kov) > 0:
		return nil, "a function with overloads is not merged"
	case del.TypeParameters() != nil || keep.TypeParameters() != nil:
		return nil, "a generic function is not merged"
	case ast.GetFunctionFlags(del) != ast.GetFunctionFlags(keep):
		return nil, fmt.Sprintf("%s and %s differ in being async or a generator", name, keepName)
	}
	if a, b := signature(c, del), signature(c, keep); !slices.Equal(a, b) {
		return nil, fmt.Sprintf("%s and %s have different signatures: (%s) and (%s)", name, keepName, strings.Join(a, ", "), strings.Join(b, ", "))
	}
	m := newMatcher(c, l, kl)
	m.inP, m.inC = within(del), within(keep)
	m.pairParams(del, keep)
	for i, prm := range del.Parameters() {
		if !m.match(prm.Initializer(), keep.Parameters()[i].Initializer()) {
			return nil, fmt.Sprintf("the bodies differ: %s", m.why)
		}
	}
	if !m.match(del.Body(), keep.Body()) {
		return nil, fmt.Sprintf("the bodies differ: %s", m.why)
	}
	delSym, keepSym := c.GetSymbolAtLocation(delName), c.GetSymbolAtLocation(keepNode)
	keepExported := ast.HasSyntacticModifier(functionDecl(keep), ast.ModifierFlagsExport)
	declStmt := functionDecl(del)
	inDel := func(n *ast.Node) bool {
		return ast.GetSourceFileOfNode(n) == l.f && n.Pos() >= declStmt.Pos() && n.End() <= declStmt.End()
	}
	var edits []Edit
	byFile := map[*ast.SourceFile][]*ast.Node{}
	var order []*ast.SourceFile
	for _, r := range p.references(c, []*ast.Symbol{delSym}) {
		if inDel(r) {
			return nil, fmt.Sprintf("%s calls itself (%s); merge by hand", name, l.where(r))
		}
		f := ast.GetSourceFileOfNode(r)
		if byFile[f] == nil {
			order = append(order, f)
		}
		byFile[f] = append(byFile[f], r)
	}
	for _, f := range order {
		fl := newLined(f)
		local := keepName // the name the file knows keep by
		known := f == kl.f
		identifiers(f.AsNode(), func(id *ast.Node) {
			if ast.IsImportSpecifier(id.Parent) && id.Parent.Name() == id && symbolOf(c, id) == keepSym {
				local, known = id.Text(), true
			}
		})
		var importsDel []*ast.Node
		uses := false
		for _, r := range byFile[f] {
			switch {
			case ast.IsImportSpecifier(r.Parent):
				importsDel = append(importsDel, r.Parent)
				continue
			case ast.IsExportSpecifier(r.Parent):
				return nil, fmt.Sprintf("%s re-exports %s; change that first", fl.where(r), name)
			}
			uses = true
			text := local
			if ast.IsShorthandPropertyAssignment(r.Parent) {
				text = r.Text() + ": " + local
			}
			edits = append(edits, Edit{File: f.FileName(), Start: fl.pos(r), End: r.End(), NewText: text})
		}
		if uses && !known {
			if !keepExported {
				return nil, fmt.Sprintf("%s is used in %s, where %s is not seen: export %s first", name, fl.f.FileName(), keepName, keepName)
			}
			if s := c.ResolveName(keepName, byFile[f][0], ast.SymbolFlagsValue, false); s != nil && c.SkipAlias(s) != keepSym {
				return nil, fmt.Sprintf("%s already means something else in %s", keepName, fl.f.FileName())
			}
			at, text := fl.importAt()
			edits = append(edits, Edit{File: f.FileName(), Start: at, End: at, NewText: text(importLine{spec: fl.specFor(kl.f.FileName()), named: []string{keepName}}.text(fl.quote(), fl.semi()))})
		}
		for _, spec := range importsDel {
			list := spec.Parent.AsNamedImports().Elements
			decl := spec.Parent.Parent.Parent
			if len(list.Nodes) == 1 && decl.AsImportDeclaration().ImportClause.Name() == nil {
				if a, b, ok := fl.lineRange(decl); ok {
					edits = append(edits, Edit{File: f.FileName(), Start: a, End: b})
				}
			} else {
				edits = append(edits, fl.removeAt(list, slices.Index(list.Nodes, spec)))
			}
		}
	}
	a, b, ok := l.lineRange(declStmt)
	if !ok {
		return nil, fmt.Sprintf("%s shares its lines with other code", name)
	}
	a, b = l.withBlank(a, b)
	edits = append(edits, Edit{File: l.f.FileName(), Start: a, End: b})
	edits = append(edits, dropImports(c, l, inDel, true)...)
	return edits, ""
}

// Parameterize returns the edits that make the functions, the same but
// for literals, into calls of a new function called name that takes
// those literals as parameters, named params in the order they appear in
// the first function.
func (p *Project) Parameterize(ctx context.Context, path string, functions []string, name string, params []string) ([]Edit, string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, why
	}
	if len(functions) < 2 {
		return nil, "give two functions or more"
	}
	c, done := p.prog.GetProgram().GetTypeChecker(ctx)
	defer done()
	var fns []*ast.Node
	for _, f := range functions {
		fn, ov, _, why := l.function(f)
		switch {
		case why != "":
			return nil, why
		case len(ov) > 0:
			return nil, fmt.Sprintf("%s has overloads", f)
		case fn.TypeParameters() != nil:
			return nil, fmt.Sprintf("%s is generic", f)
		case ast.IsConstructorDeclaration(fn):
			return nil, "constructors are not parameterized"
		}
		fns = append(fns, fn)
	}
	first := fns[0]
	method := ast.IsMethodDeclaration(first)
	for i, fn := range fns[1:] {
		if ast.IsMethodDeclaration(fn) != method || method && fn.Parent != first.Parent {
			return nil, "the functions must all be functions, or all methods of one class"
		}
		if a, b := signature(c, first), signature(c, fn); !slices.Equal(a, b) {
			return nil, fmt.Sprintf("%s and %s have different signatures: (%s) and (%s)", functions[0], functions[i+1], strings.Join(a, ", "), strings.Join(b, ", "))
		}
	}
	// Where each function's literals differ from the first's.
	var at []*ast.Node // the first's literals that differ somewhere, in order
	values := map[*ast.Node][]string{}
	for i, fn := range fns[1:] {
		m := newMatcher(c, l, l)
		m.literals = true
		m.inP, m.inC = within(first), within(fn)
		m.pairParams(first, fn)
		if !m.match(first.Body(), fn.Body()) {
			return nil, fmt.Sprintf("%s and %s differ in more than literals: %s", functions[0], functions[i+1], m.why)
		}
		for _, d := range m.diffs {
			if !slices.Contains(at, d[0]) {
				at = append(at, d[0])
			}
		}
	}
	if len(at) == 0 {
		return nil, fmt.Sprintf("the functions are the same; merge them with merge_duplicate_function")
	}
	slices.SortFunc(at, func(a, b *ast.Node) int { return a.Pos() - b.Pos() })
	if len(params) != len(at) {
		var lits []string
		for _, a := range at {
			lits = append(lits, l.source(a))
		}
		return nil, fmt.Sprintf("the functions differ in %d literals (%s in %s); give a parameter name for each", len(at), strings.Join(lits, ", "), functions[0])
	}
	for i, fn := range fns {
		m := newMatcher(c, l, l)
		m.literals = true
		m.inP, m.inC = within(first), within(fn)
		m.pairParams(first, fn)
		if i > 0 {
			m.match(first.Body(), fn.Body())
		}
		for _, a := range at {
			v := l.source(a)
			for _, d := range m.diffs {
				if d[0] == a {
					v = l.source(d[1])
				}
			}
			values[a] = append(values[a], v)
		}
	}
	types := make([]string, len(at))
	for i, a := range at {
		types[i] = c.TypeToString(c.GetBaseTypeOfLiteralType(c.GetTypeAtLocation(a)))
	}
	// The new function: the first with the literals as parameters.
	owner := functionDecl(first)
	if method {
		for _, mem := range first.Parent.Members() {
			if mem.Name() != nil && mem.Name().Text() == name {
				return nil, fmt.Sprintf("%s already has a member %s", first.Parent.Name().Text(), name)
			}
		}
	} else if why := taken(c, l, name, owner); why != "" {
		return nil, why
	}
	for _, prm := range params {
		for _, fp := range first.Parameters() {
			if fp.Name().Text() == prm {
				return nil, fmt.Sprintf("%s already has a parameter %s", functions[0], prm)
			}
		}
	}
	var rs []repl
	nameNode := first.Name()
	if !method && !ast.IsFunctionDeclaration(first) {
		nameNode = first.Parent.Name()
	}
	rs = append(rs, repl{l.pos(nameNode), nameNode.End(), name})
	var added []string
	for i, prm := range params {
		added = append(added, prm+": "+types[i])
	}
	list := first.ParameterList()
	sep := ", "
	if len(list.Nodes) == 0 {
		sep = ""
	}
	rs = append(rs, repl{list.End(), list.End(), sep + strings.Join(added, ", ")})
	for i, a := range at {
		rs = append(rs, repl{l.pos(a), a.End(), params[i]})
	}
	start, end := l.pos(owner), owner.End()
	if method {
		start, end = l.pos(first), first.End()
	}
	text := strings.TrimPrefix(l.substitute(start, end, rs), "export ")
	var edits []Edit
	last := owner
	if method {
		last = fns[len(fns)-1]
	} else {
		for _, fn := range fns {
			if d := functionDecl(fn); d.Pos() > last.Pos() {
				last = d
			}
		}
	}
	_, lastLine := l.lines(last)
	ind := l.indent(l.line(l.pos(last)))
	insertAt := l.start(lastLine + 1)
	nl := "\n"
	if insertAt == len(l.text) && !strings.HasSuffix(l.text, "\n") {
		nl = "\n\n"
	}
	edits = append(edits, Edit{File: l.f.FileName(), Start: insertAt, End: insertAt, NewText: nl + ind + text + "\n"})
	// Each function calls it.
	for i, fn := range fns {
		var args []string
		for _, prm := range fn.Parameters() {
			args = append(args, prm.Name().Text())
		}
		for _, a := range at {
			args = append(args, values[a][i])
		}
		call := name + "(" + strings.Join(args, ", ") + ")"
		if method {
			call = "this." + call
		}
		if sig := c.GetSignatureFromDeclaration(fn); sig != nil && c.GetReturnTypeOfSignature(sig).Flags()&checker.TypeFlagsVoid == 0 {
			call = "return " + call
		}
		body := fn.Body()
		if !ast.IsBlock(body) {
			edits = append(edits, Edit{File: l.f.FileName(), Start: l.pos(body), End: body.End(), NewText: strings.TrimPrefix(call, "return ")})
			continue
		}
		bind := l.indent(l.line(l.pos(fn)))
		if !method && !ast.IsFunctionDeclaration(fn) {
			bind = l.indent(l.line(l.pos(functionDecl(fn))))
		}
		edits = append(edits, Edit{File: l.f.FileName(), Start: l.pos(body), End: body.End(), NewText: "{\n" + bind + l.unit() + call + l.semi() + "\n" + bind + "}"})
	}
	slices.SortFunc(edits, func(a, b Edit) int { return a.Start - b.Start })
	return edits, ""
}
