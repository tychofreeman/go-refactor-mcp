package tsapi

import (
	"context"
	"fmt"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
)

// InlineClass returns the edits that inline the class held in the property
// field of the class holder, in the file at path, into holder, as Go's
// inline_struct does for a struct: the held class's members take the
// property's place, every x.field.m becomes x.m, and the held class goes.
// The property must be initialized with new S() of a class S of this file,
// with no arguments; S must extend nothing, have no constructor or static
// members, be used nowhere else, and share no member name with holder; and
// the property may only be used as x.field.m.
func (p *Project) InlineClass(ctx context.Context, path, holder, field string) ([]Edit, string) {
	l, why := p.lined(path)
	if why != "" {
		return nil, why
	}
	var h *ast.Node
	for _, d := range l.topLevel(holder) {
		if ast.IsClassDeclaration(d) {
			h = d
		}
	}
	if h == nil {
		return nil, fmt.Sprintf("no class %s is declared in this file", holder)
	}
	var prop *ast.Node
	for _, m := range h.AsClassDeclaration().Members.Nodes {
		if ast.IsPropertyDeclaration(m) && m.Name() != nil && ast.IsIdentifier(m.Name()) && m.Name().Text() == field {
			prop = m
		}
	}
	if prop == nil {
		return nil, fmt.Sprintf("class %s has no property %s", holder, field)
	}
	init := prop.AsPropertyDeclaration().Initializer
	if init == nil || !ast.IsNewExpression(init) || !ast.IsIdentifier(init.Expression()) {
		return nil, fmt.Sprintf("%s is not initialized with new S() of a class S, which inline takes", field)
	}
	if args := init.ArgumentList(); args != nil && len(args.Nodes) > 0 {
		return nil, fmt.Sprintf("%s is initialized with %s, not new %s() with no arguments", field, l.source(init), init.Expression().Text())
	}
	c, done := p.prog.GetProgram().GetTypeCheckerForFile(ctx, l.f)
	defer done()
	sym := symbolOf(c, init.Expression())
	if sym == nil || sym.ValueDeclaration == nil || !ast.IsClassDeclaration(sym.ValueDeclaration) || ast.GetSourceFileOfNode(sym.ValueDeclaration) != l.f {
		return nil, fmt.Sprintf("%s is not a class declared in this file", init.Expression().Text())
	}
	s := sym.ValueDeclaration
	name := sym.Name
	if hs := s.AsClassDeclaration().HeritageClauses; hs != nil && len(hs.Nodes) > 0 {
		return nil, fmt.Sprintf("%s extends or implements another type, which %s would then have to", name, holder)
	}
	members := s.AsClassDeclaration().Members.Nodes
	if len(members) == 0 {
		return nil, fmt.Sprintf("%s has no members to inline", name)
	}
	taken := map[string]bool{}
	for _, m := range h.AsClassDeclaration().Members.Nodes {
		if m != prop && m.Name() != nil {
			taken[m.Name().Text()] = true
		}
	}
	for _, m := range members {
		switch {
		case ast.IsConstructorDeclaration(m):
			return nil, fmt.Sprintf("%s has a constructor (%s), which %s's would have to run", name, l.where(m), holder)
		case m.ModifierFlags()&ast.ModifierFlagsStatic != 0:
			return nil, fmt.Sprintf("%s has a static member (%s)", name, l.where(m))
		case m.Name() != nil && taken[m.Name().Text()]:
			return nil, fmt.Sprintf("%s and %s both have %s", holder, name, m.Name().Text())
		}
	}
	var edits []Edit
	for _, f := range p.prog.GetProgram().GetSourceFiles() {
		if f.IsDeclarationFile {
			continue
		}
		fl := newLined(f)
		psym := c.GetSymbolAtLocation(prop.Name())
		var refusal string
		identifiers(f.AsNode(), func(id *ast.Node) {
			if refusal != "" || id == s.Name() || id == prop.Name() {
				return
			}
			switch symbolOf(c, id) {
			case sym:
				if id.Pos() < prop.Pos() || id.Pos() >= prop.End() {
					refusal = fmt.Sprintf("%s is used outside %s.%s, at %s", name, holder, field, fl.where(id))
				}
			case psym:
				pa := id.Parent
				if !ast.IsPropertyAccessExpression(pa) || pa.Name() != id || !ast.IsPropertyAccessExpression(pa.Parent) || pa.Parent.Expression() != pa ||
					pa.AsPropertyAccessExpression().QuestionDotToken != nil || pa.Parent.AsPropertyAccessExpression().QuestionDotToken != nil {
					refusal = fmt.Sprintf("the value of %s is used whole at %s, which would have no object to stand for once %s is inlined", field, fl.where(id), name)
					return
				}
				edits = append(edits, Edit{File: f.FileName(), Start: pa.Expression().End(), End: pa.End(), NewText: ""})
			}
		})
		if refusal != "" {
			return nil, refusal
		}
	}
	pa, pb, ok := l.lineRange(prop)
	if !ok {
		return nil, fmt.Sprintf("%s shares its lines with other code; put it on lines of its own", field)
	}
	ma, _, _ := l.lineRange(members[0])
	_, mb, _ := l.lineRange(members[len(members)-1])
	from, to := l.indent(l.line(l.pos(members[0]))), l.indent(l.line(l.pos(prop)))
	edits = append(edits, Edit{File: l.f.FileName(), Start: pa, End: pb, NewText: l.reindent(ma, mb, from, to)})
	sa, sb, ok := l.lineRange(s)
	if !ok {
		return nil, fmt.Sprintf("%s shares its lines with other code", name)
	}
	sa, sb = l.withBlank(sa, sb)
	edits = append(edits, Edit{File: l.f.FileName(), Start: sa, End: sb, NewText: ""})
	return edits, ""
}
