package inlinefunc

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
)

// existingMerge is a candidate for merging the body's local v into the
// caller's existing variable o, at result position i (assigned to l).
type existingMerge struct {
	i int
	v *types.Var
	l *ast.Ident
	o *types.Var
}

// canMergeExisting checks whether the body's local v, returned and
// assigned to the caller's existing variable o ("err, key = f(k)"), could
// become o itself, renamed if needed. That is what extract_function's
// "var key T" results came from. The caller's o must be a local nothing
// else can reach, and no argument may read it, since the body now changes
// it before the call would have returned.
func (x *inliner) canMergeExisting(v *types.Var, l *ast.Ident, o *types.Var) bool {
	if !x.callerFA.IsLocal(o) || x.callerFA.Escapes(o) || !types.Identical(v.Type(), o.Type()) {
		return false
	}
	if l.Name != v.Name() && !x.nameFreeInCallee(l.Name) {
		return false
	}
	d, _ := x.topDecl(v)
	return d != nil && !x.argsRead(o)
}

// mergeExisting decides the candidates, one body declaration at a time,
// and records the edits that go with them:
//
//   - if o is declared just for this ("var o T" earlier in the same block,
//     unused until the call), that declaration is deleted and the body's
//     declaration of v declares it instead;
//   - otherwise the body's declaration of v goes: "var v T" is dropped when
//     the body assigns v before reading it, and "v := e" becomes "v = e"
//     when every variable it declares merges; if some do not, ":=" stays
//     and assigns o, provided o is in the calling block itself.
func (x *inliner) mergeExisting(cands []existingMerge) {
	byDecl := map[ast.Stmt][]existingMerge{}
	var order []ast.Stmt
	for _, c := range cands {
		d, _ := x.topDecl(c.v)
		if byDecl[d] == nil {
			order = append(order, d)
		}
		byDecl[d] = append(byDecl[d], c)
	}
	accept := func(cs []existingMerge) {
		for _, c := range cs {
			x.merged[c.v] = c.l
			x.dropped[c.i] = true
		}
	}
	for _, d := range order {
		cs := byDecl[d]
		absorb := true
		var decls []*ast.DeclStmt
		for _, c := range cs {
			ds, ok := x.absorbable(c.o)
			absorb = absorb && ok
			decls = append(decls, ds)
		}
		switch d := d.(type) {
		case *ast.DeclStmt:
			c := cs[0]
			if absorb {
				x.callerEdits = append(x.callerEdits, x.deleteLine(x.Src, x.TF, decls[0]))
				accept(cs)
				continue
			}
			_, idx := x.topDecl(c.v)
			spec := d.Decl.(*ast.GenDecl).Specs[0].(*ast.ValueSpec)
			if len(spec.Values) == 0 && x.assignedBeforeRead(c.v, idx, c.o.Parent() == x.blockScope) {
				x.bodyEdits = append(x.bodyEdits, x.deleteLine(x.callee.Src, x.callee.TF, d))
				accept(cs)
			}
		case *ast.AssignStmt:
			declared := 0
			for _, lhs := range d.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && x.Info.Defs[id] != nil {
					declared++
				}
			}
			switch {
			case absorb:
				for _, ds := range decls {
					x.callerEdits = append(x.callerEdits, x.deleteLine(x.Src, x.TF, ds))
				}
				accept(cs)
			case declared == len(cs):
				x.bodyEdits = append(x.bodyEdits, edit.TextEdit{Start: x.callee.Off(d.TokPos), End: x.callee.Off(d.TokPos) + 2, NewText: "="})
				accept(cs)
			default:
				var keep []existingMerge
				for _, c := range cs {
					if c.o.Parent() == x.blockScope {
						keep = append(keep, c)
					}
				}
				accept(keep)
			}
		}
	}
}

// topDecl returns the top-level body statement declaring v ("var v T" with
// a single name, or "v := e" / "a, v := e"), and its index.
func (x *inliner) topDecl(v *types.Var) (ast.Stmt, int) {
	for i, s := range x.decl.Body.List {
		switch s := s.(type) {
		case *ast.DeclStmt:
			gd, ok := s.Decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR || len(gd.Specs) != 1 {
				continue
			}
			spec := gd.Specs[0].(*ast.ValueSpec)
			if len(spec.Names) == 1 && x.Info.Defs[spec.Names[0]] == v {
				return s, i
			}
		case *ast.AssignStmt:
			if s.Tok != token.DEFINE {
				continue
			}
			for _, l := range s.Lhs {
				if id, ok := l.(*ast.Ident); ok && x.Info.Defs[id] == v {
					return s, i
				}
			}
		}
	}
	return nil, -1
}

// assignedBeforeRead reports whether the first top-level statement after
// index idx that mentions v assigns it (as a plain name on the left of
// "=", or of a ":=" that reuses it if reuse is true) without reading it.
// With reuse, the caller's variable is in the calling block, so such a
// ":=" still assigns it once inlined.
func (x *inliner) assignedBeforeRead(v *types.Var, idx int, reuse bool) bool {
	for _, s := range x.decl.Body.List[idx+1:] {
		var refs []*ast.Ident
		ast.Inspect(s, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && x.Info.Uses[id] == v {
				refs = append(refs, id)
			}
			return true
		})
		if len(refs) == 0 {
			continue
		}
		as, ok := s.(*ast.AssignStmt)
		if !ok || (as.Tok != token.ASSIGN && !(as.Tok == token.DEFINE && reuse)) {
			return false
		}
		for _, r := range refs {
			direct := false
			for _, l := range as.Lhs {
				if l == ast.Expr(r) {
					direct = true
				}
			}
			if !direct {
				return false
			}
		}
		return true
	}
	return false
}

// absorbable returns o's declaration if it is "var o T" alone, earlier in
// the calling block, with no use of o between it and the call.
func (x *inliner) absorbable(o *types.Var) (*ast.DeclStmt, bool) {
	if o.Parent() != x.blockScope {
		return nil, false
	}
	for _, s := range x.listOf(x.stmt) {
		ds, ok := s.(*ast.DeclStmt)
		if !ok || s.Pos() >= x.stmt.Pos() {
			continue
		}
		gd, ok := ds.Decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR || len(gd.Specs) != 1 {
			continue
		}
		spec := gd.Specs[0].(*ast.ValueSpec)
		if len(spec.Names) != 1 || len(spec.Values) > 0 || x.Info.Defs[spec.Names[0]] != o {
			continue
		}
		for id, u := range x.Info.Uses {
			if u == o && id.Pos() > ds.End() && id.Pos() < x.stmt.Pos() {
				return nil, false
			}
		}
		if x.hasCommentOn(ds) {
			return nil, false
		}
		return ds, true
	}
	return nil, false
}

// hasCommentOn reports whether a comment is on the lines of n (deleting
// n would delete it).
func (x *inliner) hasCommentOn(n ast.Node) bool {
	first, last := x.TF.Line(n.Pos()), x.TF.Line(n.End())
	for _, cg := range x.File.Comments {
		if x.TF.Line(cg.End()) >= first && x.TF.Line(cg.Pos()) <= last {
			return true
		}
	}
	return false
}

// argsRead reports whether an argument of the call (or its receiver) reads
// o.
func (x *inliner) argsRead(o types.Object) bool {
	exprs := append([]ast.Expr(nil), x.call.Args...)
	if sel, ok := x.call.Fun.(*ast.SelectorExpr); ok {
		exprs = append(exprs, sel.X)
	}
	for _, e := range exprs {
		for _, id := range analysis.ScopeRefs(x.Info, e) {
			if x.Info.Uses[id] == o {
				return true
			}
		}
	}
	return false
}

// deadAfter reports whether the caller never reads v again after the call:
// the call is a terminal return, or v has no uses after the call's
// statement and the statement is not in a loop that v outlives, nor in a
// function with goto, which can jump back over it.
func (x *inliner) deadAfter(v *types.Var) bool {
	if !x.callerFA.IsLocal(v) || x.callerFA.Escapes(v) {
		return false
	}
	if x.shape == terminal {
		return true
	}
	for id, o := range x.Info.Uses {
		if o == v && id.Pos() >= x.stmt.End() && id.Pos() < x.callerFA.Node.End() {
			return false
		}
	}
	for _, n := range x.path {
		switch n.(type) {
		case *ast.ForStmt, *ast.RangeStmt:
			if v.Pos() < n.Pos() {
				return false
			}
		case *ast.FuncLit, *ast.FuncDecl:
			return !x.callerHasGoto() // a backward goto is a loop too
		}
	}
	return true
}
