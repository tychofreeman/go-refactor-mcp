package tsapi

import (
	"fmt"
	"slices"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
)

// StrictPropertyInitialization reports whether the project needs a
// class's fields set by its constructor: the option, or strict, which is
// on unless the config turns it off.
func (p *Project) StrictPropertyInitialization() bool {
	o := p.prog.GetProgram().Options()
	return o.GetStrictOptionValue(o.StrictPropertyInitialization)
}

// unsetField is a field of a class that has no value: its name and its
// type as written.
type unsetField struct{ name, typ string }

// WriteConstructors returns after, the text of a file an edit changed
// from before, with a constructor written in each class that has fields
// with no value (no initializer, ? or !, not static, abstract or
// declared), one of them new since before, and no constructor: a
// parameter per field, in order, of the field's type as written,
// assigning each, placed after the fields. The notes name each class and
// its fields; none, and after as it is, when no class is so.
func WriteConstructors(before, after string) (string, []string) {
	old := map[string][]string{}
	bf := parseText(before)
	for _, s := range bf.Statements.Nodes {
		if ast.IsClassDeclaration(s) && s.Name() != nil {
			for _, fl := range unsetFields(s, before) {
				old[s.Name().Text()] = append(old[s.Name().Text()], fl.name)
			}
		}
	}
	f := parseText(after)
	var placed []Placed
	var notes []string
	for _, s := range f.Statements.Nodes {
		if !ast.IsClassDeclaration(s) || s.Name() == nil || slices.ContainsFunc(s.Members(), ast.IsConstructorDeclaration) {
			continue
		}
		class := s.Name().Text()
		fields := unsetFields(s, after)
		if !slices.ContainsFunc(fields, func(fl unsetField) bool { return !slices.Contains(old[class], fl.name) }) {
			continue
		}
		parts, why := placeMembers(f, after, constructorOf(fields, semiOf(s, after)), class)
		if why != "" {
			continue
		}
		var names []string
		for _, fl := range fields {
			names = append(names, fl.name)
		}
		for _, p := range parts {
			placed = append(placed, Placed{Offset: p.at, Text: p.prefix + p.text + p.suffix})
		}
		notes = append(notes, fmt.Sprintf("wrote %s's constructor, setting %s: with strictPropertyInitialization a field with no value needs one; add_declaration of a constructor replaces it", class, strings.Join(names, ", ")))
	}
	slices.SortFunc(placed, func(a, b Placed) int { return b.Offset - a.Offset })
	for _, p := range placed {
		after = after[:p.Offset] + p.Text + after[p.Offset:]
	}
	return after, notes
}

// unsetFields are the fields of class that have no value, in order.
func unsetFields(class *ast.Node, text string) []unsetField {
	var out []unsetField
	for _, m := range class.Members() {
		if !ast.IsPropertyDeclaration(m) || m.Initializer() != nil || m.PostfixToken() != nil || m.Type() == nil || m.Name() == nil ||
			m.ModifierFlags()&(ast.ModifierFlagsStatic|ast.ModifierFlagsAbstract|ast.ModifierFlagsAmbient) != 0 ||
			!(ast.IsIdentifier(m.Name()) || ast.IsPrivateIdentifier(m.Name())) {
			continue
		}
		t := m.Type()
		out = append(out, unsetField{name: m.Name().Text(), typ: strings.TrimSpace(text[t.Pos():t.End()])})
	}
	return out
}

// constructorOf is a constructor with a parameter per field that assigns
// each, semi ending its statements.
func constructorOf(fields []unsetField, semi string) string {
	var params, body []string
	for _, fl := range fields {
		param := strings.TrimPrefix(fl.name, "#")
		params = append(params, param+": "+fl.typ)
		body = append(body, "  this."+fl.name+" = "+param+semi)
	}
	return "constructor(" + strings.Join(params, ", ") + ") {\n" + strings.Join(body, "\n") + "\n}"
}

// semiOf is ";" when class's fields end with one, as the file writes
// statements, else "".
func semiOf(class *ast.Node, text string) string {
	for _, m := range class.Members() {
		if ast.IsPropertyDeclaration(m) && !strings.HasSuffix(strings.TrimSpace(text[m.Pos():m.End()]), ";") {
			return ""
		}
	}
	return ";"
}
