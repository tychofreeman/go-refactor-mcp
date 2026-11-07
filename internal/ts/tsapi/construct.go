package tsapi

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/scanner"
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
// from before, with the constructor set up in each class that has fields
// with no value (no initializer, ? or !, not static, abstract or
// declared), one of them new since before. With no constructor, it writes
// one: a parameter per field, in order, of the field's type as written,
// assigning each, placed after the fields. With one whose body is empty
// or a stub's, as create_from_usage declares from new Pot(3), and whose
// parameters each fit by type exactly one of the fields, all of them
// covered, it gives that body the assignments. The notes name each class
// and its fields; none, and after as it is, when no class is so.
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
	type change struct {
		start, end int
		text       string
	}
	f := parseText(after)
	var changes []change
	var notes []string
	for _, s := range f.Statements.Nodes {
		if !ast.IsClassDeclaration(s) || s.Name() == nil {
			continue
		}
		class := s.Name().Text()
		fields := unsetFields(s, after)
		if !slices.ContainsFunc(fields, func(fl unsetField) bool { return !slices.Contains(old[class], fl.name) }) {
			continue
		}
		var names []string
		for _, fl := range fields {
			names = append(names, fl.name)
		}
		var ctors []*ast.Node
		for _, m := range s.Members() {
			if ast.IsConstructorDeclaration(m) {
				ctors = append(ctors, m)
			}
		}
		semi := semiOf(s, after)
		switch {
		case len(ctors) == 0:
			parts, why := placeMembers(f, after, constructorOf(fields, semi), class)
			if why != "" {
				continue
			}
			for _, p := range parts {
				changes = append(changes, change{p.at, p.at, p.prefix + p.text + p.suffix})
			}
			notes = append(notes, fmt.Sprintf("wrote %s's constructor, setting %s: with strictPropertyInitialization a field with no value needs one; add_declaration of a constructor replaces it", class, strings.Join(names, ", ")))
		case len(ctors) == 1 && ctors[0].Body() != nil:
			body := ctors[0].Body()
			from := scanner.GetTokenPosOfNode(body, f, false)
			params, ok := fitting(ctors[0], fields, after)
			if !emptyBody.MatchString(after[from:body.End()]) || !ok {
				continue
			}
			indent := leadingSpace(after[lineStartIn(after, scanner.GetTokenPosOfNode(ctors[0], f, false)):])
			var lines []string
			for i, fl := range fields {
				lines = append(lines, indent+"  this."+fl.name+" = "+params[i]+semi)
			}
			changes = append(changes, change{from, body.End(), "{\n" + strings.Join(lines, "\n") + "\n" + indent + "}"})
			notes = append(notes, fmt.Sprintf("%s's constructor now sets %s from its parameters (%s), which fit them by type: with strictPropertyInitialization a field with no value needs one", class, strings.Join(names, ", "), strings.Join(params, ", ")))
		}
	}
	slices.SortFunc(changes, func(a, b change) int { return b.start - a.start })
	for _, c := range changes {
		after = after[:c.start] + c.text + after[c.end:]
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

// emptyBody is a constructor body that does nothing yet: empty, as
// create_from_usage declares one, or a stub's.
var emptyBody = regexp.MustCompile(`^\{\s*(throw new Error\("not implemented"\);?\s*)?\}$`)

// fitting returns, for each of fields, the parameter of ctor whose type as
// written is that field's: false unless each field has one, no two fields
// or parameters share a type, and each parameter is a plain one.
func fitting(ctor *ast.Node, fields []unsetField, text string) ([]string, bool) {
	params := ctor.Parameters()
	if len(params) != len(fields) {
		return nil, false
	}
	byType := map[string][]string{}
	for _, p := range params {
		if p.Type() == nil || p.Name() == nil || !ast.IsIdentifier(p.Name()) || p.Initializer() != nil || p.ModifierFlags() != 0 {
			return nil, false
		}
		t := strings.TrimSpace(text[p.Type().Pos():p.Type().End()])
		byType[t] = append(byType[t], p.Name().Text())
	}
	typed := map[string]int{}
	for _, fl := range fields {
		typed[fl.typ]++
	}
	var out []string
	for _, fl := range fields {
		ps := byType[fl.typ]
		if len(ps) != 1 || typed[fl.typ] != 1 {
			return nil, false
		}
		out = append(out, ps[0])
	}
	return out, true
}
