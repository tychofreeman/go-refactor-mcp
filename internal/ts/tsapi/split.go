package tsapi

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
)

// SplitVariable returns the edits of split's kind variable: the assignment
// to name on line, in the block that declares name and not reading it,
// becomes the declaration of newName, a const (let when it is assigned
// again), and the uses of name after it read newName.
func (p *Project) SplitVariable(ctx context.Context, path string, line int, name, newName string) ([]Edit, string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, why
	}
	if line < 1 || line > l.count() {
		return nil, fmt.Sprintf("line %d out of range: the file has %d lines", line, l.count())
	}
	var stmt, as *ast.Node
	l.lists(func(_ *ast.Node, list []*ast.Node) {
		for _, s := range list {
			if stmt != nil || l.line(l.pos(s)) != line || !ast.IsExpressionStatement(s) {
				continue
			}
			e := s.Expression()
			if ast.IsBinaryExpression(e) && e.AsBinaryExpression().OperatorToken.Kind == ast.KindEqualsToken {
				if left := e.AsBinaryExpression().Left; ast.IsIdentifier(left) && left.Text() == name {
					stmt, as = s, e
				}
			}
		}
	})
	if stmt == nil {
		return nil, fmt.Sprintf("no assignment %s = ... starts on line %d: %s", name, line, strings.TrimSpace(l.text[l.start(line):l.end(line)]))
	}
	c, done := p.prog.GetProgram().GetTypeCheckerForFile(ctx, l.f)
	defer done()
	b := as.AsBinaryExpression()
	sym := symbolOf(c, b.Left)
	if sym == nil || sym.ValueDeclaration == nil || !ast.IsVariableDeclaration(sym.ValueDeclaration) || !ast.IsVariableDeclarationList(sym.ValueDeclaration.Parent) {
		return nil, fmt.Sprintf("%s is not a variable declared with let or var", name)
	}
	decl := sym.ValueDeclaration
	if ast.GetSourceFileOfNode(decl) != l.f || decl.Parent.Parent == nil || decl.Parent.Parent.Parent != stmt.Parent {
		return nil, fmt.Sprintf("%s is in a block inside the one that declares %s (%s), where the variable may hold its old value after it; split it there", l.part(stmt), name, l.where(decl))
	}
	if u := uses(c, b.Right, []*ast.Symbol{sym}); u != nil {
		return nil, fmt.Sprintf("the new value %s reads %s, so it is the same variable", l.source(b.Right), name)
	}
	fn := ast.FindAncestor(decl, ast.IsFunctionLike)
	var later []*ast.Node
	assigned := false
	var closure *ast.Node
	identifiers(l.f.AsNode(), func(id *ast.Node) {
		if id == b.Left || ast.IsDeclarationName(id) || symbolOf(c, id) != sym {
			return
		}
		if closure == nil && ast.FindAncestor(id, ast.IsFunctionLike) != fn {
			closure = id
		}
		if id.Pos() >= stmt.End() {
			later = append(later, id)
			assigned = assigned || isAssigned(id)
		}
	})
	if closure != nil {
		return nil, fmt.Sprintf("a closure refers to %s at %s, and may run after the assignment or before it", name, l.where(closure))
	}
	if why := taken(c, l, newName, stmt); why != "" {
		return nil, why
	}
	keyword := "const "
	if assigned {
		keyword = "let "
	}
	declared := keyword + newName
	if t := decl.Type(); t != nil {
		declared += ": " + l.source(t)
	}
	edits := []Edit{{File: l.f.FileName(), Start: l.pos(b.Left), End: b.Left.End(), NewText: declared}}
	for _, id := range later {
		text := newName
		if ast.IsShorthandPropertyAssignment(id.Parent) && id.Parent.Name() == id {
			text = name + ": " + newName
		}
		edits = append(edits, Edit{File: l.f.FileName(), Start: l.pos(id), End: id.End(), NewText: text})
	}
	return edits, ""
}

// isAssigned reports whether the identifier id is assigned where it is:
// on the left of an assignment, or by ++ or --.
func isAssigned(id *ast.Node) bool {
	p := id.Parent
	switch {
	case ast.IsBinaryExpression(p):
		return ast.IsAssignmentOperator(p.AsBinaryExpression().OperatorToken.Kind) && p.AsBinaryExpression().Left == id
	case ast.IsPrefixUnaryExpression(p):
		return isUpdate(p.AsPrefixUnaryExpression().Operator)
	case ast.IsPostfixUnaryExpression(p):
		return true
	}
	return false
}

// SplitLoop returns the edits of split's kind loop: the statements of the
// body of the for loop on line, from the one on line at, go to a second
// loop with the same header, after the first.
func (p *Project) SplitLoop(ctx context.Context, path string, line, at int) ([]Edit, string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, why
	}
	if line < 1 || line > l.count() {
		return nil, fmt.Sprintf("line %d out of range: the file has %d lines", line, l.count())
	}
	var loop *ast.Node
	inspect(l.f.AsNode(), func(n *ast.Node) bool {
		if loop == nil && (ast.IsForStatement(n) || ast.IsForInOrOfStatement(n)) && l.line(l.pos(n)) == line {
			loop = n
		}
		return loop == nil
	})
	if loop == nil {
		return nil, fmt.Sprintf("no for loop starts on line %d: %s", line, strings.TrimSpace(l.text[l.start(line):l.end(line)]))
	}
	body := loop.Statement()
	if !ast.IsBlock(body) {
		return nil, fmt.Sprintf("the body of %s is not a block", l.part(loop))
	}
	list := body.Statements()
	i := slices.IndexFunc(list, func(s *ast.Node) bool { return l.line(l.pos(s)) == at })
	switch {
	case i < 0:
		return nil, fmt.Sprintf("no statement of the body of %s starts on line %d", l.part(loop), at)
	case i == 0:
		return nil, fmt.Sprintf("%s is the first statement of the body; at is where the second loop's statements start", l.part(list[0]))
	}
	first, second := list[:i], list[i:]
	c, done := p.prog.GetProgram().GetTypeCheckerForFile(ctx, l.f)
	defer done()
	a := newEffects(c, l)
	// The parts may not use what the other declares.
	for _, pair := range [][2][]*ast.Node{{first, second}, {second, first}} {
		for _, d := range pair[0] {
			if ast.IsFunctionDeclaration(d) {
				continue
			}
			for _, u := range pair[1] {
				if id := uses(c, u, symbols(c, declared(d))); id != nil {
					return nil, fmt.Sprintf("%s uses %s, which %s declares", l.part(u), id.Text(), l.part(d))
				}
			}
		}
	}
	// The header runs again for the second loop: it must do the same, and
	// the body must not change what it reads.
	var header Effects
	var own []*ast.Symbol
	var init *ast.Node
	if ast.IsForStatement(loop) {
		f := loop.AsForStatement()
		init = f.Initializer
		if ast.IsVariableDeclarationList(f.Initializer) {
			header.add((&walk{a: a, labels: map[string]bool{}}).declare(f.Initializer, false))
		} else {
			header.add(a.Of(f.Initializer))
		}
		header.add(a.Of(f.Condition))
		header.add(a.Of(f.Incrementor))
	} else {
		f := loop.AsForInOrOfStatement()
		init = f.Initializer
		header.add(a.Of(f.Expression))
		if ast.IsForOfStatement(loop) && !c.IsArrayType(c.GetTypeAtLocation(f.Expression)) {
			return nil, fmt.Sprintf("%s loops over %s, which is not known to be an array: a second loop over an iterator would see nothing", l.part(loop), l.source(f.Expression))
		}
	}
	if init != nil && ast.IsVariableDeclarationList(init) {
		for _, d := range init.AsVariableDeclarationList().Declarations.Nodes {
			var names []*ast.Node
			identifiers(d.Name(), func(id *ast.Node) { names = append(names, id) })
			own = append(own, symbols(c, names)...)
		}
	} else if init != nil {
		return nil, fmt.Sprintf("the header of %s assigns variables declared outside it, which a second loop would assign again", l.part(loop))
	}
	for _, s := range own {
		delete(header.Assigns, s)
		delete(header.Locals, s)
	}
	if header.Writes || len(header.Assigns) > 0 || header.Leaves {
		return nil, fmt.Sprintf("the header of %s may change state (%s), and a second loop would do it again", l.part(loop), header.At.Write)
	}
	var parts [2]Effects
	for k, part := range [][]*ast.Node{first, second} {
		parts[k] = a.ofNodes(part)
		if parts[k].Leaves {
			return nil, fmt.Sprintf("%s leaves the loop, which the other loop would not", parts[k].At.Leave)
		}
		for _, s := range own {
			if w, ok := parts[k].Assigns[s]; ok {
				return nil, fmt.Sprintf("%s assigns %s, which the header declares", w, s.Name)
			}
			delete(parts[k].Locals, s)
		}
		if why := conflict(header, parts[k]); why != "" {
			return nil, why
		}
	}
	if why := conflict(parts[0], parts[1]); why != "" {
		return nil, why
	}
	start, _, ok := l.lineRange(second[0])
	if !ok {
		return nil, fmt.Sprintf("%s shares its line with other code", l.part(second[0]))
	}
	if _, _, ok := l.lineRange(first[len(first)-1]); !ok {
		return nil, fmt.Sprintf("%s shares its line with other code", l.part(first[len(first)-1]))
	}
	indent := l.indent(line)
	text := indent + "}\n" + indent + l.text[l.pos(loop):l.pos(body)] + "{\n"
	return []Edit{{File: l.f.FileName(), Start: start, End: start, NewText: text}}, ""
}

// PhaseAt finds the second phase of a function that starts with the
// statement on line of the file at path: the last line of the function's
// body, and the names the statements before it declare that the ones from
// it on use; or why there is none: not a statement of a function's body,
// its first, a second phase that uses nothing the first computes or
// assigns some of it, or typeName taken.
func (p *Project) PhaseAt(ctx context.Context, path string, line int, typeName string) (last int, names []string, refusal string) {
	l, why := p.lined(path)
	if why != "" {
		return 0, nil, why
	}
	var stmt *ast.Node
	var list []*ast.Node
	l.lists(func(_ *ast.Node, ls []*ast.Node) {
		for _, s := range ls {
			if stmt == nil && l.line(l.pos(s)) == line {
				stmt, list = s, ls
			}
		}
	})
	if stmt == nil {
		return 0, nil, fmt.Sprintf("no statement starts on line %d", line)
	}
	body := stmt.Parent
	if !ast.IsBlock(body) || body.Parent == nil || !ast.IsFunctionLike(body.Parent) || body.Parent.Body() != body {
		return 0, nil, fmt.Sprintf("line %d is not a statement of the function's body; the second phase starts at one", line)
	}
	i := slices.Index(list, stmt)
	if i == 0 {
		return 0, nil, fmt.Sprintf("line %d is the function's first statement, so there would be no first phase", line)
	}
	c, done := p.prog.GetProgram().GetTypeCheckerForFile(ctx, l.f)
	defer done()
	if taken(c, l, typeName, stmt) != "" {
		return 0, nil, fmt.Sprintf("%s is already declared; choose another type name", typeName)
	}
	for _, s := range list[:i] {
		for _, d := range declared(s) {
			sym := c.GetSymbolAtLocation(d)
			if sym == nil {
				continue
			}
			used := false
			for _, later := range list[i:] {
				if where := l.assigns(later, sym, c); where != "" {
					return 0, nil, fmt.Sprintf("%s assigns %s, which the first phase computes; split it first, with kind variable", where, d.Text())
				}
				used = used || uses(c, later, []*ast.Symbol{sym}) != nil
			}
			if used {
				names = append(names, d.Text())
			}
		}
	}
	if len(names) == 0 {
		return 0, nil, fmt.Sprintf("the statements from line %d on use nothing the ones before compute, so there is nothing to pass between phases", line)
	}
	return l.line(list[len(list)-1].End() - 1), names, ""
}
