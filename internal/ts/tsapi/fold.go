package tsapi

import (
	"context"
	"fmt"
	pathpkg "path"
	"slices"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/checker"
	"github.com/buke/typescript-go-internal/v7/pkg/tspath"
)

// Fold returns the edits that replace code that does what the function
// name of path does with calls of it: every match of its body in the
// solution, its parameters (and this, for a method) standing for any
// expressions. With line, only the matches that start on that line of
// in (path when in is ""). skipped says why matches were left.
func (p *Project) Fold(ctx context.Context, path, name string, line int, in string) (edits []Edit, folded int, skipped []string, refusal string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, 0, nil, why
	}
	fn, ov, nameNode, why := l.function(name)
	if why != "" {
		return nil, 0, nil, why
	}
	prog := p.prog.GetProgram()
	c, done := prog.GetTypeChecker(ctx)
	defer done()
	switch {
	case len(ov) > 0:
		return nil, 0, nil, fmt.Sprintf("%s has overloads", name)
	case fn.TypeParameters() != nil:
		return nil, 0, nil, fmt.Sprintf("%s is generic", name)
	case ast.IsConstructorDeclaration(fn):
		return nil, 0, nil, "a constructor is not folded"
	case ast.GetFunctionFlags(fn)&(ast.FunctionFlagsAsync|ast.FunctionFlagsGenerator) != 0:
		return nil, 0, nil, fmt.Sprintf("%s is async or a generator; a call of it would not do what its body does", name)
	}
	method := ast.IsMethodDeclaration(fn)
	if method {
		if ast.IsStatic(fn) {
			return nil, 0, nil, "a static method is not folded"
		}
		if fn.ModifierFlags()&ast.ModifierFlagsPrivate == 0 && !ast.IsPrivateIdentifier(fn.Name()) && p.extended(c, fn.Parent) {
			return nil, 0, nil, fmt.Sprintf("%s is a method a subclass could override", name)
		}
	}
	sym := c.GetSymbolAtLocation(nameNode)
	// The pattern: one expression, or the statements of a body without a
	// result.
	var expr *ast.Node
	var stmts []*ast.Node
	body := fn.Body()
	switch {
	case !ast.IsBlock(body):
		expr = body
	case len(body.Statements()) == 1 && ast.IsReturnStatement(body.Statements()[0]) && body.Statements()[0].Expression() != nil:
		expr = body.Statements()[0].Expression()
	default:
		returns := false
		var visit func(n *ast.Node) bool
		visit = func(n *ast.Node) bool {
			if ast.IsFunctionLike(n) {
				return false
			}
			if ast.IsReturnStatement(n) {
				returns = true
			}
			n.ForEachChild(visit)
			return false
		}
		body.ForEachChild(visit)
		if returns {
			return nil, 0, nil, fmt.Sprintf("%s returns from inside its body; only a body that is one return, or statements that do not return, is folded. To reuse it where its statements are copied, extract_function each copy of its statements, then merge_duplicate_function the result into %s", name, name)
		}
		stmts = body.Statements()
		if len(stmts) == 0 {
			return nil, 0, nil, fmt.Sprintf("%s has an empty body", name)
		}
	}
	holes := map[*ast.Symbol]int{}
	var paramTypes []string
	for i, prm := range fn.Parameters() {
		if !ast.IsIdentifier(prm.Name()) || prm.AsParameterDeclaration().DotDotDotToken != nil || ast.IsThisParameter(prm) {
			return nil, 0, nil, fmt.Sprintf("%s has a parameter that is not a plain name (%s)", name, l.where(prm))
		}
		s := c.GetSymbolAtLocation(prm.Name())
		holes[s] = i
		paramTypes = append(paramTypes, c.TypeToString(c.GetTypeAtLocation(prm.Name())))
		if where := l.assigns(body, s, c); where != "" {
			return nil, 0, nil, fmt.Sprintf("%s assigns its parameter %s (%s), which code passing it could not see", name, prm.Name().Text(), where)
		}
	}
	nholes := len(holes)
	if method {
		nholes++
	}
	// A parameter the body uses only by its properties (d.plants) may
	// stand for an object that a call makes of what stands for each, where
	// the code does not read them off one value.
	objects := map[int][]string{}
	fields := map[*ast.Symbol]map[string]int{}
	nall := nholes
	for i, prm := range fn.Parameters() {
		s := c.GetSymbolAtLocation(prm.Name())
		if props := propertiesOnly(c, body, s); len(props) > 0 {
			fields[s] = map[string]int{}
			for _, f := range props {
				fields[s][f] = nall
				nall++
			}
			objects[i] = props
		}
	}
	st := setterOf(c, fn, expr, holes)
	inFn := within(fn)
	exported := ast.HasSyntacticModifier(functionDecl(fn), ast.ModifierFlagsExport) || method
	if in == "" {
		in = path
	}
	in = tspath.NormalizePath(in)
	// One match of the pattern at x (an expression) or at the window of
	// statements xs.
	type found struct {
		root  []*ast.Node
		args  []*ast.Node
		texts []string
		runs  [][]*ast.Node
		uses  [][]*ast.Node
		where string
		obj   bool // parameters used by their properties stand for an object of them
	}
	var matches []found
	try := func(cl *lined, roots []*ast.Node, pats []*ast.Node, obj bool) {
		m := newMatcher(c, l, cl)
		m.holes, m.this = holes, method
		n := nholes
		if obj {
			m.fields, n = fields, nall
		}
		m.args, m.uses = make([]*ast.Node, n), make([][]*ast.Node, n)
		m.texts, m.runs = make([]string, n), make([][]*ast.Node, n)
		m.inP = inFn
		enclosing := ast.GetContainingFunction(roots[0])
		if enclosing != nil {
			m.inC = within(enclosing)
		} else {
			m.inC = func(s *ast.Symbol) bool {
				return slices.ContainsFunc(s.Declarations, func(d *ast.Node) bool { return ast.GetSourceFileOfNode(d) == cl.f })
			}
		}
		for i := range pats {
			if !m.match(pats[i], roots[i]) {
				return
			}
		}
		for i, a := range m.args {
			if a == nil && !(obj && objects[i] != nil) {
				what := "this"
				if i < len(fn.Parameters()) {
					what = fn.Parameters()[i].Name().Text()
				}
				skipped = append(skipped, fmt.Sprintf("%s: nothing stands for %s, which the body does not use", cl.where(roots[0]), what))
				return
			}
		}
		matches = append(matches, found{root: roots, args: m.args, texts: m.texts, runs: m.runs, uses: m.uses, where: cl.where(roots[0]), obj: obj})
	}
	// tryBoth tries a match as it is, then with the parameters used by
	// their properties standing for an object of them.
	tryBoth := func(cl *lined, roots []*ast.Node, pats []*ast.Node) {
		before := len(matches)
		try(cl, roots, pats, false)
		if len(matches) == before && len(fields) > 0 {
			try(cl, roots, pats, true)
		}
	}
	// trySetter matches an object literal of the setter's type that ends
	// with its property: the others stand for the object it sets.
	trySetter := func(cl *lined, n *ast.Node) bool {
		t := c.GetContextualType(n, checker.ContextFlagsNone)
		props := n.AsObjectLiteralExpression().Properties.Nodes
		if t == nil || !sameType(t, st.typ) || len(props) == 0 {
			return false
		}
		last := props[len(props)-1]
		if !ast.IsPropertyAssignment(last) || last.Name() == nil || ast.IsComputedPropertyName(last.Name()) || cl.source(last.Name()) != st.field {
			return false
		}
		m := found{root: []*ast.Node{n}, args: make([]*ast.Node, nholes), texts: make([]string, nholes), runs: make([][]*ast.Node, nholes), uses: make([][]*ast.Node, nholes), where: cl.where(n)}
		m.args[st.value], m.uses[st.value] = last.Initializer(), []*ast.Node{st.valueUse}
		m.uses[st.spread] = []*ast.Node{st.spreadUse}
		rest := props[:len(props)-1]
		if len(rest) == 1 && ast.IsSpreadAssignment(rest[0]) {
			m.args[st.spread] = rest[0].Expression()
		} else {
			var texts []string
			for _, r := range rest {
				texts = append(texts, cl.source(r))
				switch {
				case ast.IsPropertyAssignment(r):
					m.runs[st.spread] = append(m.runs[st.spread], r.Initializer())
				case ast.IsShorthandPropertyAssignment(r):
					m.runs[st.spread] = append(m.runs[st.spread], r.Name())
				case ast.IsSpreadAssignment(r):
					m.runs[st.spread] = append(m.runs[st.spread], r.Expression())
				default:
					return false
				}
			}
			m.args[st.spread], m.texts[st.spread] = n, "{}"
			if len(texts) > 0 {
				m.texts[st.spread] = "{ " + strings.Join(texts, ", ") + " }"
			}
		}
		matches = append(matches, m)
		return true
	}
	for _, f := range prog.GetSourceFiles() {
		if f.IsDeclarationFile {
			continue
		}
		cl := newLined(f)
		onLine := func(n *ast.Node) bool {
			return line == 0 || f.FileName() == in && cl.line(cl.pos(n)) == line
		}
		outside := func(n *ast.Node) bool {
			return f != l.f || n.End() <= fn.Pos() || n.Pos() >= fn.End()
		}
		if expr != nil {
			cl.expressions(func(n *ast.Node) {
				if n.Kind == expr.Kind && outside(n) && onLine(n) && !slices.ContainsFunc(matches, func(m found) bool {
					return m.root[0].Pos() <= n.Pos() && n.End() <= m.root[0].End() && ast.GetSourceFileOfNode(m.root[0]) == f
				}) && !(st != nil && trySetter(cl, n)) {
					tryBoth(cl, []*ast.Node{n}, []*ast.Node{expr})
				}
			})
			continue
		}
		cl.lists(func(_ *ast.Node, list []*ast.Node) {
			for i := 0; i+len(stmts) <= len(list); i++ {
				if list[i].Kind == stmts[0].Kind && outside(list[i]) && onLine(list[i]) {
					before := len(matches)
					tryBoth(cl, list[i:i+len(stmts)], stmts)
					if len(matches) > before {
						i += len(stmts) - 1
					}
				}
			}
		})
	}
	// Each match becomes a call, where that does the same.
	imported := map[*ast.SourceFile]bool{}
	for _, m := range matches {
		root := m.root[0]
		cl := newLined(ast.GetSourceFileOfNode(root))
		ef := newEffects(c, cl)
		skip := func(why string) { skipped = append(skipped, m.where+": "+why) }
		var holeNodes []*ast.Node
		for i, a := range m.args {
			switch {
			case m.runs[i] != nil:
				holeNodes = append(holeNodes, m.runs[i]...)
			case m.texts[i] == "" && a != nil:
				holeNodes = append(holeNodes, a)
			}
		}
		ok := true
		for i, a := range m.args {
			if a == nil {
				continue // an object of the holes of its properties
			}
			if m.texts[i] != "" {
				// Constant text needs no check; a run, each of its operands.
				for _, r := range m.runs[i] {
					var past []*ast.Node
					for _, rt := range m.root {
						past = append(past, parts(rt, rt.End(), holeNodes)...)
					}
					if why := ef.Check(Move{Expr: r, Past: past, Times: Once}); why != "" {
						skip(why)
						ok = false
					}
				}
				if !ok {
					break
				}
				continue
			}
			if i < len(paramTypes) {
				pt := c.GetTypeAtLocation(fn.Parameters()[i].Name())
				if at := c.GetTypeAtLocation(a); at != nil && pt != nil && !c.IsTypeAssignableTo(at, pt) {
					skip(fmt.Sprintf("%s is a %s, not a %s", cl.source(a), c.TypeToString(at), paramTypes[i]))
					ok = false
					break
				}
			}
			times := Once
			if len(m.uses[i]) > 1 {
				times = Shared
			} else if t, _, why := l.timesIn(m.uses[i][0], body, ef.runsDuring); why != "" {
				times = Several
			} else if t != Once {
				times = Maybe
			}
			var past []*ast.Node
			for _, r := range m.root {
				past = append(past, parts(r, r.End(), holeNodes)...)
			}
			for j, b := range m.args {
				if j != i && m.texts[j] == "" && b != nil {
					past = append(past, b)
				}
			}
			if why := ef.Check(Move{Expr: a, Past: past, Times: times}); why != "" {
				skip(why)
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		// Locals the statements declare must not be used after them.
		if stmts != nil {
			last := m.root[len(m.root)-1]
			var leak string
			for _, r := range m.root {
				for _, n := range declared(r) {
					s := c.GetSymbolAtLocation(n)
					identifiers(r.Parent, func(id *ast.Node) {
						if leak == "" && id.Pos() >= last.End() && symbolOf(c, id) == s {
							leak = fmt.Sprintf("%s, declared there, is used after it (%s)", n.Text(), cl.where(id))
						}
					})
				}
			}
			if leak != "" {
				skip(leak)
				continue
			}
		}
		// The call, and how this file reaches the function.
		callee := name
		if method {
			callee = operand(cl, m.args[len(holes)]) + "." + fn.Name().Text()
		} else {
			r := c.ResolveName(nameNode.Text(), root, ast.SymbolFlagsValue, false)
			switch {
			case r != nil && c.SkipAlias(r) == sym:
				callee = nameNode.Text()
				for _, id := range cl.importNames(c, sym) {
					callee = id
				}
			case r != nil:
				skip(fmt.Sprintf("%s means something else there", nameNode.Text()))
				continue
			case !exported:
				skip(fmt.Sprintf("%s is not exported, so %s cannot call it", nameNode.Text(), pathpkg.Base(cl.f.FileName())))
				continue
			default:
				callee = nameNode.Text()
				if !imported[cl.f] {
					imported[cl.f] = true
					at, text := cl.importAt()
					edits = append(edits, Edit{File: cl.f.FileName(), Start: at, End: at, NewText: text(importLine{spec: cl.specFor(l.f.FileName()), named: []string{callee}}.text(cl.quote(), cl.semi()))})
				}
			}
		}
		var args []string
		for i, a := range m.args {
			if i >= len(fn.Parameters()) {
				break
			}
			if m.obj && objects[i] != nil {
				var props []string
				for _, k := range objects[i] {
					b := m.args[fields[c.GetSymbolAtLocation(fn.Parameters()[i].Name())][k]]
					if ast.IsIdentifier(b) && b.Text() == k {
						props = append(props, k)
					} else {
						props = append(props, k+": "+cl.source(b))
					}
				}
				args = append(args, "{ "+strings.Join(props, ", ")+" }")
				continue
			}
			s := cl.source(a)
			if m.texts[i] != "" {
				s = m.texts[i]
			} else if ast.IsBinaryExpression(a) && a.AsBinaryExpression().OperatorToken.Kind == ast.KindCommaToken {
				s = "(" + s + ")"
			}
			args = append(args, s)
		}
		call := callee + "(" + strings.Join(args, ", ") + ")"
		if expr != nil {
			edits = append(edits, Edit{File: cl.f.FileName(), Start: cl.pos(root), End: root.End(), NewText: call})
		} else {
			last := m.root[len(m.root)-1]
			edits = append(edits, Edit{File: cl.f.FileName(), Start: cl.pos(root), End: last.End(), NewText: call + cl.semi()})
		}
		folded++
	}
	if folded == 0 {
		if len(skipped) > 0 {
			return nil, 0, skipped, fmt.Sprintf("no match of %s's body could become a call: %s", name, strings.Join(skipped, "; "))
		}
		return nil, 0, nil, fmt.Sprintf("no code matches the body of %s", name)
	}
	return edits, folded, skipped, ""
}

// propertiesOnly returns the properties of the parameter s that body
// reads, in the order it first reads them, when it uses s only so (s.f,
// neither assigned nor called) and s is an object other than an array;
// or nil.
func propertiesOnly(c *checker.Checker, body *ast.Node, s *ast.Symbol) []string {
	if s == nil || s.ValueDeclaration == nil {
		return nil
	}
	if t := c.GetTypeAtLocation(s.ValueDeclaration.Name()); t == nil || t.Flags()&checker.TypeFlagsObject == 0 || c.IsArrayType(t) {
		return nil
	}
	var props []string
	only := true
	identifiers(body, func(id *ast.Node) {
		if symbolOf(c, id) != s {
			return
		}
		p := id.Parent
		if !ast.IsPropertyAccessExpression(p) || p.Expression() != id || !ast.IsIdentifier(p.Name()) || written(p) || ast.IsCallExpression(p.Parent) && p.Parent.Expression() == p {
			only = false
			return
		}
		if !slices.Contains(props, p.Name().Text()) {
			props = append(props, p.Name().Text())
		}
	})
	if !only {
		return nil
	}
	return props
}

// setter is a function whose body is { ...p, f: v }, p and v parameters.
type setter struct {
	field               string
	spread, value       int           // the positions of p and v
	spreadUse, valueUse *ast.Node     // their uses in the body
	typ                 *checker.Type // p's type
}

// setterOf returns fn as a setter, when its body, the expression expr, is
// { ...p, f: v }; or nil.
func setterOf(c *checker.Checker, fn, expr *ast.Node, holes map[*ast.Symbol]int) *setter {
	if expr == nil || !ast.IsObjectLiteralExpression(expr) || !ast.IsFunctionDeclaration(fn) && !ast.IsArrowFunction(fn) && !ast.IsFunctionExpression(fn) {
		return nil
	}
	props := expr.AsObjectLiteralExpression().Properties.Nodes
	if len(props) != 2 || !ast.IsSpreadAssignment(props[0]) || !ast.IsPropertyAssignment(props[1]) || props[1].Name() == nil || !ast.IsIdentifier(props[1].Name()) {
		return nil
	}
	p, v := props[0].Expression(), props[1].Initializer()
	if !ast.IsIdentifier(p) || !ast.IsIdentifier(v) {
		return nil
	}
	ps, vs := symbolOf(c, p), symbolOf(c, v)
	i, ok1 := holes[ps]
	j, ok2 := holes[vs]
	if !ok1 || !ok2 || i == j {
		return nil
	}
	return &setter{field: props[1].Name().Text(), spread: i, value: j, spreadUse: p, valueUse: v, typ: c.GetTypeAtLocation(fn.Parameters()[i].Name())}
}

// sameType reports whether a and b are the same type, or name the same
// declaration.
func sameType(a, b *checker.Type) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Symbol() != nil && a.Symbol() == b.Symbol() {
		return true
	}
	return a.Alias() != nil && b.Alias() != nil && a.Alias().Symbol() == b.Alias().Symbol()
}

// importNames returns the names the file imports sym by.
func (l *lined) importNames(c *checker.Checker, sym *ast.Symbol) []string {
	var out []string
	identifiers(l.f.AsNode(), func(id *ast.Node) {
		if id.Parent != nil && ast.IsImportSpecifier(id.Parent) && id.Parent.Name() == id {
			if s := c.GetSymbolAtLocation(id); s != nil && c.SkipAlias(s) == sym {
				out = append(out, id.Text())
			}
		}
	})
	return out
}

// extended reports whether a class of the program extends class.
func (p *Project) extended(c *checker.Checker, class *ast.Node) bool {
	if class.Name() == nil {
		return true
	}
	sym := c.GetSymbolAtLocation(class.Name())
	found := false
	for _, f := range p.prog.GetProgram().GetSourceFiles() {
		var visit func(n *ast.Node) bool
		visit = func(n *ast.Node) bool {
			if found {
				return true
			}
			if ast.IsHeritageClause(n) && n.AsHeritageClause().Token == ast.KindExtendsKeyword {
				for _, t := range n.AsHeritageClause().Types.Nodes {
					if e := t.Expression(); e != nil && ast.IsIdentifier(e) && symbolOf(c, e) == sym {
						found = true
					}
				}
			}
			n.ForEachChild(visit)
			return found
		}
		visit(f.AsNode())
	}
	return found
}
