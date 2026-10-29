package inlinefunc

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
)

// replacement returns the caller file's edits that replace the call.
func (x *inliner) replacement() ([]edit.TextEdit, error) {
	if x.shape == expr {
		text, err := x.exprText()
		if err != nil {
			return nil, err
		}
		return []edit.TextEdit{{Start: x.Off(x.call.Pos()), End: x.Off(x.call.End()), NewText: text}}, nil
	}
	ind := indentOf(x.Src, x.TF, x.stmt.Pos())
	var lines []string
	if b, err := x.bindingLine(); err != nil {
		return nil, err
	} else if b != "" {
		lines = append(lines, ind+b)
	}
	var post []string
	if x.tails {
		pre, edits, after, err := x.tailResults()
		if err != nil {
			return nil, err
		}
		for _, p := range pre {
			lines = append(lines, ind+p)
		}
		x.bodyEdits = append(append(x.bodyEdits, x.tailEdits...), edits...)
		post = after
	}
	if body := x.bodyText(ind); body != "" {
		lines = append(lines, body)
	}
	for _, p := range post {
		lines = append(lines, ind+p)
	}
	res, err := x.resultLines()
	if err != nil {
		return nil, err
	}
	for _, r := range res {
		lines = append(lines, ind+r)
	}
	if len(lines) == 0 {
		return []edit.TextEdit{x.deleteLine(x.Src, x.TF, x.stmt)}, nil
	}
	text := strings.TrimPrefix(strings.Join(lines, "\n"), ind)
	return []edit.TextEdit{{Start: x.Off(x.stmt.Pos()), End: x.Off(x.stmt.End()), NewText: text}}, nil
}

// exprText is the result expression, with parameters replaced, that takes
// the call's place.
func (x *inliner) exprText() (string, error) {
	r := x.results[0]
	text := x.rewrite(x.callee.Off(r.Pos()), x.callee.Off(r.End()))
	if strings.Contains(text, "\n") {
		text = reindentTail(text, indentOf(x.callee.Src, x.callee.TF, r.Pos()), indentOf(x.Src, x.TF, x.call.Pos()))
	}
	want := x.sig.Results().At(0).Type()
	if !x.sameType(r, want) && !x.contextConverts(want, analysis.OwnType(x.WS.Fset, x.Pkg.Types, x.Info, r)) {
		t, err := x.typeString(want)
		if err != nil {
			return "", err
		}
		return convText(t, text), nil
	}
	// The shape that decides on parentheses: the argument, if the result
	// is just a parameter.
	var shape ast.Expr = ast.Unparen(r)
	if id, ok := shape.(*ast.Ident); ok {
		if p := x.paramOf(x.Info.Uses[id]); p != nil && p.subst {
			shape = ast.Unparen(p.arg)
			if p.conv != "" || p.addr != nil {
				shape = &ast.CallExpr{}
			}
		}
	}
	if analysis.NeedsParens(shape, x.path[1], x.call) || (strings.Contains(text, "{") && x.inCallerHeader()) {
		text = "(" + text + ")"
	}
	return text, nil
}

// contextConverts reports whether the call's context gives its value type
// want anyway (an argument for a parameter of that type, an assignment to
// a variable of that type, a return of that type), or, for a range clause,
// whether own ranges the same way, so the inlined expression (of type own)
// needs no conversion.
func (x *inliner) contextConverts(want, own types.Type) bool {
	is := func(t types.Type) bool { return t != nil && types.Identical(t, want) }
	switch p := x.path[1].(type) {
	case *ast.CallExpr:
		if tv, ok := x.Info.Types[p.Fun]; ok && tv.IsType() {
			return false
		}
		sig, ok := x.Info.TypeOf(p.Fun).Underlying().(*types.Signature)
		if !ok {
			return false
		}
		for i, a := range p.Args {
			if a == ast.Expr(x.call) && i < sig.Params().Len() && !(sig.Variadic() && i >= sig.Params().Len()-1) {
				return is(sig.Params().At(i).Type())
			}
		}
	case *ast.AssignStmt:
		if p.Tok == token.ASSIGN && len(p.Lhs) == len(p.Rhs) {
			for i, r := range p.Rhs {
				if r == ast.Expr(x.call) {
					return is(x.Info.TypeOf(p.Lhs[i]))
				}
			}
		}
	case *ast.ValueSpec:
		return p.Type != nil && is(x.Info.TypeOf(p.Type))
	case *ast.ReturnStmt:
		var sig *types.Signature
		for _, n := range x.path[1:] {
			if f, ok := n.(*ast.FuncLit); ok {
				sig, _ = x.Info.TypeOf(f).(*types.Signature)
				break
			}
			if f, ok := n.(*ast.FuncDecl); ok {
				if o, ok := x.Info.Defs[f.Name].(*types.Func); ok {
					sig = o.Signature()
				}
				break
			}
		}
		if sig != nil && sig.Results().Len() == len(p.Results) {
			for i, r := range p.Results {
				if r == ast.Expr(x.call) {
					return is(sig.Results().At(i).Type())
				}
			}
		}
	case *ast.RangeStmt:
		b, untyped := own.(*types.Basic)
		return p.X == ast.Expr(x.call) && !(untyped && b.Info()&types.IsUntyped != 0) &&
			types.Identical(own.Underlying(), want.Underlying())
	}
	return false
}

func (x *inliner) inCallerHeader() bool {
	return analysis.InControlHeader(&ast.CompositeLit{}, x.path)
}

// sameType reports whether the callee's expression e has type want on its
// own (so no conversion is needed where its context is not want).
func (x *inliner) sameType(e ast.Expr, want types.Type) bool {
	own := analysis.OwnType(x.WS.Fset, x.Pkg.Types, x.Info, e)
	if types.Identical(own, want) {
		return true
	}
	if b, ok := own.(*types.Basic); ok && b.Info()&types.IsUntyped != 0 && b.Kind() != types.UntypedNil {
		return types.Identical(types.Default(own), want)
	}
	return false
}

// bindingLine declares the parameters that stay variables.
func (x *inliner) bindingLine() (string, error) {
	var names, vals []string
	isNew := false
	tuple := len(x.call.Args) == 1 && x.sig.Params().Len() > 1
	for _, p := range x.params {
		if !p.bind {
			continue
		}
		n := p.name
		if len(p.uses) == 0 {
			n = "_"
		}
		isNew = isNew || n != "_"
		names = append(names, n)
		if p.arg == nil {
			continue // fed by the multi-value call
		}
		v := p.text
		if p.conv != "" {
			v = convText(p.conv, x.Text(ast.Unparen(p.arg)))
		}
		vals = append(vals, v)
	}
	if tuple {
		vals = append(vals, x.Text(x.call.Args[0]))
	}
	if len(names) == 0 {
		return "", nil
	}
	tok := " := "
	if !isNew {
		tok = " = "
	}
	return strings.Join(names, ", ") + tok + strings.Join(vals, ", "), nil
}

// bodyText returns the body's statements, with parameters replaced and
// (outside the terminal shape) the final return removed, indented with ind.
func (x *inliner) bodyText(ind string) string {
	lb, rb := x.callee.Off(x.decl.Body.Lbrace)+1, x.callee.Off(x.decl.Body.Rbrace)
	extra := append([]edit.TextEdit(nil), x.bodyEdits...)
	if x.ret != nil {
		extra = append(extra, x.deleteLine(x.callee.Src, x.callee.TF, x.ret))
	}
	text := x.rewrite(lb, rb, extra...)
	from := ""
	if i := strings.IndexByte(text, '\n'); i >= 0 && strings.TrimSpace(text[:i]) == "" {
		text = text[i+1:]
	} else {
		text = strings.TrimLeft(text, " \t")
	}
	text = strings.TrimRight(text, " \t\n")
	for strings.HasPrefix(strings.TrimLeft(text, " \t"), "\n") {
		text = strings.TrimLeft(text, " \t")[1:]
	}
	if text == "" {
		return ""
	}
	if from == "" {
		from = text[:len(text)-len(strings.TrimLeft(text, " \t"))]
	}
	return textutil.Reindent(text, from, ind)
}

// resultLines assigns (or, for a call statement, evaluates) the values the
// body returns.
func (x *inliner) resultLines() ([]string, error) {
	if x.ret == nil || x.shape == terminal || len(x.results) == 0 {
		return nil, nil
	}
	nres := x.sig.Results().Len()
	tuple := len(x.results) == 1 && nres > 1
	if x.shape == stmtCall {
		var out []string
		for _, r := range x.results {
			if x.droppable(r) {
				continue
			}
			t := x.rewrite(x.callee.Off(r.Pos()), x.callee.Off(r.End()))
			if c, ok := ast.Unparen(r).(*ast.CallExpr); ok && !x.isConversionOrBuiltin(c) && !x.resultMatters(c) {
				out = append(out, t)
			} else {
				out = append(out, "_ = "+t)
			}
		}
		return out, nil
	}
	if ds, ok := x.stmt.(*ast.DeclStmt); ok {
		spec := ds.Decl.(*ast.GenDecl).Specs[0].(*ast.ValueSpec)
		var vals []string
		for i, r := range x.results {
			t := x.rewrite(x.callee.Off(r.Pos()), x.callee.Off(r.End()))
			if !tuple && spec.Type == nil && !x.sameType(r, x.sig.Results().At(i).Type()) {
				s, err := x.typeString(x.sig.Results().At(i).Type())
				if err != nil {
					return nil, err
				}
				t = convText(s, t)
			}
			vals = append(vals, t)
		}
		head := string(x.Src[x.Off(ds.Pos()):x.Off(x.call.Pos())])
		tail := string(x.Src[x.Off(x.call.End()):x.Off(ds.End())])
		return []string{head + strings.Join(vals, ", ") + tail}, nil
	}
	as := x.stmt.(*ast.AssignStmt)
	if tuple {
		lhs := string(x.Src[x.Off(as.Lhs[0].Pos()):x.Off(as.Lhs[len(as.Lhs)-1].End())])
		r := x.results[0]
		return []string{lhs + " " + as.Tok.String() + " " + x.rewrite(x.callee.Off(r.Pos()), x.callee.Off(r.End()))}, nil
	}
	var lhs, rhs []string
	isNew := false
	for i, l := range as.Lhs {
		r := x.results[i]
		if x.dropped[i] || x.selfAssign(l, r) {
			continue
		}
		if id, ok := l.(*ast.Ident); ok && id.Name == "_" && x.droppable(r) {
			continue
		}
		t := x.rewrite(x.callee.Off(r.Pos()), x.callee.Off(r.End()))
		want := x.sig.Results().At(i).Type()
		newVar := false
		if id, ok := l.(*ast.Ident); ok && as.Tok == token.DEFINE && x.Info.Defs[id] != nil {
			newVar = true
		}
		if !x.sameType(r, want) && (newVar || !types.Identical(x.Info.TypeOf(l), want)) {
			s, err := x.typeString(want)
			if err != nil {
				return nil, err
			}
			t = convText(s, t)
		}
		isNew = isNew || newVar
		lhs = append(lhs, x.Text(l))
		rhs = append(rhs, t)
	}
	if len(lhs) == 0 {
		return nil, nil
	}
	tok := as.Tok
	if tok == token.DEFINE && !isNew {
		tok = token.ASSIGN
	}
	return []string{strings.Join(lhs, ", ") + " " + tok.String() + " " + strings.Join(rhs, ", ")}, nil
}

// droppable reports whether a result nobody receives can be left out: it
// has no effects, and every variable of the inlined code it reads is used
// elsewhere too (else that variable would be declared and not used).
func (x *inliner) droppable(r ast.Expr) bool {
	if m := (analysis.Move{Effects: x.WS.Effects(), Func: x.calleeFA, Expr: r, Times: analysis.Unused}); m.Check() != nil {
		return false
	}
	for _, id := range analysis.ScopeRefs(x.Info, r) {
		v, ok := x.Info.Uses[id].(*types.Var)
		if !ok || !x.inDecl(v.Pos()) {
			continue
		}
		if q := x.paramOf(v); q != nil && !q.bind {
			continue // replaced by its argument: nothing is declared
		}
		used := false
		for u, o := range x.Info.Uses {
			if o == v && x.inDecl(u.Pos()) && (u.Pos() < x.ret.Pos() || u.Pos() >= x.ret.End()) {
				used = true
				break
			}
		}
		if !used {
			return false
		}
	}
	return true
}

// selfAssign reports whether assigning the body's result r to l would
// assign a variable to itself: r is a parameter replaced by l.
func (x *inliner) selfAssign(l, r ast.Expr) bool {
	rid, ok := ast.Unparen(r).(*ast.Ident)
	if !ok {
		return false
	}
	p := x.paramOf(x.Info.Uses[rid])
	if p == nil || !p.subst || p.addr != nil || p.conv != "" {
		return false
	}
	aid, ok := ast.Unparen(p.arg).(*ast.Ident)
	lid, ok2 := l.(*ast.Ident)
	return ok && ok2 && x.Info.ObjectOf(aid) != nil && x.Info.ObjectOf(aid) == x.Info.ObjectOf(lid)
}

// rewrite returns the callee's source in [start, end) (byte offsets) with
// parameters replaced by their arguments, merged locals renamed, package
// names changed to the caller file's, and extra edits applied.
func (x *inliner) rewrite(start, end int, extra ...edit.TextEdit) string {
	edits := append([]edit.TextEdit(nil), extra...)
	in := func(n ast.Node) bool {
		s, e := x.callee.Off(n.Pos()), x.callee.Off(n.End())
		for _, d := range extra { // already replaced
			if d.Start <= s && e <= d.End {
				return false
			}
		}
		return s >= start && e <= end
	}
	for _, p := range x.params {
		if !p.subst {
			continue
		}
		for _, u := range p.uses {
			if in(u) {
				edits = append(edits, x.useEdit(p, u))
			}
		}
	}
	ast.Inspect(x.decl.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || !in(id) {
			return true
		}
		if v, ok := x.Info.ObjectOf(id).(*types.Var); ok && x.merged[v] != nil && x.merged[v].Name != id.Name {
			edits = append(edits, edit.TextEdit{Start: x.callee.Off(id.Pos()), End: x.callee.Off(id.End()), NewText: x.merged[v].Name})
		}
		if pn, ok := x.Info.Uses[id].(*types.PkgName); ok {
			if n, err := x.qualifier(pn.Imported()); err == nil && n != id.Name && n != "" {
				edits = append(edits, edit.TextEdit{Start: x.callee.Off(id.Pos()), End: x.callee.Off(id.End()), NewText: n})
			}
		}
		return true
	})
	for i := range edits {
		edits[i].Start -= start
		edits[i].End -= start
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].Start < edits[j].Start })
	out, err := edit.Apply(x.callee.Src[start:end], edits)
	if err != nil {
		return string(x.callee.Src[start:end]) // overlapping edits would be a bug; the type check catches it
	}
	return string(out)
}

// useEdit replaces one use of a substituted parameter.
func (x *inliner) useEdit(p *param, u *ast.Ident) edit.TextEdit {
	path := analysis.PathTo(x.decl, u)
	parent := path[1]
	at := func(n ast.Node, s string) edit.TextEdit {
		return edit.TextEdit{Start: x.callee.Off(n.Pos()), End: x.callee.Off(n.End()), NewText: s}
	}
	if p.addr != nil {
		v := x.Text(p.addr)
		switch par := parent.(type) {
		case *ast.SelectorExpr, *ast.IndexExpr, *ast.SliceExpr:
			return at(u, v) // p.f, p[i], p[:] work on the variable itself
		case *ast.StarExpr:
			// *p is the variable; (*p) too.
			var n ast.Node = par
			for i := 3; i < len(path); i++ {
				if _, ok := path[i-1].(*ast.ParenExpr); !ok {
					break
				}
				n = path[i-1]
			}
			return at(n, v)
		}
		s := "&" + v
		if analysis.NeedsParens(&ast.UnaryExpr{Op: token.AND}, parent, u) {
			s = "(" + s + ")"
		}
		return at(u, s)
	}
	if p.conv != "" {
		return at(u, convText(p.conv, x.Text(ast.Unparen(p.arg))))
	}
	s := p.text
	if analysis.NeedsParens(ast.Unparen(p.arg), parent, u) || analysis.InControlHeader(p.arg, path) {
		s = "(" + s + ")"
	}
	return at(u, s)
}

// resultMatters reports whether calling c only for its effects could be
// flagged by go vet's unusedresult check: calls into the standard library
// (fmt.Sprintf, errors.New, ...) and String or Error methods. Those keep
// "_ =".
func (x *inliner) resultMatters(c *ast.CallExpr) bool {
	f, ok := x.Info.Uses[analysis.IdentOf(c.Fun)].(*types.Func)
	if !ok || f.Pkg() == nil {
		return false
	}
	if f.Name() == "String" || f.Name() == "Error" {
		return true
	}
	return !strings.Contains(f.Pkg().Path(), ".") && f.Pkg() != x.Pkg.Types
}

func (x *inliner) isConversionOrBuiltin(c *ast.CallExpr) bool {
	if tv, ok := x.Info.Types[c.Fun]; ok && tv.IsType() {
		return true
	}
	_, builtin := x.Info.Uses[analysis.IdentOf(c.Fun)].(*types.Builtin)
	return builtin
}

// deleteLine removes statement s, with its line if it is alone on it.
func (x *inliner) deleteLine(src []byte, tf *token.File, s ast.Node) edit.TextEdit {
	start, end := tf.Offset(s.Pos()), tf.Offset(s.End())
	ls := start
	for ls > 0 && (src[ls-1] == ' ' || src[ls-1] == '\t') {
		ls--
	}
	rest := end
	for rest < len(src) && (src[rest] == ' ' || src[rest] == '\t') {
		rest++
	}
	if strings.HasPrefix(string(src[rest:]), "//") {
		for rest < len(src) && src[rest] != '\n' {
			rest++
		}
	}
	if (ls == 0 || src[ls-1] == '\n') && (rest == len(src) || src[rest] == '\n') {
		return edit.TextEdit{Start: ls, End: min(rest+1, len(src))}
	}
	if rest < len(src) && src[rest] == ';' {
		return edit.TextEdit{Start: start, End: rest + 1}
	}
	return edit.TextEdit{Start: start, End: end}
}

func convText(t, s string) string {
	if strings.HasPrefix(t, "*") || strings.HasPrefix(t, "<-") || strings.HasPrefix(t, "func") || strings.HasPrefix(t, "chan") {
		t = "(" + t + ")"
	}
	return t + "(" + strings.TrimSpace(s) + ")"
}

// reindentTail re-indents every line of text but the first.
func reindentTail(text, from, to string) string {
	i := strings.IndexByte(text, '\n')
	return text[:i+1] + textutil.Reindent(text[i+1:], from, to)
}

func (x *inliner) parenText(e ast.Expr) string {
	if analysis.NeedsParens(ast.Unparen(e), &ast.StarExpr{}, e) {
		return "(" + x.Text(e) + ")"
	}
	return x.Text(e)
}
