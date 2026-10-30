package tsapi

import (
	"context"
	"fmt"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/checker"
)

// ChangeFieldType returns the edit that makes typ the declared type of
// the property name ("Type.property") of a class, an interface or a type
// alias of an object type, declared at the top level of the file at path:
// in place of its annotation, or added when it has none. Types are
// erased, so no value changes; whether every use still type-checks is
// the engine's check. values maps old constants to the new type's values,
// put where the property is given, assigned, compared or switched on
// with them (valueEdits).
func (p *Project) ChangeFieldType(path, name, typ string, values map[string]string) ([]Edit, string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, why
	}
	owner, prop, ok := strings.Cut(name, ".")
	if !ok {
		return nil, fmt.Sprintf("name the property as Type.property, not %q", name)
	}
	members, why := l.members(owner)
	if why != "" {
		return nil, why
	}
	var found []*ast.Node
	for _, m := range members {
		if m.Name() != nil && l.source(m.Name()) == prop {
			found = append(found, m)
		}
	}
	switch {
	case len(found) == 0:
		return nil, fmt.Sprintf("%s has no property %s", owner, prop)
	case len(found) > 1 || !(ast.IsPropertyDeclaration(found[0]) || ast.IsPropertySignatureDeclaration(found[0])):
		return nil, fmt.Sprintf("%s is not a property but a method or an accessor; change a method's parameter with change_parameter_type", name)
	}
	return append([]Edit{l.annotate(found[0], found[0].PostfixToken(), typ)}, p.valueEdits(found[0], values)...), ""
}

// valueEdits returns the edits that put, for each old constant of values
// ("fern", or as written), its new value wherever the property prop is
// given that constant in an object literal of its type, assigned it,
// compared with it (=== and ==, either side), or switched on with it as a
// case.
func (p *Project) valueEdits(prop *ast.Node, values map[string]string) []Edit {
	c, done := p.prog.GetProgram().GetTypeChecker(context.Background())
	defer done()
	sym := c.GetSymbolAtLocation(prop.Name())
	if sym == nil {
		return nil
	}
	isProp := func(e *ast.Node) bool {
		e = ast.SkipParentheses(e)
		return ast.IsPropertyAccessExpression(e) && c.GetSymbolAtLocation(e.Name()) == sym
	}
	var edits []Edit
	for _, f := range p.sources() {
		fl := newLined(f)
		put := func(e *ast.Node) {
			e = ast.SkipParentheses(e)
			for old, v := range values {
				if fl.source(e) == old || ast.IsStringLiteral(e) && e.Text() == strings.Trim(old, `"'`) {
					edits = append(edits, Edit{File: f.FileName(), Start: fl.pos(e), End: e.End(), NewText: strings.TrimSpace(v)})
					return
				}
			}
		}
		inspect(f.AsNode(), func(n *ast.Node) bool {
			switch {
			case ast.IsPropertyAssignment(n) && ast.IsObjectLiteralExpression(n.Parent):
				if t := c.GetContextualType(n.Parent, checker.ContextFlagsNone); t != nil && n.Name() != nil && c.GetPropertyOfType(t, fl.source(n.Name())) == sym {
					put(n.Initializer())
				}
			case ast.IsBinaryExpression(n):
				b := n.AsBinaryExpression()
				switch b.OperatorToken.Kind {
				case ast.KindEqualsToken:
					if isProp(b.Left) {
						put(b.Right)
					}
				case ast.KindEqualsEqualsEqualsToken, ast.KindExclamationEqualsEqualsToken, ast.KindEqualsEqualsToken, ast.KindExclamationEqualsToken:
					if isProp(b.Left) {
						put(b.Right)
					} else if isProp(b.Right) {
						put(b.Left)
					}
				}
			case ast.IsSwitchStatement(n) && isProp(n.Expression()):
				for _, cl := range n.AsSwitchStatement().CaseBlock.AsCaseBlock().Clauses.Nodes {
					if ast.IsCaseClause(cl) {
						put(cl.Expression())
					}
				}
			}
			return true
		})
	}
	return edits
}

// ChangeParameterType returns the edit that makes typ the declared type
// of the parameter param of the function or method name (Class.method, or
// Iface.method) in the file at path, and of the parameter at its position
// in each method of its family, as ChangeFieldType does for a property. A
// function with overload signatures is refused: they type its parameters
// apart.
func (p *Project) ChangeParameterType(path, name, param, typ string) ([]Edit, string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, why
	}
	fn, overloads, _, why := l.function(name)
	if why != "" {
		if fn = l.interfaceMethod(name); fn == nil {
			return nil, why
		}
	}
	if len(overloads) > 0 {
		return nil, fmt.Sprintf("%s has overload signatures, which type its parameters apart; change their types by hand", name)
	}
	list, off := passed(fn)
	for i, prm := range list.Nodes[off:] {
		if nm := prm.Name(); nm == nil || !ast.IsIdentifier(nm) || nm.Text() != param {
			continue
		}
		if prm.Type() == nil && ast.IsArrowFunction(fn) && !strings.Contains(l.text[l.pos(fn):l.pos(prm)], "(") {
			// x => ... takes parentheses to take a type.
			return []Edit{{File: l.f.FileName(), Start: l.pos(prm), End: prm.End(), NewText: "(" + l.source(prm) + ": " + typ + ")"}}, ""
		}
		edits := []Edit{l.annotate(prm, prm.AsParameterDeclaration().QuestionToken, typ)}
		c, done := p.prog.GetProgram().GetTypeChecker(context.Background())
		defer done()
		family, why := p.family(c, fn)
		if why != "" {
			return nil, why
		}
		for _, m := range family {
			ml := newLined(ast.GetSourceFileOfNode(m))
			if mlist, moff := passed(m); moff+i < len(mlist.Nodes) {
				mp := mlist.Nodes[moff+i]
				edits = append(edits, ml.annotate(mp, mp.AsParameterDeclaration().QuestionToken, typ))
			}
		}
		return edits, ""
	}
	return nil, fmt.Sprintf("%s has no parameter named %s (a destructured parameter is not changed)", name, param)
}

// ChangeType returns the edit that makes typ what the type alias name, at
// the top level of the file at path, stands for.
func (p *Project) ChangeType(path, name, typ string) ([]Edit, string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, why
	}
	for _, s := range l.f.Statements.Nodes {
		if s.Name() == nil || s.Name().Text() != name {
			continue
		}
		switch {
		case ast.IsTypeAliasDeclaration(s):
			t := s.Type()
			return []Edit{{File: l.f.FileName(), Start: l.pos(t), End: t.End(), NewText: typ}}, ""
		case ast.IsInterfaceDeclaration(s), ast.IsClassDeclaration(s):
			return nil, fmt.Sprintf("%s is an interface or a class, not a type alias; change its members with change_field_type", name)
		}
	}
	return nil, fmt.Sprintf("the file declares no type alias %s", name)
}

// annotate returns the edit that makes typ the annotation of decl, a
// property or a parameter: in place of its type, or after its name and
// postfix (? or !) when it has none.
func (l *lined) annotate(decl, postfix *ast.Node, typ string) Edit {
	if t := decl.Type(); t != nil {
		return Edit{File: l.f.FileName(), Start: l.pos(t), End: t.End(), NewText: typ}
	}
	at := decl.Name().End()
	if postfix != nil {
		at = postfix.End()
	}
	return Edit{File: l.f.FileName(), Start: at, End: at, NewText: ": " + typ}
}

// members returns the members of the class, interface or object type
// alias named owner at the top level of the file.
func (l *lined) members(owner string) ([]*ast.Node, string) {
	for _, s := range l.f.Statements.Nodes {
		if s.Name() == nil || s.Name().Text() != owner {
			continue
		}
		switch {
		case ast.IsClassDeclaration(s), ast.IsInterfaceDeclaration(s):
			return s.Members(), ""
		case ast.IsTypeAliasDeclaration(s) && ast.IsTypeLiteralNode(s.Type()):
			return s.Type().Members(), ""
		case ast.IsTypeAliasDeclaration(s):
			return nil, fmt.Sprintf("%s is a type alias of %s, not of an object type; change it with change_type", owner, l.source(s.Type()))
		}
	}
	return nil, fmt.Sprintf("the file declares no class, interface or object type %s", owner)
}
