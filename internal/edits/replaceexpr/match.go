package replaceexpr

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/site"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// sameMeaning returns the expressions of file that mean what expr does,
// in source order: the same tokens, with every package-level name, import,
// field and method referring to the same object, and every local variable
// of the same type (so c.Body matches c.Body in another method of the same
// type, but not d.Body of another type).
func sameMeaning(fset *token.FileSet, info *types.Info, file *ast.File, src []byte, expr ast.Expr) []ast.Expr {
	tf := fset.File(file.Pos())
	want, err := locate.Tokens(src[tf.Offset(expr.Pos()):tf.Offset(expr.End())])
	if err != nil {
		return []ast.Expr{expr}
	}
	names := idents(expr)
	var out []ast.Expr
	ast.Inspect(file, func(n ast.Node) bool {
		e, ok := n.(ast.Expr)
		if !ok || !locate.TokensMatch(tf, src, e, want) {
			return true
		}
		if e != expr && !sameNames(fset, info, names, idents(e)) {
			return true
		}
		out = append(out, e)
		return false // an occurrence inside a match is part of it
	})
	return out
}

// idents lists every name in e, selected ones included, in order.
func idents(e ast.Expr) []*ast.Ident {
	var out []*ast.Ident
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			out = append(out, id)
		}
		return true
	})
	return out
}

// sameNames reports whether two lists of names (of expressions with the
// same tokens) mean the same.
func sameNames(fset *token.FileSet, info *types.Info, a, b []*ast.Ident) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !sameObject(fset, info.ObjectOf(a[i]), info.ObjectOf(b[i])) {
			return false
		}
	}
	return true
}

// isLocal reports whether o is a variable declared in a function.
func isLocal(o types.Object) bool {
	v, ok := o.(*types.Var)
	return ok && !v.IsField() && v.Pkg() != nil && v.Parent() != v.Pkg().Scope()
}

// holeRE finds the holes of a pattern, $name, which holePrefix makes into
// names a pattern can be parsed with.
var holeRE = regexp.MustCompile(`\$([A-Za-z_]\w*)`)

const holePrefix = "__hole_"

// match is an expression with a pattern's shape: what each hole stands
// for, and the names that are not holes, in order, with the types.Info
// they are resolved in.
type match struct {
	expr  ast.Expr
	holes map[string]ast.Expr
	names []*ast.Ident
	info  *types.Info
}

// shapeOf returns e as a match of pattern, or nil: the same kinds of node,
// operators, literals and names, a hole standing for any expression (the
// same one each time it occurs).
func shapeOf(pattern, e ast.Expr, info *types.Info) *match {
	m := &match{expr: e, holes: map[string]ast.Expr{}, info: info}
	if !m.same(reflect.ValueOf(pattern), reflect.ValueOf(e)) {
		return nil
	}
	return m
}

// same reports whether the node c has the shape of the node p, binding
// p's holes.
func (m *match) same(p, c reflect.Value) bool {
	if id, ok := p.Interface().(*ast.Ident); ok && id != nil && strings.HasPrefix(id.Name, holePrefix) {
		ce, ok := c.Interface().(ast.Expr)
		if !ok || ce == nil || reflect.ValueOf(ce).IsNil() {
			return false
		}
		if prev, ok := m.holes[id.Name]; ok {
			return types.ExprString(prev) == types.ExprString(ce)
		}
		m.holes[id.Name] = ce
		return true
	}
	if p.Type() != c.Type() {
		return false
	}
	switch p.Kind() {
	case reflect.Interface, reflect.Pointer:
		if p.IsNil() || c.IsNil() {
			return p.IsNil() == c.IsNil()
		}
		if id, ok := c.Interface().(*ast.Ident); ok {
			if p.Interface().(*ast.Ident).Name != id.Name {
				return false
			}
			m.names = append(m.names, id)
			return true
		}
		return m.same(p.Elem(), c.Elem())
	case reflect.Struct:
		for i := 0; i < p.NumField(); i++ {
			switch p.Type().Field(i).Type {
			case reflect.TypeOf(token.NoPos), reflect.TypeOf((*ast.Object)(nil)), reflect.TypeOf((*ast.Scope)(nil)), reflect.TypeOf((*ast.CommentGroup)(nil)):
				continue
			}
			if !m.same(p.Field(i), c.Field(i)) {
				return false
			}
		}
		return true
	case reflect.Slice:
		if p.Len() != c.Len() {
			return false
		}
		for i := 0; i < p.Len(); i++ {
			if !m.same(p.Index(i), c.Index(i)) {
				return false
			}
		}
		return true
	}
	return p.Interface() == c.Interface()
}

// sameAs reports whether m means what the anchor a does: its names refer
// to the same objects (a local, to one of the same type) and each hole
// stands for an expression of the type it has in a.
func (m *match) sameAs(fset *token.FileSet, a *match) bool {
	if len(m.names) != len(a.names) {
		return false
	}
	for i := range a.names {
		if !sameObject(fset, a.info.ObjectOf(a.names[i]), m.info.ObjectOf(m.names[i])) {
			return false
		}
	}
	for h, e := range a.holes {
		ta, tm := a.info.TypeOf(e), m.info.TypeOf(m.holes[h])
		if ta == nil || tm == nil || !types.Identical(ta, tm) {
			return false
		}
	}
	return true
}

// holeNames lists the holes of e, as $name, in order.
func holeNames(e ast.Expr) []string {
	var out []string
	for _, id := range idents(e) {
		if strings.HasPrefix(id.Name, holePrefix) {
			out = append(out, "$"+strings.TrimPrefix(id.Name, holePrefix))
		}
	}
	return out
}

// moduleFiles lists the Go files of the workspace's packages, each once.
func moduleFiles(ws *workspace.Workspace) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range ws.Pkgs {
		for _, f := range p.Syntax {
			if name := ws.Fset.Position(f.Pos()).Filename; !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// planPattern is replace_expression with $name holes, or everywhere: the
// occurrence at line binds each hole's type and each other name's object,
// and every expression of the same shape, names and hole types is
// replaced (with all_occurrences, in file; with everywhere, in the
// module; else the one at line) by new, its holes filled with the source
// each matched.
func planPattern(ws *workspace.Workspace, a args) (*refactor.Plan, error) {
	pattern, err := parsePattern(holeRE.ReplaceAllString(a.Expression, holePrefix+"$1"))
	if err != nil {
		return nil, refactor.Refusef("expression is not a Go expression: %v", err)
	}
	newText := holeRE.ReplaceAllString(strings.TrimSpace(a.New), holePrefix+"$1")
	newExpr, err := parsePattern(newText)
	if err != nil {
		return nil, refactor.Refusef("new is not a Go expression: %v", err)
	}
	if holes, used := holeNames(pattern), holeNames(newExpr); !slices.Equal(holes, used) {
		return nil, refactor.Refusef("new uses the holes %v, expression %v: each hole must appear in new once, in expression's order, so that what it stands for is evaluated as before", used, holes)
	}
	codeText := holeRE.ReplaceAllString(a.New, "nil") // a hole needs no import
	if _, ok := newExpr.(*ast.KeyValueExpr); ok {
		codeText = "_{" + codeText + "}"
	}
	code, err := gocode.ParseStatements("_ = " + codeText)
	if err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	opened, err := site.At(ws, path)
	if err != nil {
		return nil, err
	}
	tf := ws.Fset.File(opened.File.Pos())
	var anchor *match
	ast.Inspect(opened.File, func(n ast.Node) bool {
		e, ok := n.(ast.Expr)
		if anchor != nil || !ok || tf.Line(e.Pos()) != a.Line || (a.Column != 0 && tf.Position(e.Pos()).Column != a.Column) {
			return anchor == nil
		}
		anchor = shapeOf(pattern, e, opened.Pkg.TypesInfo)
		return anchor == nil
	})
	if anchor == nil {
		return nil, refactor.Refusef("nothing on %s:%d has the shape of %s", a.File, a.Line, a.Expression)
	}
	files := []string{path}
	if a.Everywhere {
		files = moduleFiles(ws)
	}
	var fes []edit.FileEdit
	var added []string
	n := 0
	for _, f := range files {
		opened, err := site.At(ws, f)
		if err != nil {
			return nil, err
		}
		var targets []*match
		ast.Inspect(opened.File, func(node ast.Node) bool {
			e, ok := node.(ast.Expr)
			if !ok {
				return true
			}
			m := shapeOf(pattern, e, opened.Pkg.TypesInfo)
			if m == nil || !m.sameAs(ws.Fset, anchor) || (!a.Everywhere && !a.AllOccurrences && e != anchor.expr) {
				return true
			}
			targets = append(targets, m)
			return false // an occurrence inside a match is part of it
		})
		if len(targets) == 0 {
			continue
		}
		adder := imports.New(ws, opened.Pkg, opened.File, opened.Src)
		add, err := gocode.AddImports(ws, opened.Pkg, opened.File, adder, targets[0].expr.Pos(), code, a.Imports)
		if err != nil {
			return nil, err
		}
		added = append(added, add...)
		ftf := ws.Fset.File(opened.File.Pos())
		var es []edit.TextEdit
		for _, t := range targets {
			text := holeRE.ReplaceAllStringFunc(strings.TrimSpace(a.New), func(h string) string {
				b := t.holes[holePrefix+h[1:]]
				s := string(opened.Src[ftf.Offset(b.Pos()):ftf.Offset(b.End())])
				switch b.(type) {
				case *ast.Ident, *ast.SelectorExpr, *ast.CallExpr, *ast.IndexExpr, *ast.ParenExpr, *ast.BasicLit, *ast.CompositeLit:
					return s
				}
				return "(" + s + ")"
			})
			if kv, ok := t.expr.(*ast.KeyValueExpr); ok {
				if lit, ok := analysis.PathTo(opened.File, kv)[1].(*ast.CompositeLit); ok {
					if m, ok := merged(ftf, lit, kv, text); ok {
						es = append(es, m...)
						continue
					}
				}
			}
			text = fitted(opened.File, t.expr, newExpr, text)
			es = append(es, edit.TextEdit{Start: ftf.Offset(t.expr.Pos()), End: ftf.Offset(t.expr.End()), NewText: text})
		}
		n += len(targets)
		fes = append(fes, edit.FileEdit{Path: f, Edits: append(es, adder.Edits()...)})
	}
	plan := &refactor.Plan{Summary: fmt.Sprintf("replaced %d occurrence%s of %s with %s in %d file%s", n, textutil.Plural(n), strings.TrimSpace(a.Expression), strings.TrimSpace(a.New), len(fes), textutil.Plural(len(fes))), Edits: fes}
	if len(added) > 0 {
		plan.Notes = append(plan.Notes, gocode.ImportNote("added", added))
	}
	return plan, nil
}

// sameObject reports whether two names' objects mean the same: both
// unresolved, two locals of one type, or one declared object.
func sameObject(fset *token.FileSet, oa, ob types.Object) bool {
	switch {
	case oa == nil || ob == nil:
		return (oa == nil) == (ob == nil)
	case isLocal(oa) && isLocal(ob):
		return types.Identical(oa.Type(), ob.Type())
	}
	return analysis.ObjectKey(fset, oa) == analysis.ObjectKey(fset, ob)
}

// parsePattern parses text as an expression, or else as a literal's
// element (Road: $v), which is not one.
func parsePattern(text string) (ast.Expr, error) {
	e, err := parser.ParseExpr(text)
	if err == nil {
		return e, nil
	}
	if lit, ok := parseLit("_{" + text + "}"); ok && len(lit.Elts) == 1 {
		if kv, ok := lit.Elts[0].(*ast.KeyValueExpr); ok {
			return kv, nil
		}
	}
	return nil, err
}

// parseLit parses text as a composite literal.
func parseLit(text string) (*ast.CompositeLit, bool) {
	e, err := parser.ParseExpr(text)
	if err != nil {
		return nil, false
	}
	lit, ok := e.(*ast.CompositeLit)
	return lit, ok
}

// merged returns the edits that put text, the element replacing kv, into
// the element of the same key that kv's literal already has: the new
// value's elements go after that value's, and kv is deleted. ok is false
// when the literal has no such element, or either value is not a
// composite literal.
func merged(tf *token.File, lit *ast.CompositeLit, kv *ast.KeyValueExpr, text string) (_ []edit.TextEdit, ok bool) {
	wrapped := "_{" + text + "}"
	outer, ok := parseLit(wrapped)
	if !ok || len(outer.Elts) != 1 {
		return nil, false
	}
	nkv, ok := outer.Elts[0].(*ast.KeyValueExpr)
	if !ok {
		return nil, false
	}
	key, ok := nkv.Key.(*ast.Ident)
	nv, isLit := nkv.Value.(*ast.CompositeLit)
	if !ok || !isLit {
		return nil, false
	}
	var into *ast.CompositeLit
	for _, e := range lit.Elts {
		if o, ok := e.(*ast.KeyValueExpr); ok && o != kv {
			if k, ok := o.Key.(*ast.Ident); ok && k.Name == key.Name {
				into, _ = o.Value.(*ast.CompositeLit)
			}
		}
	}
	if into == nil {
		return nil, false
	}
	var elts []string
	for _, e := range nv.Elts {
		elts = append(elts, wrapped[e.Pos()-1:e.End()-1])
	}
	add, at := strings.Join(elts, ", "), tf.Offset(into.Lbrace)+1
	if n := len(into.Elts); n > 0 {
		add, at = ", "+add, tf.Offset(into.Elts[n-1].End())
	}
	// kv goes with the separator before it, or after it when it is first.
	i := slices.Index(lit.Elts, ast.Expr(kv))
	start, end := tf.Offset(kv.Pos()), tf.Offset(kv.End())
	if i > 0 {
		start = tf.Offset(lit.Elts[i-1].End())
	} else if len(lit.Elts) > 1 {
		end = tf.Offset(lit.Elts[1].Pos())
	}
	return []edit.TextEdit{{Start: at, End: at, NewText: add}, {Start: start, End: end}}, true
}

// isElement reports whether text is a literal's element (Road: "nw").
func isElement(text string) bool {
	e, _ := parsePattern(text)
	_, ok := e.(*ast.KeyValueExpr)
	return ok
}
