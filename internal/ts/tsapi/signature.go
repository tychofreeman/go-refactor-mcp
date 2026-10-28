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
}

// NewParameter is a parameter to add, and the value every call passes
// for it.
type NewParameter struct {
	Name, Type, Value string
}

// ChangeSignature returns the edits that make ch to the function or
// method called name ("f", or "Class.method") in the file at path, at its
// declaration and every call in the project; or why it cannot be made.
func (p *Project) ChangeSignature(ctx context.Context, path, name string, ch SignatureChange) ([]Edit, string) {
	l, refusal := p.lined(path)
	if refusal != "" {
		return nil, refusal
	}
	fn, nameNode, why := l.function(name)
	if why != "" {
		return nil, why
	}
	c, done := p.prog.GetProgram().GetTypeChecker(ctx)
	defer done()
	sym := c.GetSymbolAtLocation(nameNode)
	if sym == nil {
		return nil, fmt.Sprintf("%s has no symbol to find its calls by", name)
	}
	params := fn.Parameters()
	off := 0 // a this parameter is not passed
	if len(params) > 0 && ast.IsThisParameter(params[0]) {
		off = 1
	}
	index := func(n string) int {
		for i, prm := range params[off:] {
			if nm := prm.Name(); nm != nil && ast.IsIdentifier(nm) && nm.Text() == n {
				return i
			}
		}
		return -1
	}
	isRest := func(i int) bool {
		return i >= 0 && i < len(params)-off && params[off+i].AsParameterDeclaration().DotDotDotToken != nil
	}
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
		if other := l.reads(c, fn.Body(), ch.Add.Name, nil); other != "" {
			return nil, fmt.Sprintf("the body of %s reads another %s (%s), which the parameter would hide; choose another name", name, ch.Add.Name, other)
		}
	case ch.Remove != "":
		i := index(ch.Remove)
		if i < 0 {
			return nil, fmt.Sprintf("%s has no parameter %s", name, ch.Remove)
		}
		prm := c.GetSymbolAtLocation(params[off+i].Name())
		if where := l.reads(c, fn.Body(), ch.Remove, prm); where != "" {
			return nil, fmt.Sprintf("the body of %s reads %s (%s); change the body first so that it no longer does", name, ch.Remove, where)
		}
		if where := l.reads(c, fn.Body(), "arguments", nil); where != "" {
			return nil, fmt.Sprintf("the body of %s reads arguments (%s), whose positions would change", name, where)
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

	var calls []*ast.Node
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
			if ast.IsIdentifier(nd) && nd.Text() == sym.Name && nd != nameNode {
				if s := c.GetSymbolAtLocation(nd); s != nil && c.SkipAlias(s) == sym {
					call, w := fl.callOf(nd)
					switch {
					case w != "":
						why = w
					case call != nil:
						calls = append(calls, call)
					}
				}
			}
			return nd.ForEachChild(visit)
		}
		visit(f.AsNode())
		if why != "" {
			return nil, why
		}
	}
	if value != nil {
		if why := sameMeaning(c, value, calls); why != "" {
			return nil, why
		}
	}

	var edits []Edit
	decl := fn.ParameterList()
	switch {
	case ch.Add != nil:
		edits = append(edits, l.insertAt(decl, off+at, ch.Add.Name+": "+strings.TrimSpace(ch.Add.Type)))
	case ch.Remove != "":
		edits = append(edits, l.removeAt(decl, off+index(ch.Remove)))
	default:
		edits = append(edits, l.permute(decl.Nodes[off:], order(n, moved, at))...)
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
			if !simple(args.Nodes[i]) {
				return nil, fmt.Sprintf("the argument %s of the call at %s could have effects, which removing it would drop", cl.source(args.Nodes[i]), cl.where(call))
			}
			edits = append(edits, cl.removeAt(args, i))
		default:
			lo, hi := min(moved, at), max(moved, at-1)
			if m <= lo {
				continue
			}
			for _, a := range args.Nodes[lo:min(m, hi+1)] {
				if !simple(a) {
					return nil, fmt.Sprintf("the argument %s of the call at %s could have effects, so its order matters", cl.source(a), cl.where(call))
				}
			}
			if m <= hi {
				return nil, fmt.Sprintf("the call at %s passes %d of the arguments the move reorders; pass them all first", cl.where(call), m)
			}
			edits = append(edits, cl.permute(args.Nodes[:hi+1], order(hi+1, moved, at))...)
		}
	}
	return edits, ""
}

// function finds the function or method called name in the file: a
// function declaration, a function or arrow function bound to a const, or
// a method of a class ("Class.method"). It returns the function and the
// name its callers use.
func (l *lined) function(name string) (fn, nameNode *ast.Node, refusal string) {
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
				return nil, nil, "changing a constructor's parameters is not supported yet"
			}
			for _, m := range s.Members() {
				if ast.IsMethodDeclaration(m) && m.Name() != nil && l.text[l.pos(m.Name()):m.Name().End()] == member {
					found, nameNode = append(found, m), m.Name()
				}
			}
		}
	}
	switch {
	case len(found) == 0:
		return nil, nil, fmt.Sprintf("the file declares no function or method %s (give name, or Class.method)", name)
	case len(found) > 1:
		return nil, nil, fmt.Sprintf("%s has overloads; changing them together is not supported yet", name)
	case found[0].Body() == nil:
		return nil, nil, fmt.Sprintf("%s has no body", name)
	}
	return found[0], nameNode, ""
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
