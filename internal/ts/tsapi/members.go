package tsapi

import (
	"fmt"
	"slices"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/scanner"
)

// MemberPlace returns where new members of the type called name go in the
// file at path: after its member called after, before the one called
// before, or at its end. The type is an interface, a class, or a type
// alias of an object type ({ ... }); kind is "class" for a class and
// "interface" otherwise. A type on one line is opened up into lines, one
// member a line.
func (p *Project) MemberPlace(path, name, after, before string) (pl Place, kind string, refusal string) {
	l, refusal := p.lined(path)
	if refusal != "" {
		return Place{}, "", refusal
	}
	var members *ast.NodeList
	for _, s := range l.f.Statements.Nodes {
		if nm := s.Name(); nm == nil || nm.Text() != name {
			continue
		}
		switch {
		case ast.IsInterfaceDeclaration(s):
			members, kind = s.AsInterfaceDeclaration().Members, "interface"
		case ast.IsClassDeclaration(s):
			members, kind = s.AsClassDeclaration().Members, "class"
		case ast.IsTypeAliasDeclaration(s):
			t := s.AsTypeAliasDeclaration().Type
			if !ast.IsTypeLiteralNode(t) {
				return Place{}, "", fmt.Sprintf("type %s is not an object type ({ ... }); add the property to the type it names", name)
			}
			members, kind = t.AsTypeLiteralNode().Members, "interface"
		}
	}
	if members == nil {
		return Place{}, "", fmt.Sprintf("the file declares no interface, class or object type %s", name)
	}
	open := strings.LastIndexByte(l.text[:members.Pos()], '{')
	closing := members.End() + strings.IndexByte(l.text[members.End():], '}')
	nodes := members.Nodes
	nameOf := func(m *ast.Node) string {
		if nm := m.Name(); nm != nil {
			return l.text[l.pos(nm):nm.End()]
		}
		return ""
	}
	at := len(nodes)
	switch {
	case after != "" && before != "":
		return Place{}, "", "give after or before, not both"
	case after != "" || before != "":
		target := after + before
		i := slices.IndexFunc(nodes, func(m *ast.Node) bool { return nameOf(m) == target })
		if i < 0 {
			return Place{}, "", fmt.Sprintf("%s has no member %s", name, target)
		}
		at = i
		if after != "" {
			at = i + 1
		}
	}
	outer := l.indent(l.line(open))
	inner := outer + l.unit()
	if l.line(open) == l.line(closing) {
		line := func(ms []*ast.Node) string {
			var sb strings.Builder
			for _, m := range ms {
				text := strings.TrimSpace(l.text[l.pos(m):m.End()])
				if !strings.HasSuffix(text, ";") && !strings.HasSuffix(text, ",") {
					text += ";"
				}
				sb.WriteString(inner + text + "\n")
			}
			return sb.String()
		}
		return Place{At: open + 1, End: closing, Indent: inner, Prefix: "\n" + line(nodes[:at]), Suffix: "\n" + line(nodes[at:]) + outer}, kind, ""
	}
	if len(nodes) == 0 {
		return Place{At: open + 1, End: open + 1, Indent: inner, Prefix: "\n"}, kind, ""
	}
	if at < len(nodes) {
		start := l.start(l.line(scanner.GetTokenPosOfNode(nodes[at], l.f, true)))
		return Place{At: start, End: start, Indent: l.indent(l.line(l.pos(nodes[at]))), Suffix: "\n"}, kind, ""
	}
	last := nodes[len(nodes)-1]
	end := l.start(l.line(last.End()-1) + 1)
	return Place{At: end, End: end, Indent: l.indent(l.line(l.pos(last))), Suffix: "\n"}, kind, ""
}

// ParseMembers parses content as members of an interface, or of a class
// when kind is "class", and returns their names and whether a template
// literal spans lines. It refuses content that is not such members.
func ParseMembers(content, kind string) (names []string, raw bool, refusal string) {
	head := "interface __Members {\n"
	if kind == "class" {
		head = "class __Members {\n"
	}
	f := parseText(head + content + "\n}\n")
	if ds := f.Diagnostics(); len(ds) > 0 {
		return nil, false, fmt.Sprintf("field is not the members of %s %s: %s", article(kind), kind, messages(ds))
	}
	if len(f.Statements.Nodes) != 1 {
		return nil, false, "field closes the type it goes in: its braces do not balance"
	}
	for _, m := range f.Statements.Nodes[0].Members() {
		if nm := m.Name(); nm != nil {
			names = append(names, nm.Text())
		}
	}
	return names, multiline(f.AsNode()), ""
}

func article(kind string) string {
	if kind == "interface" {
		return "an"
	}
	return "a"
}
