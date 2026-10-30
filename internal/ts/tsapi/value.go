package tsapi

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/checker"
)

// WriteValue returns the edits that write value, an expression in the
// terms of the body of the function called name (this, its other
// parameters), for each use of its parameter param there: the first step
// of removing a parameter that every call passes value for.
func (p *Project) WriteValue(ctx context.Context, path, name, param, value string) ([]Edit, string) {
	t, done, why := p.target(ctx, path, name)
	if why != "" {
		return nil, why
	}
	defer done()
	l, c, fn := t.l, t.c, t.fn
	i := t.index(param)
	switch {
	case fn.Body() == nil:
		return nil, fmt.Sprintf("%s has no body to write value in", name)
	case len(t.family) > 0:
		return nil, fmt.Sprintf("value is not supported for a method that changes with others (%s) yet; remove the uses of %s in each body first", memberName(t.family[0]), param)
	case i < 0:
		return nil, fmt.Sprintf("%s has no parameter %s", name, param)
	case t.isRest(i):
		return nil, fmt.Sprintf("%s is a rest parameter, so no one expression is what every call passes", param)
	}
	v, why := parseExpression(value)
	if why != "" {
		return nil, strings.Replace(why, "new is", "value is", 1)
	}
	mentions := false
	identifiers(v, func(id *ast.Node) {
		named := id.Parent != nil && (ast.IsPropertyAccessExpression(id.Parent) && id.Parent.Name() == id || ast.IsPropertyAssignment(id.Parent) && id.Parent.Name() == id)
		mentions = mentions || id.Text() == param && !named
	})
	if mentions {
		return nil, fmt.Sprintf("value %s uses %s itself; give the expression every call passes in terms of this and the other parameters", strings.TrimSpace(value), param)
	}
	text := operand(newLined(ast.GetSourceFileOfNode(v)), v)
	sym := c.GetSymbolAtLocation(t.params[t.off+i].Name())
	if where := l.assigns(fn.Body(), sym, c); where != "" {
		return nil, fmt.Sprintf("the body of %s assigns %s (%s), so no expression can stand for it", name, param, where)
	}
	var edits []Edit
	identifiers(fn.Body(), func(id *ast.Node) {
		if symbolOf(c, id) != sym || ast.IsDeclarationName(id) && !ast.IsShorthandPropertyAssignment(id.Parent) {
			return
		}
		if ast.IsShorthandPropertyAssignment(id.Parent) {
			edits = append(edits, Edit{File: l.f.FileName(), Start: l.pos(id), End: id.End(), NewText: param + ": " + strings.TrimSpace(value)})
			return
		}
		edits = append(edits, Edit{File: l.f.FileName(), Start: l.pos(id), End: id.End(), NewText: text})
	})
	return edits, ""
}

// shown checks that removing the parameter ch.Remove, which every call is
// to pass ch.Value for, changes nothing: each call passes value in its own
// terms, and the body evaluates value where it does (outside an argument
// that goes with the parameter: of a call of the function, or of one
// named in ch.Also) to what it would at the call.
func (t *target) shown(ch SignatureChange, name string) string {
	l, c, fn := t.l, t.c, t.fn
	if len(t.family) > 0 {
		return fmt.Sprintf("value is not supported for a method that changes with others (%s) yet; remove the uses of %s in each body first", memberName(t.family[0]), ch.Remove)
	}
	dropped := map[*ast.Node]bool{}
	for _, n := range append([]string{name}, ch.Also...) {
		g, _, nn, why := l.function(n)
		if why != "" {
			continue // removed by an earlier step, or not a function of the file
		}
		gs := c.GetSymbolAtLocation(nn)
		params, off := g.Parameters(), 0
		if len(params) > 0 && ast.IsThisParameter(params[0]) {
			off = 1
		}
		at := slices.IndexFunc(params[off:], func(prm *ast.Node) bool {
			return prm.Name() != nil && ast.IsIdentifier(prm.Name()) && prm.Name().Text() == ch.Remove
		})
		if at < 0 || gs == nil {
			continue
		}
		var visit func(x *ast.Node) bool
		visit = func(x *ast.Node) bool {
			if ast.IsCallExpression(x) {
				callee := x.Expression()
				if ast.IsPropertyAccessExpression(callee) {
					callee = callee.Name()
				}
				if s := c.GetSymbolAtLocation(callee); s != nil && c.SkipAlias(s) == gs && at < len(x.Arguments()) {
					dropped[x.Arguments()[at]] = true
				}
			}
			return x.ForEachChild(visit)
		}
		fn.Body().ForEachChild(visit)
	}
	want := tokens(ch.Value)
	var found []*ast.Node
	var visit func(x *ast.Node) bool
	visit = func(x *ast.Node) bool {
		if dropped[x] {
			return false
		}
		if ast.IsExpressionNode(x) && !(ast.IsPropertyAccessExpression(x.Parent) && x.Parent.Name() == x) && slices.Equal(tokens(l.source(x)), want) {
			found = append(found, x)
			return false
		}
		x.ForEachChild(visit)
		return false
	}
	fn.Body().ForEachChild(visit)
	if len(found) == 0 {
		return ""
	}
	for _, o := range found {
		if written(o) {
			return fmt.Sprintf("the body of %s assigns %s (%s), so it does not keep the value the calls passed", name, strings.TrimSpace(ch.Value), l.where(o))
		}
	}
	for _, o := range found[1:] {
		if !slices.Equal(symbolsOf(c, o), symbolsOf(c, found[0])) {
			return fmt.Sprintf("%s means something else at %s than at %s", strings.TrimSpace(ch.Value), l.where(o), l.where(found[0]))
		}
	}
	texts, why := t.introduced(found[0], ch.Value, len(found), name)
	if why != "" {
		return why
	}
	i := t.index(ch.Remove)
	for k, call := range t.calls {
		cl := newLined(ast.GetSourceFileOfNode(call))
		args := call.Arguments()
		if i >= len(args) {
			return fmt.Sprintf("the call at %s does not pass %s, so it gets undefined, not %s", cl.where(call), ch.Remove, texts[k])
		}
		if got := cl.source(args[i]); !slices.Equal(unparenTokens(tokens(got)), unparenTokens(tokens(texts[k]))) {
			return fmt.Sprintf("the call at %s passes %s, not %s; change what it passes first, or, if its function passes on a parameter of its own, name that function in also", cl.where(call), got, texts[k])
		}
	}
	return ""
}

// unparenTokens drops the parentheses around a whole expression's tokens.
func unparenTokens(ts []string) []string {
	for len(ts) >= 2 && ts[0] == "(" && ts[len(ts)-1] == ")" {
		depth := 0
		for i, tk := range ts {
			switch tk {
			case "(":
				depth++
			case ")":
				depth--
			}
			if depth == 0 && i < len(ts)-1 {
				return ts
			}
		}
		ts = ts[1 : len(ts)-1]
	}
	return ts
}

// written reports whether the expression o is assigned to, or
// incremented or decremented.
func written(o *ast.Node) bool {
	par := o.Parent
	return ast.IsBinaryExpression(par) && ast.IsAssignmentOperator(par.AsBinaryExpression().OperatorToken.Kind) && par.AsBinaryExpression().Left == o ||
		ast.IsPostfixUnaryExpression(par) || ast.IsPrefixUnaryExpression(par) && (par.AsPrefixUnaryExpression().Operator == ast.KindPlusPlusToken || par.AsPrefixUnaryExpression().Operator == ast.KindMinusMinusToken)
}

// hidden returns the variable at the top level of a module that body
// reads by name, which a parameter so named would take the place of; or
// nil, with where body reads another of that name (a local, a function,
// an import of something else), or "" when it reads none.
func (l *lined) hidden(c *checker.Checker, body *ast.Node, name string) (*ast.Symbol, string) {
	var sym *ast.Symbol
	where := ""
	identifiers(body, func(id *ast.Node) {
		if where != "" || id.Text() != name || id.Parent != nil && (ast.IsPropertyAccessExpression(id.Parent) && id.Parent.Name() == id || ast.IsPropertyAssignment(id.Parent) && id.Parent.Name() == id) {
			return
		}
		s := c.GetSymbolAtLocation(id)
		if s == nil {
			return
		}
		s = c.SkipAlias(s)
		if d := s.ValueDeclaration; d != nil && ast.IsVariableDeclaration(d) && d.Parent != nil && d.Parent.Parent != nil && ast.IsVariableStatement(d.Parent.Parent) && ast.IsSourceFile(d.Parent.Parent.Parent) && (sym == nil || sym == s) {
			sym = s
			return
		}
		where = l.where(id)
	})
	if where != "" {
		return nil, where
	}
	return sym, ""
}

// hides checks the name of the parameter add against what the bodies of
// t and its family read: a parameter so named would hide it. With
// add.ReplaceVariable, a variable at the top level of a module may be
// hidden, if it has the parameter's type and no body assigns it; the
// parameter then takes its place.
func (t *target) hides(add *NewParameter, name string) string {
	c := t.c
	for _, m := range append([]*ast.Node{t.fn}, t.family...) {
		if m.Body() == nil {
			continue
		}
		ml := newLined(ast.GetSourceFileOfNode(m))
		owner := name
		if m != t.fn {
			owner = memberName(m) + ", which changes with " + name + ","
		}
		v, where := ml.hidden(c, m.Body(), add.Name)
		switch {
		case where != "":
			return fmt.Sprintf("the body of %s reads another %s (%s), which the parameter would hide; choose another name", owner, add.Name, where)
		case v == nil:
			continue
		case !add.ReplaceVariable:
			return fmt.Sprintf("the body of %s reads another %s (%s), which the parameter would hide; choose another name, or with replace_variable, the parameter takes its place", owner, add.Name, ml.reads(c, m.Body(), add.Name, nil))
		}
		vt := c.TypeToString(c.GetWidenedType(c.GetBaseTypeOfLiteralType(c.GetTypeOfSymbol(v))))
		if strings.Join(strings.Fields(vt), "") != strings.Join(strings.Fields(add.Type), "") {
			return fmt.Sprintf("the parameter's type %s is not the type %s of the variable %s it would replace", strings.TrimSpace(add.Type), vt, add.Name)
		}
		if where := ml.assigns(m.Body(), v, c); where != "" {
			return fmt.Sprintf("the body of %s assigns %s (%s); a parameter would take that write from the variable", owner, add.Name, where)
		}
	}
	return ""
}

// ReplacedNote says why adding the parameter param with value to the
// function called name, replacing the variable of that name its body
// reads, is not proven to keep behavior: the variable is assigned
// somewhere, or the calls pass another value. "" when it is proven, or
// the body reads no such variable.
func (p *Project) ReplacedNote(ctx context.Context, path, name, param, value string) string {
	t, done, why := p.target(ctx, path, name)
	if why != "" {
		return ""
	}
	defer done()
	c := t.c
	if t.fn.Body() == nil {
		return ""
	}
	v, _ := t.l.hidden(c, t.fn.Body(), param)
	if v == nil {
		return ""
	}
	for _, f := range p.prog.GetProgram().GetSourceFiles() {
		if f.IsDeclarationFile || strings.Contains(f.FileName(), "/node_modules/") {
			continue
		}
		fl := newLined(f)
		where := ""
		identifiers(f.AsNode(), func(id *ast.Node) {
			if where == "" && id.Text() == param && written(id) {
				if s := c.GetSymbolAtLocation(id); s != nil && c.SkipAlias(s) == v {
					where = fl.where(id)
				}
			}
		})
		if where != "" {
			return fmt.Sprintf("%s is assigned at %s, so no call is proven to pass what the body read: the step is an edit; check that each call passes the variable's value", param, where)
		}
	}
	if why := t.findCalls(p); why != "" || len(t.calls) == 0 {
		return ""
	}
	if s := c.ResolveName(strings.TrimSpace(value), t.calls[0], ast.SymbolFlagsValue, false); s == nil || c.SkipAlias(s) != v {
		return fmt.Sprintf("the body now reads the parameter %s where it read the variable: an edit, since every call passes %s, not %s; check that it holds what %s does", param, strings.TrimSpace(value), param, param)
	}
	return ""
}
