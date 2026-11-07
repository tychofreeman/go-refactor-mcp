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

// ExtractType returns the edits that move the members fields of the class,
// interface or type alias name into a new type newType, held in the
// property as in their place: every x.f of a moved member becomes x.as.f.
// From a class, newType is a class whose instance the property holds (as =
// new NewType()), the inverse of InlineClass: its properties need
// initializers that do not read this, and its methods may use only the
// moved members. From an interface or a type alias of an object type,
// newType is an interface, and every object literal of the type is
// restructured: { id, street, city } becomes { id, as: { street, city } }.
func (p *Project) ExtractType(ctx context.Context, path string, line int, name string, column int, fields []string, newType, as string) ([]Edit, string) {
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
	if why := taken(c, l, newType, decl); why != "" {
		return nil, why
	}
	if !scanner.IsIdentifierText(as, 0) {
		return nil, fmt.Sprintf("give as, the name of the property that holds the %s: %q is not a valid name", newType, as)
	}
	if decl.TypeParameters() != nil {
		return nil, fmt.Sprintf("%s is generic", name)
	}
	class := ast.IsClassDeclaration(decl)
	var members []*ast.Node
	switch {
	case class, ast.IsInterfaceDeclaration(decl):
		members = decl.Members()
	case ast.IsTypeAliasDeclaration(decl) && ast.IsTypeLiteralNode(decl.Type()):
		members = decl.Type().Members()
	default:
		return nil, fmt.Sprintf("%s is a type alias of something other than an object type", name)
	}
	if len(fields) == 0 {
		return nil, "give fields: the members to move"
	}
	var chosen []*ast.Node
	moved := map[string]bool{}
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
		moved[f] = true
	}
	for _, m := range members {
		if m.Name() != nil && l.source(m.Name()) == as {
			return nil, fmt.Sprintf("%s already has a member %s; choose another as", name, as)
		}
	}
	slices.SortFunc(chosen, func(a, b *ast.Node) int { return a.Pos() - b.Pos() })
	for _, m := range chosen {
		if why := l.movable(m, class, name, newType, moved); why != "" {
			return nil, why
		}
	}
	declSym := c.GetSymbolAtLocation(decl.Name())
	for _, id := range p.references(c, []*ast.Symbol{declSym}) {
		if h := id.Parent; h != nil && ast.IsExpressionWithTypeArguments(h) && h.Parent != nil && ast.IsHeritageClause(h.Parent) {
			return nil, fmt.Sprintf("%s is extended or implemented at %s, which would have to change with it", name, newLined(ast.GetSourceFileOfNode(id)).where(id))
		}
	}
	if why := p.byName(c, declSym, name, newType); why != "" {
		return nil, why
	}
	var syms []*ast.Symbol
	for _, m := range chosen {
		syms = append(syms, c.GetSymbolAtLocation(m.Name()))
	}
	inMoved := func(n *ast.Node) bool {
		if ast.GetSourceFileOfNode(n) != l.f {
			return false
		}
		return slices.ContainsFunc(chosen, func(m *ast.Node) bool { return n.Pos() >= m.Pos() && n.End() <= m.End() })
	}
	var edits []Edit
	for _, id := range p.references(c, syms) {
		fl := newLined(ast.GetSourceFileOfNode(id))
		pa := id.Parent
		access := ast.IsPropertyAccessExpression(pa) && pa.Name() == id
		switch {
		case inMoved(id) && access && pa.Expression().Kind == ast.KindThisKeyword:
			// Moves with its member, where this is the new type.
		case inMoved(id):
			return nil, fmt.Sprintf("%s is read from another value at %s, in a member that moves", id.Text(), fl.where(id))
		case access:
			edits = append(edits, Edit{File: fl.f.FileName(), Start: fl.pos(id), End: fl.pos(id), NewText: as + "."})
		default:
			return nil, fmt.Sprintf("%s is used at %s other than as x.%s", id.Text(), fl.where(id), id.Text())
		}
	}
	lits, why := p.literals(c, declSym, name, class, moved, as)
	if why != "" {
		return nil, why
	}
	edits = append(edits, lits...)
	edits = append(edits, l.moveMembers(decl, chosen, class, newType, as)...)
	slices.SortFunc(edits, func(x, y Edit) int { return x.Start - y.Start })
	return edits, ""
}

// movable says why the member m of name cannot move into newType, or "".
func (l *lined) movable(m *ast.Node, class bool, name, newType string, moved map[string]bool) string {
	what := l.source(m.Name())
	if _, _, ok := l.lineRange(m); !ok {
		return fmt.Sprintf("%s shares its line with other members; put each member on a line of its own", what)
	}
	if !class {
		switch {
		case !ast.IsPropertySignatureDeclaration(m) && !ast.IsMethodSignatureDeclaration(m):
			return fmt.Sprintf("%s is not a property or method", what)
		case m.PostfixToken() != nil && m.PostfixToken().Kind == ast.KindQuestionToken:
			return fmt.Sprintf("%s is optional, so a literal of %s may not say it; make it required first", what, name)
		}
		return ""
	}
	if !ast.IsPropertyDeclaration(m) && !ast.IsMethodDeclaration(m) {
		return fmt.Sprintf("%s is not a property or method", what)
	}
	if m.ModifierFlags()&(ast.ModifierFlagsPrivate|ast.ModifierFlagsProtected|ast.ModifierFlagsStatic|ast.ModifierFlagsAbstract) != 0 || ast.IsPrivateIdentifier(m.Name()) {
		return fmt.Sprintf("%s is private, protected, static or abstract, which %s could not reach in %s", what, name, newType)
	}
	if ast.IsPropertyDeclaration(m) {
		init := m.AsPropertyDeclaration().Initializer
		if init == nil {
			return fmt.Sprintf("%s has no initializer, so a constructor sets it; give it one where it is declared first", what)
		}
		var this *ast.Node
		inspect(init, func(n *ast.Node) bool {
			if n.Kind == ast.KindThisKeyword && this == nil {
				this = n
			}
			return this == nil
		})
		if this != nil {
			return fmt.Sprintf("%s's initializer reads this, at %s", what, l.where(this))
		}
		return ""
	}
	why := ""
	inspect(m, func(n *ast.Node) bool {
		if why != "" {
			return false
		}
		switch n.Kind {
		case ast.KindSuperKeyword:
			why = fmt.Sprintf("%s uses super, at %s", what, l.where(n))
		case ast.KindThisKeyword:
			pa := n.Parent
			if !ast.IsPropertyAccessExpression(pa) || pa.Expression() != n {
				why = fmt.Sprintf("%s uses this as a whole, at %s", what, l.where(n))
			} else if used := pa.Name().Text(); !moved[used] {
				why = fmt.Sprintf("%s uses %s, which stays in %s; move it too, or pass it in", what, used, name)
			}
		}
		return true
	})
	return why
}

// byName says why values of the type sym may not change shape: code that
// finds their properties by name or prints them, whose result would change.
func (p *Project) byName(c *checker.Checker, sym *ast.Symbol, name, newType string) string {
	holds := func(t *checker.Type) bool { return typeHolds(c, t, sym, map[*checker.Type]bool{}) }
	why := ""
	for _, f := range p.sources() {
		fl := newLined(f)
		inspect(f.AsNode(), func(n *ast.Node) bool {
			if why != "" {
				return false
			}
			switch {
			case ast.IsCallExpression(n) && ast.IsPropertyAccessExpression(n.Expression()):
				callee := fl.source(n.Expression())
				for _, a := range n.Arguments() {
					switch {
					case !holds(c.GetTypeAtLocation(a)):
					case byKey[callee]:
						why = fmt.Sprintf("a value of %s is passed to %s at %s, which finds properties by name, so moving members into %s would change what it does", name, callee, fl.where(n), newType)
					case strings.HasPrefix(callee, "console."):
						why = fmt.Sprintf("a value of %s is printed by %s at %s, whose output would change", name, callee, fl.where(n))
					}
				}
			case ast.IsForInStatement(n) && holds(c.GetTypeAtLocation(n.Expression())):
				why = fmt.Sprintf("a value of %s is looped over with for...in at %s, which finds properties by name", name, fl.where(n))
			case ast.IsBinaryExpression(n) && n.AsBinaryExpression().OperatorToken.Kind == ast.KindInKeyword && holds(c.GetTypeAtLocation(n.AsBinaryExpression().Right)):
				why = fmt.Sprintf("a value of %s is tested for a property by name at %s", name, fl.where(n))
			case ast.IsObjectBindingPattern(n) && holds(c.GetTypeAtLocation(n)):
				why = fmt.Sprintf("a value of %s is destructured at %s; read its members as x.m first", name, fl.where(n))
			}
			return true
		})
		if why != "" {
			return why
		}
	}
	return ""
}

// literals returns the edits that restructure the object literals of the
// type sym: the moved properties of each go into one literal held in as.
func (p *Project) literals(c *checker.Checker, sym *ast.Symbol, name string, class bool, moved map[string]bool, as string) ([]Edit, string) {
	var edits []Edit
	why := ""
	for _, f := range p.sources() {
		fl := newLined(f)
		inspect(f.AsNode(), func(n *ast.Node) bool {
			if why != "" {
				return false
			}
			if !ast.IsObjectLiteralExpression(n) {
				return true
			}
			t := c.GetContextualType(n, checker.ContextFlagsNone)
			if t == nil || t.Symbol() != sym && (t.Alias() == nil || t.Alias().Symbol() != sym) {
				return true
			}
			props := n.AsObjectLiteralExpression().Properties.Nodes
			var at []int
			for i, pr := range props {
				switch {
				case ast.IsSpreadAssignment(pr):
					why = fmt.Sprintf("a literal of %s at %s spreads another value, whose members would not move", name, fl.where(n))
					return false
				case pr.Name() != nil && moved[fl.source(pr.Name())]:
					at = append(at, i)
				}
			}
			if len(at) == 0 {
				return true
			}
			if class {
				why = fmt.Sprintf("an object literal stands for a %s at %s", name, fl.where(n))
				return false
			}
			var parts []string
			for _, i := range at {
				parts = append(parts, fl.source(props[i]))
			}
			first := props[at[0]]
			edits = append(edits, Edit{File: f.FileName(), Start: fl.pos(first), End: first.End(), NewText: as + ": { " + strings.Join(parts, ", ") + " }"})
			for _, i := range at[1:] {
				edits = append(edits, Edit{File: f.FileName(), Start: props[i-1].End(), End: props[i].End()})
			}
			return true
		})
		if why != "" {
			return nil, why
		}
	}
	return edits, ""
}

// moveMembers returns the edits that declare newType, just before decl,
// with the chosen members, and put the property as in their place.
func (l *lined) moveMembers(decl *ast.Node, chosen []*ast.Node, class bool, newType, as string) []Edit {
	ind := l.indent(l.line(l.pos(decl)))
	in := ind + l.unit()
	export := ""
	if ast.HasSyntacticModifier(decl, ast.ModifierFlagsExport) {
		export = "export "
	}
	kind := "interface "
	if class {
		kind = "class "
	}
	text := ind + export + kind + newType + " {\n"
	var edits []Edit
	for i, m := range chosen {
		a, b, _ := l.lineRange(m)
		text += l.reindent(a, b, l.indent(l.line(l.pos(m))), in)
		held := ""
		if i == 0 {
			if class {
				held = in + as + " = new " + newType + "()" + l.semi() + "\n"
			} else {
				end := ";"
				if s := strings.TrimSpace(l.text[a:b]); !strings.HasSuffix(s, ";") && !strings.HasSuffix(s, ",") {
					end = ""
				} else if strings.HasSuffix(s, ",") {
					end = ","
				}
				held = in + as + ": " + newType + end + "\n"
			}
		}
		edits = append(edits, Edit{File: l.f.FileName(), Start: a, End: b, NewText: held})
	}
	text += ind + "}\n\n"
	a, _, ok := l.lineRange(decl)
	if !ok {
		a = l.start(l.line(l.pos(decl)))
	}
	return append(edits, Edit{File: l.f.FileName(), Start: a, End: a, NewText: text})
}
