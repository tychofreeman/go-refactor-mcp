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

// SignatureChange is a change of a function's parameters, made alike at
// its declaration and at every call: one of Add, Remove or Move.
type SignatureChange struct {
	Add *NewParameter
	// Remove and Move name a parameter to remove, or to move.
	Remove, Move string
	// Before is the parameter that an added or moved one goes just
	// before; "" for the end.
	Before string
	// Value, with Remove, is what every call passes for the parameter, in
	// the body's terms, which the body reads in its place (WriteValue);
	// Also names functions of the file whose parameter of that name goes
	// in the same step, whose calls pass it on.
	Value string
	Also  []string
}

// NewParameter is a parameter to add, and the value every call passes
// for it.
type NewParameter struct {
	Name, Type, Value string
	// ReplaceVariable lets the parameter hide a variable at the top level
	// of a module that the body reads by its name, taking its place.
	ReplaceVariable bool
}

// ChangeSignature returns the edits that make ch to the function or
// method called name ("f", or "Class.method") in the file at path, at its
// declaration and every call in the project; or why it cannot be made.
func (p *Project) ChangeSignature(ctx context.Context, path, name string, ch SignatureChange) ([]Edit, string) {
	t, done, why := p.target(ctx, path, name)
	if why != "" {
		return nil, why
	}
	defer done()
	l, fn, c, params, off, ctor := t.l, t.fn, t.c, t.params, t.off, t.ctor
	index, isRest := t.index, t.isRest
	n := len(params) - off
	at := n // where the added or moved parameter goes
	if ch.Before != "" {
		if at = index(ch.Before); at < 0 {
			return nil, fmt.Sprintf("%s has no parameter %s", name, ch.Before)
		}
	} else if isRest(n - 1) {
		at = n - 1 // before the rest parameter
	}
	moved := -1
	switch {
	case ch.Add != nil:
		if !scanner.IsIdentifierText(ch.Add.Name, 0) {
			return nil, fmt.Sprintf("%q is not a valid parameter name", ch.Add.Name)
		}
		if p.prog.GetProgram().Options().NoUnusedParameters.IsTrue() && !strings.HasPrefix(ch.Add.Name, "_") {
			return nil, fmt.Sprintf("this project reports unused parameters (noUnusedParameters), so a parameter the body does not read yet does not type-check; name it _%s, which TypeScript does not report, and rename it once the body reads it", ch.Add.Name)
		}
		if index(ch.Add.Name) >= 0 {
			return nil, fmt.Sprintf("%s already has a parameter %s", name, ch.Add.Name)
		}
		if why := t.hides(ch.Add, name); why != "" {
			return nil, why
		}
		for _, m := range t.family {
			if list, moff := passed(m); len(list.Nodes)-moff < at {
				return nil, fmt.Sprintf("%s, which changes with %s, has %d parameters, fewer than the position the new one takes (%d)", memberName(m), name, len(list.Nodes)-moff, at+1)
			}
		}
	case ch.Remove != "":
		i := index(ch.Remove)
		if i < 0 {
			return nil, fmt.Sprintf("%s has no parameter %s", name, ch.Remove)
		}
		if ctor && params[off+i].Modifiers() != nil && len(params[off+i].Modifiers().Nodes) > 0 {
			return nil, fmt.Sprintf("%s is a parameter property, which declares a field of the class too; it cannot be removed as a parameter", ch.Remove)
		}
		prm := c.GetSymbolAtLocation(params[off+i].Name())
		if where := l.reads(c, fn.Body(), ch.Remove, prm); where != "" {
			return nil, fmt.Sprintf("the body of %s reads %s (%s); change the body first so that it no longer does", name, ch.Remove, where)
		}
		if where := l.reads(c, fn.Body(), "arguments", nil); where != "" {
			return nil, fmt.Sprintf("the body of %s reads arguments (%s), whose positions would change", name, where)
		}
		for _, m := range t.family {
			list, moff := passed(m)
			if len(list.Nodes)-moff <= i {
				continue
			}
			ml, prm := newLined(ast.GetSourceFileOfNode(m)), list.Nodes[moff+i]
			pname := ml.source(prm.Name())
			if where := ml.reads(c, m.Body(), pname, c.GetSymbolAtLocation(prm.Name())); where != "" {
				return nil, fmt.Sprintf("the body of %s, which changes with %s, reads %s (%s); change the body first so that it no longer does", memberName(m), name, pname, where)
			}
		}
	case ch.Move != "":
		if moved = index(ch.Move); moved < 0 {
			return nil, fmt.Sprintf("%s has no parameter %s", name, ch.Move)
		}
		if isRest(moved) {
			return nil, fmt.Sprintf("%s is a rest parameter, which stays last", ch.Move)
		}
		if at == moved || at == moved+1 {
			return nil, fmt.Sprintf("%s is already there", ch.Move)
		}
	}

	var value *ast.Node
	if ch.Add != nil {
		if value, why = parseExpression(ch.Add.Value); why != "" {
			return nil, strings.Replace(why, "new is", "value is", 1)
		}
	}

	if why := t.findCalls(p); why != "" {
		return nil, why
	}
	calls := t.calls
	if ch.Remove != "" && ch.Value != "" {
		if why := t.shown(ch, name); why != "" {
			return nil, why
		}
	}
	if value != nil {
		if why := sameMeaning(c, value, calls); why != "" {
			return nil, why
		}
	}

	edits, why := t.familyEdits(ch, at, moved)
	if why != "" {
		return nil, why
	}
	decl := fn.ParameterList()
	switch {
	case ch.Add != nil:
		edits = append(edits, l.insertAt(decl, off+at, ch.Add.Name+": "+strings.TrimSpace(ch.Add.Type)))
		more, why := t.insertInOverloads(at, ch.Add.Name+": "+strings.TrimSpace(ch.Add.Type))
		if why != "" {
			return nil, why
		}
		edits = append(edits, more...)
	case ch.Remove != "":
		edits = append(edits, l.removeAt(decl, off+index(ch.Remove)))
		for _, o := range t.overloads {
			if list, ooff := passed(o); len(list.Nodes)-ooff > index(ch.Remove) {
				edits = append(edits, l.removeAt(list, ooff+index(ch.Remove)))
			}
		}
	default:
		edits = append(edits, l.permute(decl.Nodes[off:], order(n, moved, at))...)
		lo, hi := min(moved, at), max(moved, at-1)
		for _, o := range t.overloads {
			list, ooff := passed(o)
			switch k := len(list.Nodes) - ooff; {
			case k <= lo:
			case k <= hi:
				return nil, fmt.Sprintf("the overload at %s has only some of the parameters the move reorders", l.where(o))
			default:
				edits = append(edits, l.permute(list.Nodes[ooff:ooff+hi+1], order(hi+1, moved, at))...)
			}
		}
	}
	for _, call := range calls {
		cl := newLined(ast.GetSourceFileOfNode(call))
		args := call.ArgumentList()
		m := len(args.Nodes)
		switch {
		case ch.Add != nil:
			text := strings.TrimSpace(ch.Add.Value)
			if m < at {
				text = strings.Repeat("undefined, ", at-m) + text
			}
			edits = append(edits, cl.insertAt(args, min(at, m), text))
		case ch.Remove != "":
			i := index(ch.Remove)
			if i >= m {
				continue
			}
			if e := newEffects(c, cl).Of(args.Nodes[i]); !e.CanDrop() {
				return nil, fmt.Sprintf("removing the argument %s of the call at %s would drop %s", cl.source(args.Nodes[i]), cl.where(call), e.Why)
			}
			edits = append(edits, cl.removeAt(args, i))
		default:
			lo, hi := min(moved, at), max(moved, at-1)
			if m <= lo {
				continue
			}
			var all []Effects
			for _, a := range args.Nodes[lo:min(m, hi+1)] {
				all = append(all, newEffects(c, cl).Of(a))
			}
			if why := canReorder(all); why != "" {
				return nil, fmt.Sprintf("the move would reorder the arguments of the call at %s, and their order matters: %s", cl.where(call), why)
			}
			if m <= hi {
				return nil, fmt.Sprintf("the call at %s passes %d of the arguments the move reorders; pass them all first", cl.where(call), m)
			}
			edits = append(edits, cl.permute(args.Nodes[:hi+1], order(hi+1, moved, at))...)
		}
	}
	return edits, ""
}

// target is a function whose signature is to change: its declaration and,
// once findCalls has run, its calls in the project.
type target struct {
	l         *lined
	fn        *ast.Node   // the implementation
	overloads []*ast.Node // its overload signatures, which change with it
	nameNode  *ast.Node   // the name its callers use
	c         *checker.Checker
	sym       *ast.Symbol
	params    []*ast.Node // with a this parameter, if there is one
	off       int         // 1 when a this parameter, which is not passed, comes first
	ctor      bool
	calls     []*ast.Node
	// family are the other methods that change with it (Project.family),
	// and syms the symbols of it and of them, which its calls name.
	family []*ast.Node
	syms   map[*ast.Symbol]bool
}

// target finds the function or method called name in the file at path;
// done releases the checker it holds.
func (p *Project) target(ctx context.Context, path, name string) (*target, func(), string) {
	l, refusal := p.lined(path)
	if refusal != "" {
		return nil, nil, refusal
	}
	fn, overloads, nameNode, why := l.function(name)
	if why != "" {
		sig := l.interfaceMethod(name)
		if sig == nil {
			return nil, nil, why
		}
		fn, overloads, nameNode = sig, nil, sig.Name()
	}
	c, done := p.prog.GetProgram().GetTypeChecker(ctx)
	sym := c.GetSymbolAtLocation(nameNode)
	if sym == nil {
		done()
		return nil, nil, fmt.Sprintf("%s has no symbol to find its calls by", name)
	}
	family, why := p.family(c, fn)
	if why != "" {
		done()
		return nil, nil, why
	}
	syms := map[*ast.Symbol]bool{sym: true}
	for _, m := range family {
		if s := c.GetSymbolAtLocation(m.Name()); s != nil {
			syms[s] = true
		}
	}
	t := &target{l: l, fn: fn, overloads: overloads, nameNode: nameNode, c: c, sym: sym, params: fn.Parameters(), ctor: ast.IsConstructorDeclaration(fn), family: family, syms: syms}
	if len(t.params) > 0 && ast.IsThisParameter(t.params[0]) {
		t.off = 1
	}
	return t, done, ""
}

// index is the position of the parameter called n among those passed, or
// -1.
func (t *target) index(n string) int {
	for i, prm := range t.params[t.off:] {
		if nm := prm.Name(); nm != nil && ast.IsIdentifier(nm) && nm.Text() == n {
			return i
		}
	}
	return -1
}

// isRest reports whether the parameter at position i is a rest parameter.
func (t *target) isRest(i int) bool {
	return i >= 0 && i < len(t.params)-t.off && t.params[t.off+i].AsParameterDeclaration().DotDotDotToken != nil
}

// declares reports whether id is the name of one of t's overloads, or of
// a method of its family.
func (t *target) declares(id *ast.Node) bool {
	for _, o := range append(t.overloads, t.family...) {
		if o.Name() == id {
			return true
		}
	}
	return false
}

// findCalls finds every call of t in the project, or why one is not known.
func (t *target) findCalls(p *Project) string {
	c := t.c
	why := ""
	for _, f := range p.prog.GetProgram().GetSourceFiles() {
		if f.IsDeclarationFile || strings.Contains(f.FileName(), "/node_modules/") {
			continue
		}
		fl := newLined(f)
		var visit func(nd *ast.Node) bool
		visit = func(nd *ast.Node) bool {
			if why != "" {
				return true
			}
			if ast.IsIdentifier(nd) && nd.Text() == t.sym.Name && nd != t.nameNode && !t.declares(nd) {
				if s := c.GetSymbolAtLocation(nd); s != nil && t.syms[c.SkipAlias(s)] {
					var found []*ast.Node
					var w string
					if t.ctor {
						found, w = fl.constructionsOf(nd)
					} else {
						var call *ast.Node
						if call, w = fl.callOf(nd); call != nil {
							found = []*ast.Node{call}
						}
					}
					if w != "" {
						why = w
					}
					t.calls = append(t.calls, found...)
				}
			}
			return nd.ForEachChild(visit)
		}
		visit(f.AsNode())
		if why != "" {
			return why
		}
	}
	return ""
}

// function finds the function or method called name in the file: a
// function declaration, a function or arrow function bound to a const, or
// a method of a class ("Class.method"). It returns the implementation,
// its overload signatures, and the name its callers use.
func (l *lined) function(name string) (fn *ast.Node, overloads []*ast.Node, nameNode *ast.Node, refusal string) {
	class, member, isMember := strings.Cut(name, ".")
	var found []*ast.Node
	for _, s := range l.f.Statements.Nodes {
		switch {
		case !isMember && ast.IsFunctionDeclaration(s) && s.Name() != nil && s.Name().Text() == name:
			found = append(found, s)
			nameNode = s.Name()
		case !isMember && ast.IsVariableStatement(s):
			for _, d := range s.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes {
				if nm := d.Name(); nm != nil && ast.IsIdentifier(nm) && nm.Text() == name {
					if init := d.Initializer(); init != nil && (ast.IsArrowFunction(init) || ast.IsFunctionExpression(init)) {
						found, nameNode = append(found, init), nm
					}
				}
			}
		case isMember && ast.IsClassDeclaration(s) && s.Name() != nil && s.Name().Text() == class:
			if member == "constructor" {
				for _, m := range s.Members() {
					if ast.IsConstructorDeclaration(m) {
						found, nameNode = append(found, m), s.Name()
					}
				}
				if len(found) == 0 {
					return nil, nil, nil, fmt.Sprintf("%s has no constructor of its own; declare one first", class)
				}
				continue
			}
			for _, m := range s.Members() {
				if ast.IsMethodDeclaration(m) && m.Name() != nil && l.text[l.pos(m.Name()):m.Name().End()] == member {
					found, nameNode = append(found, m), m.Name()
				}
			}
		}
	}
	if len(found) == 0 {
		return nil, nil, nil, fmt.Sprintf("the file declares no function or method %s (give name, or Class.method)", name)
	}
	for _, f := range found {
		switch {
		case f.Body() == nil:
			overloads = append(overloads, f)
		case fn != nil:
			return nil, nil, nil, fmt.Sprintf("%s has two bodies", name)
		default:
			fn = f
		}
	}
	if fn == nil {
		return nil, nil, nil, fmt.Sprintf("%s has no body", name)
	}
	if !ast.IsConstructorDeclaration(fn) && !ast.IsArrowFunction(fn) && !ast.IsFunctionExpression(fn) {
		nameNode = fn.Name()
	}
	return fn, overloads, nameNode, ""
}

// interfaceMethod returns the method signature that name ("Iface.method")
// names in an interface at the top level of the file, or nil.
func (l *lined) interfaceMethod(name string) *ast.Node {
	owner, member, ok := strings.Cut(name, ".")
	if !ok {
		return nil
	}
	for _, s := range l.f.Statements.Nodes {
		if !ast.IsInterfaceDeclaration(s) || s.Name().Text() != owner {
			continue
		}
		for _, m := range s.Members() {
			if ast.IsMethodSignatureDeclaration(m) && m.Name() != nil && l.source(m.Name()) == member {
				return m
			}
		}
	}
	return nil
}

// family returns the other methods whose signature must change with the
// method fn, a member of a class or interface: the same method of the
// types it implements or extends, through types that do not declare it
// too, and of every type of the program that implements or extends one
// of those or fn's own type and declares it; or why one cannot change
// with it.
func (p *Project) family(c *checker.Checker, fn *ast.Node) ([]*ast.Node, string) {
	if !ast.IsMethodDeclaration(fn) && !ast.IsMethodSignatureDeclaration(fn) || fn.Name() == nil {
		return nil, ""
	}
	name := fn.Name().Text()
	own := fn.Parent
	var types []*ast.Node
	bases := map[*ast.Node][]*ast.Node{}
	for _, f := range p.prog.GetProgram().GetSourceFiles() {
		if f.IsDeclarationFile || strings.Contains(f.FileName(), "/node_modules/") {
			continue
		}
		inspect(f.AsNode(), func(n *ast.Node) bool {
			if ast.IsClassDeclaration(n) || ast.IsClassExpression(n) || ast.IsInterfaceDeclaration(n) {
				types = append(types, n)
				for _, e := range append(ast.GetExtendsHeritageClauseElements(n), ast.GetImplementsHeritageClauseElements(n)...) {
					sym := c.GetSymbolAtLocation(e.Expression())
					if sym == nil {
						continue
					}
					for _, d := range c.SkipAlias(sym).Declarations {
						if ast.IsClassDeclaration(d) || ast.IsClassExpression(d) || ast.IsInterfaceDeclaration(d) {
							bases[n] = append(bases[n], d)
						}
					}
				}
			}
			return true
		})
	}
	ancestors := func(t *ast.Node) map[*ast.Node]bool {
		seen := map[*ast.Node]bool{}
		var up func(t *ast.Node)
		up = func(t *ast.Node) {
			for _, b := range bases[t] {
				if !seen[b] {
					seen[b] = true
					up(b)
				}
			}
		}
		up(t)
		return seen
	}
	declared := func(t *ast.Node) []*ast.Node {
		var out []*ast.Node
		for _, m := range t.Members() {
			if (ast.IsMethodDeclaration(m) || ast.IsMethodSignatureDeclaration(m)) && m.Name() != nil && ast.IsIdentifier(m.Name()) && m.Name().Text() == name {
				out = append(out, m)
			}
		}
		return out
	}
	roots := map[*ast.Node]bool{own: true}
	for a := range ancestors(own) {
		if len(declared(a)) > 0 {
			roots[a] = true
		}
	}
	var family []*ast.Node
	for _, t := range types {
		in := roots[t]
		for a := range ancestors(t) {
			in = in || roots[a]
		}
		if !in || t == own {
			continue
		}
		ms := declared(t)
		switch {
		case len(ms) > 1:
			return nil, fmt.Sprintf("%s.%s, which changes with it, has overloads (%s); change it on its own first", typeName(t), name, newLined(ast.GetSourceFileOfNode(t)).where(ms[0]))
		case len(ms) == 1:
			family = append(family, ms[0])
		}
	}
	return family, ""
}

// typeName is the name of a class or interface, or "the class" for one
// without a name.
func typeName(t *ast.Node) string {
	if t.Name() != nil {
		return t.Name().Text()
	}
	return "the class"
}

// memberName is a method's name with its type's: "Hose.pour".
func memberName(m *ast.Node) string {
	return typeName(m.Parent) + "." + m.Name().Text()
}

// passed returns the parameter list of the signature o and how many of its
// parameters are not passed (1 for a this parameter).
func passed(o *ast.Node) (*ast.NodeList, int) {
	list := o.ParameterList()
	if len(list.Nodes) > 0 && ast.IsThisParameter(list.Nodes[0]) {
		return list, 1
	}
	return list, 0
}

// insertInOverloads returns the edits that insert text as the parameter at
// position at of every overload of t; or why an overload has too few
// parameters to take it there, where calls matching it would pass it.
func (t *target) insertInOverloads(at int, text string) ([]Edit, string) {
	var edits []Edit
	for _, o := range t.overloads {
		list, off := passed(o)
		if k := len(list.Nodes) - off; k < at {
			return nil, fmt.Sprintf("the overload at %s has %d parameters, fewer than the position the new one takes (%d): calls matching it would not pass it there", t.l.where(o), k, at+1)
		}
		edits = append(edits, t.l.insertAt(list, off+at, text))
	}
	return edits, ""
}

// familyEdits returns the edits that make ch to the parameters of each
// method of t's family: an added one inserted at position at, a removed
// one (at index, by t's names) deleted, or the one at moved put at at; or
// why a method has too few parameters for the move.
func (t *target) familyEdits(ch SignatureChange, at, moved int) ([]Edit, string) {
	var edits []Edit
	for _, m := range t.family {
		ml := newLined(ast.GetSourceFileOfNode(m))
		list, moff := passed(m)
		k := len(list.Nodes) - moff
		switch {
		case ch.Add != nil:
			edits = append(edits, ml.insertAt(list, moff+at, ch.Add.Name+": "+strings.TrimSpace(ch.Add.Type)))
		case ch.Remove != "":
			if i := t.index(ch.Remove); i < k {
				edits = append(edits, ml.removeAt(list, moff+i))
			}
		default:
			lo, hi := min(moved, at), max(moved, at-1)
			switch {
			case k <= lo:
			case k <= hi:
				return nil, fmt.Sprintf("%s, which changes with it, has only some of the parameters the move reorders", memberName(m))
			default:
				edits = append(edits, ml.permute(list.Nodes[moff:moff+hi+1], order(hi+1, moved, at))...)
			}
		}
	}
	return edits, ""
}

// reads returns where body reads name: the symbol sym if it is given,
// else any symbol so named; "" if it does not.
func (l *lined) reads(c *checker.Checker, body *ast.Node, name string, sym *ast.Symbol) string {
	where := ""
	var visit func(n *ast.Node) bool
	visit = func(n *ast.Node) bool {
		if where != "" {
			return true
		}
		if ast.IsIdentifier(n) && n.Text() == name && !(ast.IsPropertyAccessExpression(n.Parent) && n.Parent.Name() == n) {
			if s := c.GetSymbolAtLocation(n); s != nil && (sym == nil || s == sym) || name == "arguments" {
				where = l.where(n)
			}
		}
		return n.ForEachChild(visit)
	}
	if body != nil {
		visit(body)
	}
	return where
}

// callOf returns the call whose callee is the reference id, or nil for
// an import or export of it; refusal for any other use, as a value.
func (l *lined) callOf(id *ast.Node) (*ast.Node, string) {
	switch {
	case ast.IsImportSpecifier(id.Parent), ast.IsExportSpecifier(id.Parent), ast.IsImportClause(id.Parent):
		return nil, ""
	}
	callee := id
	if ast.IsPropertyAccessExpression(id.Parent) && id.Parent.Name() == id {
		callee = id.Parent
	}
	if call := callee.Parent; call != nil && ast.IsCallExpression(call) && call.Expression() == callee {
		for _, a := range call.Arguments() {
			if ast.IsSpreadElement(a) {
				return nil, fmt.Sprintf("the call at %s spreads its arguments (%s), so where each goes is not known", l.where(call), l.source(a))
			}
		}
		return call, ""
	}
	return nil, fmt.Sprintf("%s is used as a value at %s, where what calls it passes is not known", id.Text(), l.where(id))
}

// constructionsOf returns the calls of the constructor of the class that
// the reference id names: a new of it, or the super calls of a class
// extending it; none for a use as a type, a static member, instanceof,
// implements, or an import. It refuses any other use, which could
// construct the class with arguments that are not known.
func (l *lined) constructionsOf(id *ast.Node) ([]*ast.Node, string) {
	p := id.Parent
	spread := func(call *ast.Node) string {
		for _, a := range call.Arguments() {
			if ast.IsSpreadElement(a) {
				return fmt.Sprintf("the call at %s spreads its arguments (%s), so where each goes is not known", l.where(call), l.source(a))
			}
		}
		return ""
	}
	switch {
	case ast.IsImportSpecifier(p), ast.IsExportSpecifier(p), ast.IsImportClause(p), ast.IsTypeReferenceNode(p), ast.IsTypeQueryNode(p):
		return nil, ""
	case ast.IsPropertyAccessExpression(p) && p.Expression() == id:
		return nil, ""
	case ast.IsBinaryExpression(p) && p.AsBinaryExpression().OperatorToken.Kind == ast.KindInstanceOfKeyword && p.AsBinaryExpression().Right == id:
		return nil, ""
	case ast.IsNewExpression(p) && p.Expression() == id:
		if p.ArgumentList() == nil {
			return nil, fmt.Sprintf("new %s at %s has no parentheses to pass arguments in", id.Text(), l.where(p))
		}
		if why := spread(p); why != "" {
			return nil, why
		}
		return []*ast.Node{p}, ""
	case ast.IsExpressionWithTypeArguments(p) && ast.IsHeritageClause(p.Parent):
		if p.Parent.AsHeritageClause().Token != ast.KindExtendsKeyword {
			return nil, ""
		}
		derived := p.Parent.Parent
		name := "a class"
		if derived.Name() != nil {
			name = derived.Name().Text()
		}
		var ctor *ast.Node
		for _, m := range derived.Members() {
			if ast.IsConstructorDeclaration(m) && m.Body() != nil {
				ctor = m
			}
		}
		if ctor == nil {
			return nil, fmt.Sprintf("%s extends %s without a constructor of its own, so its news pass their arguments straight to %s's; declare one first", name, id.Text(), id.Text())
		}
		var calls []*ast.Node
		why := ""
		var visit func(n *ast.Node) bool
		visit = func(n *ast.Node) bool {
			if ast.IsClassDeclaration(n) || ast.IsClassExpression(n) {
				return false
			}
			if ast.IsCallExpression(n) && n.Expression().Kind == ast.KindSuperKeyword {
				if w := spread(n); w != "" && why == "" {
					why = w
				}
				calls = append(calls, n)
			}
			n.ForEachChild(visit)
			return false
		}
		ctor.Body().ForEachChild(visit)
		return calls, why
	}
	return nil, fmt.Sprintf("%s is used as a value at %s, where it could be constructed with arguments that are not known", id.Text(), l.where(id))
}

// sameMeaning refuses a value whose names do not mean the same at every
// call, or that reads this.
func sameMeaning(c *checker.Checker, value *ast.Node, calls []*ast.Node) string {
	why := ""
	var visit func(n *ast.Node) bool
	visit = func(n *ast.Node) bool {
		if why != "" {
			return true
		}
		if n.Kind == ast.KindThisKeyword {
			why = "value reads this, which is not the same at every call"
			return true
		}
		if ast.IsIdentifier(n) && !(n.Parent != nil && (ast.IsPropertyAccessExpression(n.Parent) && n.Parent.Name() == n || ast.IsPropertyAssignment(n.Parent) && n.Parent.Name() == n)) {
			var first *ast.Symbol
			var firstCall *ast.Node
			for _, call := range calls {
				s := c.ResolveName(n.Text(), call, ast.SymbolFlagsValue, false)
				cl := newLined(ast.GetSourceFileOfNode(call))
				if s == nil {
					why = fmt.Sprintf("value names %s, which the call at %s cannot see", n.Text(), cl.where(call))
					return true
				}
				s = c.SkipAlias(s)
				if first == nil {
					first, firstCall = s, call
				} else if s != first {
					why = fmt.Sprintf("value's %s means one thing at %s and another at %s; pass it at each call yourself", n.Text(), newLined(ast.GetSourceFileOfNode(firstCall)).where(firstCall), cl.where(call))
					return true
				}
			}
		}
		return n.ForEachChild(visit)
	}
	visit(value)
	return why
}

// simple reports whether evaluating e plainly has no effects: a name, a
// literal, this, a function, or a property, array or object of such.
func simple(e *ast.Node) bool {
	switch {
	case ast.IsIdentifier(e), ast.IsLiteralExpression(e), ast.IsArrowFunction(e), ast.IsFunctionExpression(e):
		return true
	case ast.IsPropertyAccessExpression(e), ast.IsParenthesizedExpression(e):
		return simple(e.Expression())
	case ast.IsArrayLiteralExpression(e):
		return !slices.ContainsFunc(e.AsArrayLiteralExpression().Elements.Nodes, func(x *ast.Node) bool { return !simple(x) })
	case ast.IsObjectLiteralExpression(e):
		for _, p := range e.AsObjectLiteralExpression().Properties.Nodes {
			if ast.IsShorthandPropertyAssignment(p) {
				continue
			}
			if !ast.IsPropertyAssignment(p) || !simple(p.Initializer()) {
				return false
			}
		}
		return true
	}
	switch e.Kind {
	case ast.KindThisKeyword, ast.KindTrueKeyword, ast.KindFalseKeyword, ast.KindNullKeyword:
		return true
	}
	return false
}

// order is the positions 0..n-1 after the one at from moves to just
// before the one at to (n for the end): order[i] is where position i's
// element came from.
func order(n, from, to int) []int {
	var rest []int
	for i := range n {
		if i != from {
			rest = append(rest, i)
		}
	}
	ins := len(rest)
	if to < n {
		ins = slices.Index(rest, to)
	}
	return slices.Insert(rest, ins, from)
}

// insertAt returns the edit that inserts text as element i of list.
func (l *lined) insertAt(list *ast.NodeList, i int, text string) Edit {
	nodes := list.Nodes
	switch {
	case len(nodes) == 0:
		return Edit{File: l.f.FileName(), Start: list.Pos(), End: list.Pos(), NewText: text}
	case i < len(nodes):
		at := l.pos(nodes[i])
		return Edit{File: l.f.FileName(), Start: at, End: at, NewText: text + ", "}
	}
	at := nodes[len(nodes)-1].End()
	return Edit{File: l.f.FileName(), Start: at, End: at, NewText: ", " + text}
}

// removeAt returns the edit that removes element i of list and a comma
// beside it.
func (l *lined) removeAt(list *ast.NodeList, i int) Edit {
	nodes := list.Nodes
	switch {
	case i+1 < len(nodes):
		return Edit{File: l.f.FileName(), Start: l.pos(nodes[i]), End: l.pos(nodes[i+1])}
	case i > 0:
		return Edit{File: l.f.FileName(), Start: nodes[i-1].End(), End: nodes[i].End()}
	}
	return Edit{File: l.f.FileName(), Start: l.pos(nodes[i]), End: nodes[i].End()}
}

// permute returns the edits that put at each position i of nodes the
// text of nodes[from[i]], keeping what is between them.
func (l *lined) permute(nodes []*ast.Node, from []int) []Edit {
	var out []Edit
	for i, j := range from {
		if i != j {
			out = append(out, Edit{File: l.f.FileName(), Start: l.pos(nodes[i]), End: nodes[i].End(), NewText: l.source(nodes[j])})
		}
	}
	return out
}

func (l *lined) source(n *ast.Node) string { return l.text[l.pos(n):n.End()] }

// where is file:line of n, the file by its base name.
func (l *lined) where(n *ast.Node) string {
	name := l.f.FileName()
	return fmt.Sprintf("%s:%d", name[strings.LastIndex(name, "/")+1:], l.line(l.pos(n)))
}
