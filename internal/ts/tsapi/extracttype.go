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
	var ctorSet []*ast.Node
	private := false
	for _, m := range chosen {
		if class && ast.IsPropertyDeclaration(m) && m.AsPropertyDeclaration().Initializer == nil {
			ctorSet = append(ctorSet, m)
		}
		if m.ModifierFlags()&ast.ModifierFlagsPrivate != 0 {
			private = true
		}
	}
	var ctorEdits []Edit
	var rewritten [][2]int
	if len(ctorSet) > 0 {
		ctorEdits, rewritten, why = l.buildInConstructor(c, decl, ctorSet, chosen, newType, as)
		if why != "" {
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
		case ast.GetSourceFileOfNode(id) == l.f && slices.ContainsFunc(rewritten, func(r [2]int) bool { return id.Pos() >= r[0] && id.End() <= r[1] }):
			// In a statement of the constructor that buildInConstructor rewrites.
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
	edits = append(edits, ctorEdits...)
	edits = append(edits, l.moveMembers(c, decl, chosen, class, newType, as, ctorSet, private)...)
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
	if m.ModifierFlags()&(ast.ModifierFlagsProtected|ast.ModifierFlagsStatic|ast.ModifierFlagsAbstract) != 0 || ast.IsPrivateIdentifier(m.Name()) {
		return fmt.Sprintf("%s is protected, static, abstract or #private, which %s could not reach in %s", what, name, newType)
	}
	if ast.IsPropertyDeclaration(m) {
		init := m.AsPropertyDeclaration().Initializer
		if init == nil {
			return "" // the constructor sets it: buildInConstructor
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
func (l *lined) moveMembers(c *checker.Checker, decl *ast.Node, chosen []*ast.Node, class bool, newType, as string, ctorSet []*ast.Node, private bool) []Edit {
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
		var drop []repl
		if mods := m.Modifiers(); private && mods != nil {
			// Private to the holder now: the moved member is the new class's own.
			for _, mod := range mods.Nodes {
				if mod.Kind == ast.KindPrivateKeyword {
					end := mod.End()
					for end < len(l.text) && l.text[end] == ' ' {
						end++
					}
					drop = append(drop, repl{start: l.pos(mod), end: end})
				}
			}
		}
		text += l.reindent(a, b, l.indent(l.line(l.pos(m))), in, drop...)
		held := ""
		if i == 0 {
			if class {
				mods := ""
				if private {
					mods = "private "
					if allReadonly(chosen) {
						mods += "readonly "
					}
				}
				if len(ctorSet) > 0 {
					held = in + mods + as + ": " + newType + l.semi() + "\n"
				} else {
					held = in + mods + as + " = new " + newType + "()" + l.semi() + "\n"
				}
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
	if len(ctorSet) > 0 {
		var params, sets []string
		for _, m := range ctorSet {
			f := l.source(m.Name())
			t := ""
			if m.Type() != nil {
				t = l.source(m.Type())
			} else {
				t = c.TypeToString(c.GetTypeAtLocation(m.Name()))
			}
			params = append(params, f+": "+t)
			sets = append(sets, in+l.unit()+"this."+f+" = "+f+l.semi()+"\n")
		}
		text += "\n" + in + "constructor(" + strings.Join(params, ", ") + ") {\n" + strings.Join(sets, "") + in + "}\n"
	}
	text += ind + "}\n\n"
	a, _, ok := l.lineRange(decl)
	if !ok {
		a = l.start(l.line(l.pos(decl)))
	}
	return append(edits, Edit{File: l.f.FileName(), Start: a, End: a, NewText: text})
}

// allReadonly reports whether every property of members is readonly.
func allReadonly(members []*ast.Node) bool {
	for _, m := range members {
		if ast.IsPropertyDeclaration(m) && m.ModifierFlags()&ast.ModifierFlagsReadonly == 0 {
			return false
		}
	}
	return true
}

// buildInConstructor returns the edits that build the holder in decl's
// constructor, for fields that have no initializer, from the statements
// that set them (this.f = e;, each e reading only the constructor's
// parameters and literals): the first becomes this.as = new
// NewType(e1, ...), the others go. It also returns the ranges of those
// statements, whose references are rewritten so. As java's #345.
func (l *lined) buildInConstructor(c *checker.Checker, decl *ast.Node, fields, chosen []*ast.Node, newType, as string) ([]Edit, [][2]int, string) {
	var ctor *ast.Node
	for _, m := range decl.Members() {
		if ast.IsConstructorDeclaration(m) && m.Body() != nil {
			ctor = m
		}
	}
	if ctor == nil {
		return nil, nil, fmt.Sprintf("%s has no initializer and no constructor sets it; give it one where it is declared first", l.source(fields[0].Name()))
	}
	stmts := ctor.Body().Statements()
	sets := map[string]int{}
	for i, s := range stmts {
		if !ast.IsExpressionStatement(s) {
			continue
		}
		e := s.Expression()
		if !ast.IsBinaryExpression(e) || e.AsBinaryExpression().OperatorToken.Kind != ast.KindEqualsToken {
			continue
		}
		left := e.AsBinaryExpression().Left
		if !ast.IsPropertyAccessExpression(left) || left.Expression().Kind != ast.KindThisKeyword {
			continue
		}
		name := left.Name().Text()
		for _, f := range fields {
			if l.source(f.Name()) != name {
				continue
			}
			if _, twice := sets[name]; twice {
				return nil, nil, fmt.Sprintf("the constructor sets %s twice; leave it out of fields", name)
			}
			sets[name] = i
		}
	}
	var args []string
	lo, hi := len(stmts), -1
	for _, f := range fields {
		name := l.source(f.Name())
		i, ok := sets[name]
		if !ok {
			return nil, nil, fmt.Sprintf("the constructor does not set %s in a statement of its own body (this.%s = ...;), so the holder cannot be built there; give it an initializer, or leave it out", name, name)
		}
		right := stmts[i].Expression().AsBinaryExpression().Right
		if why := l.fromParameters(c, ctor, right); why != "" {
			return nil, nil, fmt.Sprintf("the constructor sets %s to %s, which %s; built into the holder it would run at another point. Assign it to a local first, or leave %s out", name, l.source(right), why, name)
		}
		args = append(args, l.source(right))
		lo, hi = min(lo, i), max(hi, i)
	}
	set := map[int]bool{}
	for _, i := range sets {
		set[i] = true
	}
	for i := lo + 1; i < hi; i++ {
		if set[i] {
			continue
		}
		why := ""
		inspect(stmts[i], func(n *ast.Node) bool {
			if why == "" && ast.IsPropertyAccessExpression(n) && n.Expression().Kind == ast.KindThisKeyword && slices.ContainsFunc(chosen, func(m *ast.Node) bool { return m.Name() != nil && l.source(m.Name()) == n.Name().Text() }) {
				why = fmt.Sprintf("the constructor reads %s between setting the fields (%s), and the holder sets them all at once; move that statement first", l.source(n), l.where(n))
			}
			return why == ""
		})
		if why != "" {
			return nil, nil, why
		}
	}
	var edits []Edit
	var ranges [][2]int
	for i := range set {
		s := stmts[i]
		a, b, ok := l.lineRange(s)
		if !ok {
			a, b = l.pos(s), s.End()
		}
		ranges = append(ranges, [2]int{a, b})
		if i == lo {
			edits = append(edits, Edit{File: l.f.FileName(), Start: l.pos(s), End: s.End(), NewText: "this." + as + " = new " + newType + "(" + strings.Join(args, ", ") + ")" + l.semi()})
		} else {
			edits = append(edits, Edit{File: l.f.FileName(), Start: a, End: b})
		}
	}
	return edits, ranges, ""
}

// fromParameters says why e reads more than the parameters of ctor that
// it never assigns, and literals, or calls or makes something; or "".
func (l *lined) fromParameters(c *checker.Checker, ctor, e *ast.Node) string {
	why := ""
	inspect(e, func(n *ast.Node) bool {
		if why != "" {
			return false
		}
		switch {
		case ast.IsCallExpression(n), ast.IsNewExpression(n), ast.IsTaggedTemplateExpression(n), ast.IsAwaitExpression(n), ast.IsYieldExpression(n):
			why = "calls or makes something"
		case n.Kind == ast.KindThisKeyword:
			why = "reads this"
		case ast.IsBinaryExpression(n) && ast.IsAssignmentOperator(n.AsBinaryExpression().OperatorToken.Kind),
			ast.IsPrefixUnaryExpression(n) && isUpdate(n.AsPrefixUnaryExpression().Operator), ast.IsPostfixUnaryExpression(n):
			why = "assigns"
		case ast.IsFunctionLike(n):
			return false
		case ast.IsIdentifier(n):
			if p := n.Parent; p != nil && ast.IsPropertyAccessExpression(p) && p.Name() == n {
				return true
			}
			s := c.GetSymbolAtLocation(n)
			param := s != nil && s.ValueDeclaration != nil && ast.IsParameterDeclaration(s.ValueDeclaration) && s.ValueDeclaration.Parent == ctor
			if !param && n.Text() != "undefined" {
				why = "reads " + n.Text() + ", not a parameter"
			} else if param && l.assigns(ctor.Body(), s, c) != "" {
				why = "reads " + n.Text() + ", which the constructor assigns"
			}
		}
		return why == ""
	})
	return why
}
