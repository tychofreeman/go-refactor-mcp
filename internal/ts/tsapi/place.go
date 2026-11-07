package tsapi

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/scanner"
)

// Placed is a part of what add_declaration adds and where it goes in a
// file: Text, inserted at Offset.
type Placed struct {
	Offset int
	Text   string
}

// placedPart is one declaration or member of content: where it goes, the
// text around it there, its kind's rank, and its text.
type placedPart struct {
	at           int
	prefix, sep  string
	suffix, text string
	rank         int
}

// PlaceDeclarations says where the declarations of content go in text, a
// file's source. With class, content is members of that class: a field
// goes after the class's last field, a constructor after its last
// constructor or field, either before its first member when it has none
// of those, and any other member after its last one, at the class's
// indentation and with its spacing. Without class, a const, let, var,
// class or enum that a top-level statement of text names goes before the
// first such statement, where it is declared before it is used, and
// anything else at the end. Parts that go to one place stay together:
// fields, then constructors, then the rest, each in content's order. The
// string is a refusal, or "".
func PlaceDeclarations(text, content, class string) ([]Placed, string) {
	f := parseText(text)
	var parts []placedPart
	if class == "" {
		parts = placeStatements(f, text, content)
	} else {
		var why string
		if parts, why = placeMembers(f, text, content, class); why != "" {
			return nil, why
		}
	}
	var order []int
	byAt := map[int][]placedPart{}
	for _, p := range parts {
		if _, ok := byAt[p.at]; !ok {
			order = append(order, p.at)
		}
		byAt[p.at] = append(byAt[p.at], p)
	}
	var out []Placed
	for _, at := range order {
		group := byAt[at]
		slices.SortStableFunc(group, func(a, b placedPart) int { return a.rank - b.rank })
		var texts []string
		for _, p := range group {
			texts = append(texts, p.text)
		}
		out = append(out, Placed{Offset: at, Text: group[0].prefix + strings.Join(texts, group[0].sep) + group[0].suffix})
	}
	return out, ""
}

// placeStatements places content's top-level statements in f (see
// PlaceDeclarations).
func placeStatements(f *ast.SourceFile, text, content string) []placedPart {
	cf := parseText(content)
	nodes := cf.Statements.Nodes
	if len(cf.Diagnostics()) > 0 || len(nodes) == 0 {
		nodes = nil
	}
	end := func(s string) placedPart {
		p := placedPart{at: len(text), prefix: "\n", sep: "\n\n", suffix: "\n", text: s}
		if text != "" && !strings.HasSuffix(text, "\n") {
			p.prefix = "\n\n"
		}
		return p
	}
	if nodes == nil {
		return []placedPart{end(strings.Trim(content, "\n"))}
	}
	var parts []placedPart
	for i, s := range nodes {
		seg := strings.Trim(content[segmentStart(cf, content, nodes, i, 0):segmentEnd(cf, content, nodes, i, 0, len(content))], "\n")
		p := end(seg)
		if at := firstUse(f, text, declaredNames(s)); at >= 0 {
			p = placedPart{at: at, sep: "\n\n", suffix: "\n\n", text: seg}
		}
		parts = append(parts, p)
	}
	return parts
}

// placeMembers places content, members of class, in class's body in f
// (see PlaceDeclarations).
func placeMembers(f *ast.SourceFile, text, content, class string) ([]placedPart, string) {
	var cls *ast.Node
	for _, s := range f.Statements.Nodes {
		if ast.IsClassDeclaration(s) && s.Name() != nil && s.Name().Text() == class {
			cls = s
		}
	}
	if cls == nil {
		return nil, fmt.Sprintf("the file declares no class %s; type names the class whose members content is, and is left out for top-level declarations", class)
	}
	const wrap = "class __Content {\n"
	cf := parseText(wrap + content + "\n}\n")
	if len(cf.Diagnostics()) > 0 || len(cf.Statements.Nodes) != 1 || !ast.IsClassDeclaration(cf.Statements.Nodes[0]) {
		return nil, fmt.Sprintf("content is not members of a class; with type %s, give only the members, without the class around them", class)
	}
	members := cls.Members()
	start := func(m *ast.Node) int { return lineStartIn(text, scanner.GetTokenPosOfNode(m, f, true)) }
	classIndent := leadingSpace(text[lineStartIn(text, scanner.GetTokenPosOfNode(cls, f, false)):])
	indent, sep := classIndent+"  ", "\n"
	lastField, lastCtor := -1, -1
	for i, m := range members {
		if i == 0 {
			indent = leadingSpace(text[start(m):])
		} else if strings.Count(text[members[i-1].End():start(m)], "\n") >= 2 {
			sep = "\n\n"
		}
		switch {
		case ast.IsConstructorDeclaration(m):
			lastCtor = i
		case isFieldLike(m):
			lastField = i
		}
	}
	after := func(i int, rank int, seg string) placedPart {
		return placedPart{at: lineEndIn(text, members[i].End()), prefix: sep, sep: sep, text: seg, rank: rank}
	}
	var parts []placedPart
	cmembers := cf.Statements.Nodes[0].Members()
	for i, m := range cmembers {
		seg := reindent(strings.Trim(content[segmentStart(cf, content, cmembers, i, len(wrap)):segmentEnd(cf, content, cmembers, i, len(wrap), len(content))], "\n"), indent)
		rank, anchor := 2, len(members)-1
		switch {
		case isFieldLike(m):
			rank, anchor = 0, lastField
		case ast.IsConstructorDeclaration(m):
			rank, anchor = 1, max(lastCtor, lastField)
		}
		switch {
		case anchor >= 0:
			parts = append(parts, after(anchor, rank, seg))
		case len(members) > 0:
			parts = append(parts, placedPart{at: start(members[0]), sep: sep, suffix: sep, text: seg, rank: rank})
		default:
			closing := cls.End() - 1
			if strings.TrimSpace(text[lineStartIn(text, closing):closing]) == "" {
				parts = append(parts, placedPart{at: lineStartIn(text, closing), sep: sep, suffix: "\n", text: seg, rank: rank})
			} else {
				parts = append(parts, placedPart{at: closing, prefix: "\n", sep: sep, suffix: "\n" + classIndent, text: seg, rank: rank})
			}
		}
	}
	return parts, ""
}

// segmentStart is the offset in content where its i'th node's text
// starts: its first line, with its doc comment, or 0 for the first; skip
// is the length of what was parsed before content.
func segmentStart(cf *ast.SourceFile, content string, nodes []*ast.Node, i, skip int) int {
	if i == 0 {
		return 0
	}
	return lineStartIn(content, scanner.GetTokenPosOfNode(nodes[i], cf, true)-skip)
}

// segmentEnd is where the i'th node's text ends: where the next one's
// starts, or end for the last.
func segmentEnd(cf *ast.SourceFile, content string, nodes []*ast.Node, i, skip, end int) int {
	if i+1 < len(nodes) {
		return segmentStart(cf, content, nodes, i+1, skip)
	}
	return end
}

// isFieldLike reports whether a class member is a field (not one holding
// a function) or a static block: the members that run in the order
// written.
func isFieldLike(m *ast.Node) bool {
	return (ast.IsPropertyDeclaration(m) && functionBody(m.Initializer()) == nil) || ast.IsClassStaticBlockDeclaration(m)
}

// declaredNames are the names a top-level statement declares that cannot
// be used before it: its const, let and var names, or its class or enum.
func declaredNames(s *ast.Node) []string {
	switch {
	case ast.IsVariableStatement(s):
		var names []string
		for _, d := range s.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes {
			if nm := d.Name(); nm != nil && ast.IsIdentifier(nm) {
				names = append(names, nm.Text())
			}
		}
		return names
	case ast.IsClassDeclaration(s), ast.IsEnumDeclaration(s):
		if s.Name() != nil {
			return []string{s.Name().Text()}
		}
	}
	return nil
}

// firstUse is the offset of the start of the first top-level statement of
// f, other than a function, interface or type alias, that names one of
// names, or -1.
func firstUse(f *ast.SourceFile, text string, names []string) int {
	if len(names) == 0 {
		return -1
	}
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = regexp.QuoteMeta(n)
	}
	word := regexp.MustCompile(`(^|[^\w$])(` + strings.Join(quoted, "|") + `)($|[^\w$])`)
	for _, s := range f.Statements.Nodes {
		if ast.IsFunctionDeclaration(s) || ast.IsInterfaceDeclaration(s) || ast.IsTypeAliasDeclaration(s) {
			continue
		}
		if word.MatchString(text[scanner.GetTokenPosOfNode(s, f, false):s.End()]) {
			return lineStartIn(text, scanner.GetTokenPosOfNode(s, f, true))
		}
	}
	return -1
}

// lineStartIn is the offset of the start of the line holding off.
func lineStartIn(s string, off int) int {
	return strings.LastIndexByte(s[:off], '\n') + 1
}

// lineEndIn is the offset of the newline that ends the line holding off,
// or len(s).
func lineEndIn(s string, off int) int {
	if i := strings.IndexByte(s[off:], '\n'); i >= 0 {
		return off + i
	}
	return len(s)
}

// leadingSpace is the spaces and tabs s starts with.
func leadingSpace(s string) string {
	return s[:len(s)-len(strings.TrimLeft(s, " \t"))]
}

// reindent returns s with the indentation its lines share replaced by
// indent on each line that is not blank.
func reindent(s, indent string) string {
	lines := strings.Split(s, "\n")
	common, set := "", false
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if lead := leadingSpace(l); !set || len(lead) < len(common) {
			common, set = lead, true
		}
	}
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			lines[i] = indent + strings.TrimPrefix(l, common)
		}
	}
	return strings.Join(lines, "\n")
}
