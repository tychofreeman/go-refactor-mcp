package tsapi

import (
	"context"
	"fmt"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/scanner"
)

// ParamsToObject returns the edits that bundle the parameters of the
// function or method called name into one object, destructured so that
// the body keeps its names, and pass an object literal at every call:
// water(bed: string, liters = 2) becomes water({ bed, liters = 2 }: { bed:
// string; liters?: number }), and water("roses", 3) water({ bed: "roses",
// liters: 3 }). With typeName, the object's type is an interface declared
// before the function. The object defaults to {} when every field is
// optional.
func (p *Project) ParamsToObject(ctx context.Context, path, name, typeName string) ([]Edit, string) {
	t, done, why := p.target(ctx, path, name)
	if why != "" {
		return nil, why
	}
	defer done()
	l, c, fn := t.l, t.c, t.fn
	params := t.params[t.off:]
	if len(params) == 0 {
		return nil, fmt.Sprintf("%s has no parameters to bundle", name)
	}
	if typeName != "" && len(t.overloads) > 0 {
		return nil, fmt.Sprintf("%s has overloads, whose objects would have different types; leave out type_name", name)
	}
	if typeName != "" && !scanner.IsIdentifierText(typeName, 0) {
		return nil, fmt.Sprintf("%q is not a valid type name", typeName)
	}
	if where := l.reads(c, fn.Body(), "arguments", nil); where != "" {
		return nil, fmt.Sprintf("the body of %s reads arguments (%s), which would hold one object", name, where)
	}
	var binds, fields []string
	allOptional := true
	for _, prm := range params {
		d := prm.AsParameterDeclaration()
		nm := prm.Name()
		switch {
		case nm == nil || !ast.IsIdentifier(nm):
			return nil, fmt.Sprintf("%s already destructures a parameter (%s)", name, l.source(prm))
		case d.DotDotDotToken != nil:
			return nil, fmt.Sprintf("%s has a rest parameter (%s), which an object cannot hold in order", name, l.source(prm))
		case prm.Modifiers() != nil && len(prm.Modifiers().Nodes) > 0:
			return nil, fmt.Sprintf("%s is a parameter property, which declares a field of the class too", nm.Text())
		}
		typ := ""
		if d.Type != nil {
			typ = l.source(d.Type)
		} else {
			typ = c.TypeToString(c.GetWidenedType(c.GetTypeAtLocation(nm)))
		}
		optional := d.QuestionToken != nil || d.Initializer != nil
		bind := nm.Text()
		if d.Initializer != nil {
			bind += " = " + l.source(d.Initializer)
		}
		binds = append(binds, bind)
		mark := ""
		if optional {
			mark = "?"
		} else {
			allOptional = false
		}
		fields = append(fields, fmt.Sprintf("%s%s: %s", nm.Text(), mark, typ))
	}
	if why := t.findCalls(p); why != "" {
		return nil, why
	}
	objType := "{ " + strings.Join(fields, "; ") + " }"
	if typeName != "" {
		objType = typeName
	}
	param := "{ " + strings.Join(binds, ", ") + " }: " + objType
	if allOptional {
		param += " = {}"
	}
	edits := []Edit{{File: l.f.FileName(), Start: l.pos(params[0]), End: params[len(params)-1].End(), NewText: param}}
	// Each overload takes an object of its own type, keyed by the
	// implementation's names.
	for _, o := range t.overloads {
		list, ooff := passed(o)
		ops := list.Nodes[ooff:]
		if len(ops) == 0 {
			continue
		}
		if len(ops) > len(params) {
			return nil, fmt.Sprintf("the overload at %s has more parameters than the implementation", l.where(o))
		}
		var keys, types []string
		optional := true
		for i, op := range ops {
			d := op.AsParameterDeclaration()
			if d.DotDotDotToken != nil || op.Name() == nil || !ast.IsIdentifier(op.Name()) {
				return nil, fmt.Sprintf("the overload at %s has a rest or destructured parameter (%s)", l.where(o), l.source(op))
			}
			key := params[i].Name().Text()
			mark := ""
			if d.QuestionToken != nil {
				mark = "?"
			} else {
				optional = false
			}
			typ := "any"
			if d.Type != nil {
				typ = l.source(d.Type)
			}
			keys = append(keys, key)
			types = append(types, fmt.Sprintf("%s%s: %s", key, mark, typ))
		}
		text := "{ " + strings.Join(keys, ", ") + " }: { " + strings.Join(types, "; ") + " }"
		if optional {
			text = "{ " + strings.Join(keys, ", ") + " }?: { " + strings.Join(types, "; ") + " }"
		}
		edits = append(edits, Edit{File: l.f.FileName(), Start: l.pos(ops[0]), End: ops[len(ops)-1].End(), NewText: text})
	}
	if typeName != "" {
		decl := fn
		if ast.IsArrowFunction(fn) || ast.IsFunctionExpression(fn) {
			for decl.Parent != nil && !ast.IsVariableStatement(decl) {
				decl = decl.Parent
			}
		}
		if ast.IsMethodDeclaration(decl) || ast.IsConstructorDeclaration(decl) {
			decl = decl.Parent // the class
		}
		export := ""
		if decl.ModifierFlags()&ast.ModifierFlagsExport != 0 {
			export = "export "
		}
		var lines []string
		for _, f := range fields {
			lines = append(lines, "  "+f+";")
		}
		start := l.start(l.line(scanner.GetTokenPosOfNode(decl, l.f, true)))
		edits = append(edits, Edit{File: l.f.FileName(), Start: start, End: start, NewText: fmt.Sprintf("%sinterface %s {\n%s\n}\n\n", export, typeName, strings.Join(lines, "\n"))})
	}
	for _, call := range t.calls {
		cl := newLined(ast.GetSourceFileOfNode(call))
		args := call.ArgumentList().Nodes
		if len(args) == 0 {
			if !allOptional {
				return nil, fmt.Sprintf("the call at %s passes no arguments, though %s has required ones", cl.where(call), name)
			}
			continue
		}
		var props []string
		for i, a := range args {
			if i >= len(params) {
				return nil, fmt.Sprintf("the call at %s passes more arguments than %s has parameters", cl.where(call), name)
			}
			key := params[i].Name().Text()
			if ast.IsIdentifier(a) && a.Text() == key {
				props = append(props, key)
			} else {
				props = append(props, key+": "+cl.source(a))
			}
		}
		edits = append(edits, Edit{File: cl.f.FileName(), Start: cl.pos(args[0]), End: args[len(args)-1].End(), NewText: "{ " + strings.Join(props, ", ") + " }"})
	}
	return edits, ""
}
