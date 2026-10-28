package tsapi

import (
	"fmt"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
)

// ChangeFieldType returns the edit that makes typ the declared type of
// the property name ("Type.property") of a class, an interface or a type
// alias of an object type, declared at the top level of the file at path:
// in place of its annotation, or added when it has none. Types are
// erased, so no value changes; whether every use still type-checks is
// the engine's check.
func (p *Project) ChangeFieldType(path, name, typ string) ([]Edit, string) {
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
	return []Edit{l.annotate(found[0], found[0].PostfixToken(), typ)}, ""
}

// ChangeParameterType returns the edit that makes typ the declared type
// of the parameter param of the function or method name (Class.method) in
// the file at path, as ChangeFieldType does for a property. A function
// with overload signatures is refused: they type its parameters apart.
func (p *Project) ChangeParameterType(path, name, param, typ string) ([]Edit, string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, why
	}
	fn, overloads, _, why := l.function(name)
	if why != "" {
		return nil, why
	}
	if len(overloads) > 0 {
		return nil, fmt.Sprintf("%s has overload signatures, which type its parameters apart; change their types by hand", name)
	}
	for _, prm := range fn.Parameters() {
		if nm := prm.Name(); nm == nil || !ast.IsIdentifier(nm) || nm.Text() != param {
			continue
		}
		if prm.Type() == nil && ast.IsArrowFunction(fn) && !strings.Contains(l.text[l.pos(fn):l.pos(prm)], "(") {
			// x => ... takes parentheses to take a type.
			return []Edit{{File: l.f.FileName(), Start: l.pos(prm), End: prm.End(), NewText: "(" + l.source(prm) + ": " + typ + ")"}}, ""
		}
		return []Edit{l.annotate(prm, prm.AsParameterDeclaration().QuestionToken, typ)}, ""
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
