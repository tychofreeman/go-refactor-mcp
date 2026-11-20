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
	l, why := p.Lined(path)
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
		if fn.ModifierFlags()&ast.ModifierFlagsPrivate == 0 && !ast.IsPrivateIdentifier(fn.Name()) {
			if by := p.overrider(c, fn.Parent, fn.Name().Text()); by != "" {
				return nil, 0, nil, fmt.Sprintf("%s is overridden by %s, whose body a call could run instead", name, by)
			}
		}
	}
	sym := c.GetSymbolAtLocation(nameNode)
	// The pattern: one expression, or the statements of a body, which
	// end in a return if any statement returns.
	var expr *ast.Node
	var stmts []*ast.Node
	returning := false       // stmts end in a return
	held := -1               // the statement declaring what stmts return, when a copy may go on after them
	var returned *ast.Symbol // the name stmts' only return, their last, returns
	back := -1               // the parameter the body assigns and returns: a copy of the statements before the return assigns the call to what stands for it
	body := fn.Body()
	switch {
	case !ast.IsBlock(body):
		expr = body
	case len(body.Statements()) == 1 && ast.IsReturnStatement(body.Statements()[0]) && body.Statements()[0].Expression() != nil:
		expr = body.Statements()[0].Expression()
	default:
		bare, returns := false, 0
		var visit func(n *ast.Node) bool
		visit = func(n *ast.Node) bool {
			if ast.IsFunctionLike(n) {
				return false
			}
			if ast.IsReturnStatement(n) {
				returning = true
				returns++
				bare = bare || n.Expression() == nil
			}
			n.ForEachChild(visit)
			return false
		}
		body.ForEachChild(visit)
		stmts = body.Statements()
		if len(stmts) == 0 {
			return nil, 0, nil, fmt.Sprintf("%s has an empty body", name)
		}
		if returning && (bare || !ast.IsReturnStatement(stmts[len(stmts)-1])) {
			return nil, 0, nil, fmt.Sprintf("%s returns without a value, or does not end in a return; only a body that is one return, statements ending in a return where every return gives a value, or statements that do not return, is folded. To reuse it where its statements are copied, extract_function each copy of its statements, then merge_duplicate_function the result into %s", name, name)
		}
		// A body that ends in return x, its only return, x a local it
		// declares, also matches a copy of the statements before that
		// return, which goes on after them using x.
		if ret := stmts[len(stmts)-1]; returning && returns == 1 && ast.IsIdentifier(ret.Expression()) {
			x := c.GetSymbolAtLocation(ret.Expression())
			returned = x
			for k, s := range stmts[:len(stmts)-1] {
				if d := onlyDeclaration(s); d != nil && d.Initializer() != nil && c.GetSymbolAtLocation(d.Name()) == x {
					held = k
				}
			}
		}
	}
	holes := map[*ast.Symbol]int{}
	var paramTypes []string
	for i, prm := range fn.Parameters() {
		if !ast.IsIdentifier(prm.Name()) || prm.AsParameterDeclaration().DotDotDotToken != nil || ast.IsThisParameter(prm) {
			return nil, 0, nil, fmt.Sprintf("%s has a parameter that is not a plain name (%s)", name, l.Where(prm))
		}
		s := c.GetSymbolAtLocation(prm.Name())
		holes[s] = i
		paramTypes = append(paramTypes, c.TypeToString(c.GetTypeAtLocation(prm.Name())))
		if where := l.Assigns(body, s, c); where != "" {
			if s != returned || back >= 0 {
				return nil, 0, nil, fmt.Sprintf("%s assigns its parameter %s (%s), which code passing it could not see", name, prm.Name().Text(), where)
			}
			back = i
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
		root   []*ast.Node
		args   []*ast.Node
		texts  []string
		runs   [][]*ast.Node
		uses   [][]*ast.Node
		where  string
		obj    bool // parameters used by their properties stand for an object of them
		goesOn bool // a copy of the statements before the return, which goes on using what it returns
		back   bool // a copy of the statements before the return, which assigns what stands for the parameter it returns
	}
	var matches []found
	try := func(cl *Lined, roots []*ast.Node, pats []*ast.Node, obj bool) {
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
				return slices.ContainsFunc(s.Declarations, func(d *ast.Node) bool { return ast.GetSourceFileOfNode(d) == cl.File })
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
				skipped = append(skipped, fmt.Sprintf("%s: nothing stands for %s, which the body does not use", cl.Where(roots[0]), what))
				return
			}
		}
		matches = append(matches, found{root: roots, args: m.args, texts: m.texts, runs: m.runs, uses: m.uses, where: cl.Where(roots[0]), obj: obj})
	}
	// tryBoth tries a match as it is, then with the parameters used by
	// their properties standing for an object of them.
	tryBoth := func(cl *Lined, roots []*ast.Node, pats []*ast.Node) {
		before := len(matches)
		try(cl, roots, pats, false)
		if len(matches) == before && len(fields) > 0 {
			try(cl, roots, pats, true)
		}
	}
	// trySetter matches an object literal of the setter's type that ends
	// with its property: the others stand for the object it sets.
	trySetter := func(cl *Lined, n *ast.Node) bool {
		t := c.GetContextualType(n, checker.ContextFlagsNone)
		props := n.AsObjectLiteralExpression().Properties.Nodes
		if t == nil || !sameType(t, st.typ) || len(props) == 0 {
			return false
		}
		last := props[len(props)-1]
		if !ast.IsPropertyAssignment(last) || last.Name() == nil || ast.IsComputedPropertyName(last.Name()) || cl.Source(last.Name()) != st.field {
			return false
		}
		m := found{root: []*ast.Node{n}, args: make([]*ast.Node, nholes), texts: make([]string, nholes), runs: make([][]*ast.Node, nholes), uses: make([][]*ast.Node, nholes), where: cl.Where(n)}
		m.args[st.value], m.uses[st.value] = last.Initializer(), []*ast.Node{st.valueUse}
		m.uses[st.spread] = []*ast.Node{st.spreadUse}
		rest := props[:len(props)-1]
		if len(rest) == 1 && ast.IsSpreadAssignment(rest[0]) {
			m.args[st.spread] = rest[0].Expression()
		} else {
			var texts []string
			for _, r := range rest {
				texts = append(texts, cl.Source(r))
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
			return line == 0 || f.FileName() == in && cl.Line(cl.Pos(n)) == line
		}
		outside := func(n *ast.Node) bool {
			return f != l.File || n.End() <= fn.Pos() || n.Pos() >= fn.End()
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
			shortest := len(stmts)
			if held >= 0 || back >= 0 {
				shortest--
			}
			for i := 0; i+shortest <= len(list); i++ {
				if list[i].Kind != stmts[0].Kind || !outside(list[i]) || !onLine(list[i]) {
					continue
				}
				before := len(matches)
				if i+len(stmts) <= len(list) && back < 0 {
					tryBoth(cl, list[i:i+len(stmts)], stmts)
				}
				if len(matches) == before && (held >= 0 || back >= 0) {
					tryBoth(cl, list[i:i+shortest], stmts[:shortest])
					if len(matches) > before {
						matches[len(matches)-1].goesOn = held >= 0
						matches[len(matches)-1].back = back >= 0
					}
				}
				if len(matches) > before {
					i += len(matches[len(matches)-1].root) - 1
				}
			}
		})
		// One expression statement also matches an arrow's expression body.
		if len(stmts) == 1 && ast.IsExpressionStatement(stmts[0]) {
			cl.expressions(func(n *ast.Node) {
				if ast.IsArrowFunction(n) && !ast.IsBlock(n.Body()) && outside(n) && onLine(n.Body()) {
					tryBoth(cl, []*ast.Node{n.Body()}, []*ast.Node{stmts[0].Expression()})
				}
			})
		}
	}
	// Each match becomes a call, where that does the same.
	// A body this small matches code that only looks like it: without
	// line, the model chooses among the matches.
	if line == 0 && expr != nil && slight(expr) && len(matches) > 1 {
		var list []string
		for _, m := range matches {
			list = append(list, m.where+" `"+newLined(ast.GetSourceFileOfNode(m.root[0])).Source(m.root[0])+"`")
		}
		return nil, 0, nil, fmt.Sprintf("%s's body, %s, is small enough to match code unrelated to it; give line to fold the match on it (several lines: one fold each, in a batch). It matches %s", name, l.Source(expr), strings.Join(list, ", "))
	}
	imported := map[*ast.SourceFile]bool{}
	for _, m := range matches {
		root := m.root[0]
		cl := newLined(ast.GetSourceFileOfNode(root))
		ef := newEffects(c, cl)
		skip := func(why string) { skipped = append(skipped, m.where+": "+why) }
		if isAssigned(root) {
			continue // a target is written, not a value a call could stand for
		}
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
					skip(fmt.Sprintf("%s is a %s, not a %s", cl.Source(a), c.TypeToString(at), paramTypes[i]))
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
			for k, r := range m.root {
				if m.goesOn && k == held {
					continue // what the call returns, used after it
				}
				for _, n := range Declared(r) {
					s := c.GetSymbolAtLocation(n)
					Identifiers(r.Parent, func(id *ast.Node) {
						if leak == "" && id.Pos() >= last.End() && symbolOf(c, id) == s {
							leak = fmt.Sprintf("%s, declared there, is used after it (%s)", n.Text(), cl.Where(id))
						}
					})
				}
			}
			if leak != "" {
				skip(leak)
				continue
			}
		}
		// A copy that returns becomes return f(...), where the function it
		// is in returns what f does; one that goes on declares what f returns
		// as the copy declared it, when that takes it.
		if returning && !m.goesOn && !m.back {
			if why := returnsTaken(c, fn, ast.GetContainingFunction(root), name); why != "" {
				skip(why)
				continue
			}
		}
		if m.goesOn {
			d := onlyDeclaration(m.root[held])
			want := c.GetReturnTypeOfSignature(c.GetSignatureFromDeclaration(fn))
			if have := c.GetTypeAtLocation(d.Name()); !c.IsTypeAssignableTo(want, have) {
				skip(fmt.Sprintf("%s is a %s, which the result of %s, %s, is not", d.Name().Text(), c.TypeToString(have), name, c.TypeToString(want)))
				continue
			}
		}
		if m.back {
			y := m.args[back]
			pn := fn.Parameters()[back].Name().Text()
			if y == nil || m.texts[back] != "" || !ast.IsIdentifier(y) {
				skip(fmt.Sprintf("%s stands for %s, which %s assigns; only a variable can take the call's result back", cl.Source(root), pn, name))
				continue
			}
			x := symbolOf(c, y)
			named := false
			for i, a := range m.args {
				named = named || i != back && a != nil && slices.ContainsFunc(identifiersOf(a), func(id *ast.Node) bool { return symbolOf(c, id) == x })
			}
			if named || !unseen(c, x, root) {
				skip(fmt.Sprintf("%s could be seen while %s runs, which writes it only at the end", y.Text(), name))
				continue
			}
			want := c.GetReturnTypeOfSignature(c.GetSignatureFromDeclaration(fn))
			if have := c.GetTypeAtLocation(y); !c.IsTypeAssignableTo(want, have) {
				skip(fmt.Sprintf("%s is a %s, which the result of %s, %s, is not", y.Text(), c.TypeToString(have), name, c.TypeToString(want)))
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
				skip(fmt.Sprintf("%s is not exported, so %s cannot call it", nameNode.Text(), pathpkg.Base(cl.File.FileName())))
				continue
			default:
				callee = nameNode.Text()
				if !imported[cl.File] {
					imported[cl.File] = true
					at, text := cl.importAt()
					edits = append(edits, Edit{File: cl.File.FileName(), Start: at, End: at, NewText: text(importLine{spec: cl.specFor(l.File.FileName()), named: []string{callee}}.text(cl.quote(), cl.Semi()))})
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
						props = append(props, k+": "+cl.Source(b))
					}
				}
				args = append(args, "{ "+strings.Join(props, ", ")+" }")
				continue
			}
			s := cl.Source(a)
			if m.texts[i] != "" {
				s = m.texts[i]
			} else if ast.IsBinaryExpression(a) && a.AsBinaryExpression().OperatorToken.Kind == ast.KindCommaToken {
				s = "(" + s + ")"
			}
			args = append(args, s)
		}
		call := callee + "(" + strings.Join(args, ", ") + ")"
		if expr != nil || isArrowBody(root) {
			edits = append(edits, Edit{File: cl.File.FileName(), Start: cl.Pos(root), End: root.End(), NewText: call})
		} else {
			last := m.root[len(m.root)-1]
			text := call + cl.Semi()
			switch {
			case m.goesOn:
				decl := m.root[held]
				text = cl.Text[cl.Pos(decl):cl.Pos(onlyDeclaration(decl).Initializer())] + text
			case m.back:
				text = cl.Source(m.args[back]) + " = " + text
			case returning:
				text = "return " + text
			}
			edits = append(edits, Edit{File: cl.File.FileName(), Start: cl.Pos(root), End: last.End(), NewText: text})
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
	Identifiers(body, func(id *ast.Node) {
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
func (l *Lined) importNames(c *checker.Checker, sym *ast.Symbol) []string {
	var out []string
	Identifiers(l.File.AsNode(), func(id *ast.Node) {
		if id.Parent != nil && ast.IsImportSpecifier(id.Parent) && id.Parent.Name() == id {
			if s := c.GetSymbolAtLocation(id); s != nil && c.SkipAlias(s) == sym {
				out = append(out, id.Text())
			}
		}
	})
	return out
}

// overrider says where a class of the program that extends class,
// directly or not, declares a member named name: "Minus (a.ts:11)"; or
// "" when none does. A class with no name, or one extending it, counts
// as one, since what it declares cannot be followed by name.
func (p *Project) overrider(c *checker.Checker, class *ast.Node, name string) string {
	if class.Name() == nil {
		return "a class with no name"
	}
	var classes []*ast.Node
	for _, f := range p.prog.GetProgram().GetSourceFiles() {
		Inspect(f.AsNode(), func(n *ast.Node) bool {
			if ast.IsClassLike(n) && n != class {
				classes = append(classes, n)
			}
			return true
		})
	}
	below := map[*ast.Symbol]bool{c.GetSymbolAtLocation(class.Name()): true}
	extends := func(k *ast.Node) bool {
		for _, h := range k.ClassLikeData().HeritageClauses.Nodes {
			if h.AsHeritageClause().Token != ast.KindExtendsKeyword {
				continue
			}
			for _, t := range h.AsHeritageClause().Types.Nodes {
				if e := t.Expression(); e != nil && ast.IsIdentifier(e) && below[symbolOf(c, e)] {
					return true
				}
			}
		}
		return false
	}
	for grew := true; grew; {
		grew = false
		for _, k := range classes {
			if k.ClassLikeData().HeritageClauses == nil || !extends(k) {
				continue
			}
			kl := newLined(ast.GetSourceFileOfNode(k))
			if k.Name() == nil {
				return "a class with no name (" + kl.Where(k) + ")"
			}
			ks := c.GetSymbolAtLocation(k.Name())
			if below[ks] {
				continue
			}
			for _, m := range k.Members() {
				if m.Name() != nil && kl.Source(m.Name()) == name {
					return k.Name().Text() + " (" + kl.Where(m) + ")"
				}
			}
			below[ks], grew = true, true
		}
	}
	return ""
}

// isArrowBody reports whether n is an arrow function's expression body.
func isArrowBody(n *ast.Node) bool {
	return n.Parent != nil && ast.IsArrowFunction(n.Parent) && n.Parent.Body() == n
}

// returnsTaken says why a copy of fn's statements in the function
// enclosing cannot become return fn(...): enclosing is async or a
// generator, or its return type does not take fn's; or "".
func returnsTaken(c *checker.Checker, fn, enclosing *ast.Node, name string) string {
	if enclosing == nil {
		return "it is in no function"
	}
	what := "the function it is in"
	if n := enclosing.Name(); n != nil && ast.IsIdentifier(n) {
		what = n.Text()
	}
	if ast.GetFunctionFlags(enclosing)&(ast.FunctionFlagsAsync|ast.FunctionFlagsGenerator) != 0 {
		return fmt.Sprintf("%s is async or a generator, so its return is not the value it returns", what)
	}
	have := c.GetReturnTypeOfSignature(c.GetSignatureFromDeclaration(enclosing))
	want := c.GetReturnTypeOfSignature(c.GetSignatureFromDeclaration(fn))
	if !c.IsTypeAssignableTo(want, have) {
		return fmt.Sprintf("%s returns %s, which the result of %s, %s, is not", what, c.TypeToString(have), name, c.TypeToString(want))
	}
	return ""
}

// onlyDeclaration returns the declaration of the variable statement s,
// when it declares one name; or nil.
func onlyDeclaration(s *ast.Node) *ast.Node {
	if !ast.IsVariableStatement(s) {
		return nil
	}
	ds := s.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes
	if len(ds) != 1 || !ast.IsIdentifier(ds[0].Name()) {
		return nil
	}
	return ds[0]
}

// slight reports whether e, a body's expression, is small enough to match
// code unrelated to the function: a literal or a name, or one operator or
// index on those. A property read is not: its object's type narrows it.
func slight(e *ast.Node) bool {
	leaf := func(n *ast.Node) bool {
		n = ast.SkipParentheses(n)
		switch n.Kind {
		case ast.KindIdentifier, ast.KindStringLiteral, ast.KindNumericLiteral, ast.KindNoSubstitutionTemplateLiteral, ast.KindTrueKeyword, ast.KindFalseKeyword, ast.KindNullKeyword, ast.KindThisKeyword:
			return true
		}
		return false
	}
	e = ast.SkipParentheses(e)
	switch {
	case leaf(e):
		return true
	case ast.IsBinaryExpression(e):
		return leaf(e.AsBinaryExpression().Left) && leaf(e.AsBinaryExpression().Right)
	case ast.IsElementAccessExpression(e):
		return leaf(e.Expression()) && leaf(e.AsElementAccessExpression().ArgumentExpression)
	case ast.IsPrefixUnaryExpression(e):
		return leaf(e.AsPrefixUnaryExpression().Operand)
	}
	return false
}
