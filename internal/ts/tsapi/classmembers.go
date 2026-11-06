package tsapi

import (
	"fmt"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
)

// ClassMembers returns the members a new class needs to implement the
// interfaces named in implements and to extend base: for each method
// signature of the interfaces (and those they extend) and each abstract
// method of base (and its abstract bases), the method with a body that
// throws; for each required property, the property with a
// definite-assignment mark. Each name is found among the project's
// top-level interfaces and classes; found is false when one is not.
func (p *Project) ClassMembers(implements []string, base string) (members []string, found bool, refusal string) {
	decls := map[string][]*ast.Node{}
	files := map[*ast.Node]*lined{}
	for _, f := range p.sources() {
		l := newLined(f)
		for _, s := range f.Statements.Nodes {
			if (ast.IsInterfaceDeclaration(s) || ast.IsClassDeclaration(s)) && s.Name() != nil {
				decls[s.Name().Text()] = append(decls[s.Name().Text()], s)
				files[s] = l
			}
		}
	}
	seen := map[string]bool{}
	have := map[string]bool{}
	var add func(name string, abstractOnly bool) (bool, string)
	add = func(name string, abstractOnly bool) (bool, string) {
		if seen[name] {
			return true, ""
		}
		seen[name] = true
		ds := decls[name]
		if len(ds) == 0 {
			return false, ""
		}
		for _, d := range ds {
			l := files[d]
			var list []*ast.Node
			var heritage *ast.NodeList
			if ast.IsInterfaceDeclaration(d) {
				if abstractOnly {
					return true, fmt.Sprintf("%s is an interface: give it in implements, not as the class extended", name)
				}
				list, heritage = d.AsInterfaceDeclaration().Members.Nodes, d.AsInterfaceDeclaration().HeritageClauses
			} else {
				if !abstractOnly {
					return true, fmt.Sprintf("%s is a class: give it as the class extended (extends %s), not in implements", name, name)
				}
				if !ast.HasSyntacticModifier(d, ast.ModifierFlagsAbstract) {
					return true, ""
				}
				list, heritage = d.AsClassDeclaration().Members.Nodes, d.AsClassDeclaration().HeritageClauses
			}
			for _, m := range list {
				if m.Name() == nil || have[m.Name().Text()] || (m.PostfixToken() != nil && m.PostfixToken().Kind == ast.KindQuestionToken) {
					continue
				}
				if abstractOnly && !ast.HasSyntacticModifier(m, ast.ModifierFlagsAbstract) {
					continue
				}
				text := strings.TrimSuffix(strings.TrimSpace(l.source(m)), ";")
				text = strings.TrimPrefix(text, "abstract ")
				switch {
				case ast.IsMethodSignatureDeclaration(m) || ast.IsMethodDeclaration(m):
					text += " {\n    throw new Error(\"not implemented\");\n  }"
				case ast.IsPropertySignatureDeclaration(m) || ast.IsPropertyDeclaration(m):
					nameEnd := m.Name().End() - l.pos(m)
					rest := strings.TrimPrefix(strings.TrimSpace(l.source(m)), "abstract ")
					nameEnd -= len(strings.TrimSpace(l.source(m))) - len(rest)
					text = strings.TrimSuffix(rest[:nameEnd]+"!"+rest[nameEnd:], ";") + ";"
				default:
					continue
				}
				have[m.Name().Text()] = true
				members = append(members, text)
			}
			for _, h := range nodes(heritage) {
				for _, t := range h.AsHeritageClause().Types.Nodes {
					if e := t.Expression(); e != nil && ast.IsIdentifier(e) {
						if ok, why := add(e.Text(), !ast.IsInterfaceDeclaration(d)); why != "" || !ok {
							return ok, why
						}
					}
				}
			}
		}
		return true, ""
	}
	if base != "" {
		if ok, why := add(base, true); why != "" || !ok {
			return nil, ok, why
		}
	}
	for _, name := range implements {
		if ok, why := add(name, false); why != "" || !ok {
			return nil, ok, why
		}
	}
	return members, true, ""
}
