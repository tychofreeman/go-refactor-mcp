// Package pattern matches Go code against a pattern with holes: an
// expression or statements in which some names stand for whatever the
// code has in their place, by the type checker's view of both.
package pattern

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"golang.org/x/tools/go/packages"
)

// Matcher matches code against a pattern, code of package Pkg: it knows
// what each of the pattern's names is (a hole, a field of a struct
// parameter, a local the pattern declares), binds the holes, and keeps
// where the last match failed.
type Matcher struct {
	Pkg *packages.Package // the pattern's
	// Locals are, while matching statements, the code's variables
	// standing for the pattern's locals; BodyLocals are the pattern's locals.
	Locals, BodyLocals map[types.Object]types.Object
	Holes              map[types.Object]int // the holes (a function's parameters and receiver), and how often the pattern uses each
	// Fields are the struct holes the pattern uses only by their fields:
	// the fields, in the order the pattern first uses them. Each field is
	// a hole of its own.
	Fields map[types.Object][]string
	Recv   types.Object // the hole that is a method's receiver, or nil
	Relax  Relaxations  // rules for code of another shape, or nil
	depth  int          // how deep the comparison under way is
	Miss   *Miss        // where the last match failed, or nil
}

// Relaxations match code that does what the pattern does in another
// shape: rules a user of the matcher adds (fold's), which matching
// itself does not have. Without them, "a" + "b" does not match "ab".
type Relaxations interface {
	// Call matches a call whose arguments differ in number from the pattern's.
	Call(m *Matcher, p, x *ast.CallExpr, info *types.Info, b map[HoleKey]ast.Expr) bool
	// Concat matches a string concatenation by its operands.
	Concat(m *Matcher, p, x *ast.BinaryExpr, info *types.Info, b map[HoleKey]ast.Expr) bool
	// Address matches *p against an expression that is not a dereference.
	Address(m *Matcher, p *ast.StarExpr, x ast.Expr, info *types.Info, b map[HoleKey]ast.Expr) bool
}

// HoleKey is a hole: a parameter, or with Field one field of a struct
// parameter the pattern uses only by its fields.
type HoleKey struct {
	Obj   types.Object
	Field string
}

// Miss is where a match failed: the innermost nodes of the pattern (p)
// and of the code (x) that differ, depth comparisons below the top.
type Miss struct {
	P, X  ast.Node
	Depth int
}

// Match reports whether x, of the package whose info is info, is the
// pattern p (of Pkg) with its holes filled in b.
func (m *Matcher) Match(p, x ast.Expr, info *types.Info, b map[HoleKey]ast.Expr) bool {
	m.depth++
	ok := m.compareExpr(p, x, info, b)
	m.depth--
	m.note(ok, p, x)
	return ok
}

// note keeps where a match failed: the innermost comparison that
// failed, unless a comparison around it then matched another way.
func (m *Matcher) note(ok bool, p, x ast.Node) {
	if ok {
		m.Miss = nil
	} else if m.Miss == nil {
		m.Miss = &Miss{P: p, X: x, Depth: m.depth}
	}
}

// compareExpr reports whether x is the pattern p, comparing their nodes;
// Match calls it and keeps where it failed.
func (m *Matcher) compareExpr(p ast.Expr, x ast.Expr, info *types.Info, b map[HoleKey]ast.Expr) bool {
	p, x = ast.Unparen(p), ast.Unparen(x)
	pinfo := m.Pkg.TypesInfo
	sub := func(p, x ast.Expr) bool {
		if p == nil || x == nil {
			return p == nil && x == nil
		}
		return m.Match(p, x, info, b)
	}
	switch pp := p.(type) {
	case *ast.Ident:
		return m.matchIdent(pp, x, info, b, pinfo)
	case *ast.BasicLit:
		xb, ok := x.(*ast.BasicLit)
		return ok && xb.Kind == pp.Kind && xb.Value == pp.Value
	case *ast.SelectorExpr:
		return m.matchSelector(pp, x, info, b, pinfo, sub)
	case *ast.CallExpr:
		return m.matchCall(pp, x, info, b, sub)
	case *ast.BinaryExpr:
		return m.matchBinary(pp, x, info, b, pinfo, sub)
	case *ast.UnaryExpr:
		xu, ok := x.(*ast.UnaryExpr)
		return ok && xu.Op == pp.Op && sub(pp.X, xu.X)
	case *ast.StarExpr:
		if xs, ok := x.(*ast.StarExpr); ok {
			return sub(pp.X, xs.X)
		}
		return m.Relax != nil && m.Relax.Address(m, pp, x, info, b)
	case *ast.IndexExpr:
		xi, ok := x.(*ast.IndexExpr)
		return ok && sub(pp.X, xi.X) && sub(pp.Index, xi.Index)
	case *ast.SliceExpr:
		xs, ok := x.(*ast.SliceExpr)
		return ok && xs.Slice3 == pp.Slice3 && sub(pp.X, xs.X) && sub(pp.Low, xs.Low) && sub(pp.High, xs.High) && sub(pp.Max, xs.Max)
	case *ast.TypeAssertExpr:
		xt, ok := x.(*ast.TypeAssertExpr)
		return ok && pp.Type != nil && xt.Type != nil && sub(pp.X, xt.X) && types.Identical(pinfo.TypeOf(pp.Type), info.TypeOf(xt.Type))
	case *ast.CompositeLit:
		return matchComposite(pp, x, info, pinfo, sub)
	case *ast.KeyValueExpr:
		xk, ok := x.(*ast.KeyValueExpr)
		return ok && sub(pp.Key, xk.Key) && sub(pp.Value, xk.Value)
	}
	return false
}

// matchComposite reports whether x matches the composite literal pp of the
// pattern, element by element.
func matchComposite(pp *ast.CompositeLit, x ast.Expr, info *types.Info, pinfo *types.Info, sub func(p ast.Expr, x ast.Expr) bool) bool {
	xc, ok := x.(*ast.CompositeLit)
	if !ok || len(xc.Elts) != len(pp.Elts) || types.TypeString(pinfo.TypeOf(pp), nil) != types.TypeString(info.TypeOf(xc), nil) {
		return false
	}
	for i := range pp.Elts {
		if !sub(pp.Elts[i], xc.Elts[i]) {
			return false
		}
	}
	return true
}

// matchBinary reports whether x matches the binary expression pp of the
// pattern; string concatenations match by their operands.
func (m *Matcher) matchBinary(pp *ast.BinaryExpr, x ast.Expr, info *types.Info, b map[HoleKey]ast.Expr, pinfo *types.Info, sub func(p ast.Expr, x ast.Expr) bool) bool {
	if m.Relax != nil && pp.Op == token.ADD && analysis.IsString(pinfo.TypeOf(pp)) {
		xb, ok := x.(*ast.BinaryExpr)
		return ok && xb.Op == token.ADD && m.Relax.Concat(m, pp, xb, info, b)
	}
	xb, ok := x.(*ast.BinaryExpr)
	return ok && xb.Op == pp.Op && sub(pp.X, xb.X) && sub(pp.Y, xb.Y)
}

// matchCall reports whether x matches the call pp of the pattern, argument
// by argument or as a format string.
func (m *Matcher) matchCall(pp *ast.CallExpr, x ast.Expr, info *types.Info, b map[HoleKey]ast.Expr, sub func(p ast.Expr, x ast.Expr) bool) bool {
	xc, ok := x.(*ast.CallExpr)
	if !ok || xc.Ellipsis.IsValid() != pp.Ellipsis.IsValid() || !sub(pp.Fun, xc.Fun) {
		return false
	}
	if len(xc.Args) != len(pp.Args) {
		return m.Relax != nil && m.Relax.Call(m, pp, xc, info, b)
	}
	for i := range pp.Args {
		if !sub(pp.Args[i], xc.Args[i]) {
			return false
		}
	}
	return true
}

// matchSelector reports whether x matches the selector pp of the pattern: a
// field of a struct parameter, a qualified name, or the same field or
// method of matching operands.
func (m *Matcher) matchSelector(pp *ast.SelectorExpr, x ast.Expr, info *types.Info, b map[HoleKey]ast.Expr, pinfo *types.Info, sub func(p ast.Expr, x ast.Expr) bool) bool {
	if id := IdentOf(pp.X); id != nil {
		po := pinfo.Uses[id]
		if _, byField := m.Fields[po]; byField {
			k := HoleKey{po, pp.Sel.Name}
			if prev, ok := b[k]; ok {
				return types.ExprString(prev) == types.ExprString(x)
			}
			xt := info.TypeOf(x)
			if xt == nil || !info.Types[x].IsValue() || !types.AssignableTo(xt, pinfo.TypeOf(pp)) && types.TypeString(xt, nil) != types.TypeString(pinfo.TypeOf(pp), nil) {
				return false
			}
			b[k] = x
			return true
		}
		if _, pkgName := po.(*types.PkgName); pkgName {
			if xi, ok := x.(*ast.Ident); ok { // Name in the package itself
				return xi.Name == pp.Sel.Name && SameObj(pinfo.Uses[pp.Sel], info.Uses[xi])
			}
		}
	}
	xs, ok := x.(*ast.SelectorExpr)
	return ok && xs.Sel.Name == pp.Sel.Name && sub(pp.X, xs.X) && SameObj(pinfo.Uses[pp.Sel], info.Uses[xs.Sel])
}

// matchIdent reports whether x matches the identifier pp of the pattern: a
// name a local of fn declares, a hole, or the same object.
func (m *Matcher) matchIdent(pp *ast.Ident, x ast.Expr, info *types.Info, b map[HoleKey]ast.Expr, pinfo *types.Info) bool {
	if pd := pinfo.Defs[pp]; pd != nil || pp.Name == "_" {
		// A local fn declares: the occurrence's own name for it.
		xi, ok := x.(*ast.Ident)
		if !ok || pp.Name == "_" {
			return ok && xi.Name == "_"
		}
		xd := info.Defs[xi]
		if xd == nil || m.Locals == nil {
			return false
		}
		m.Locals[pd] = xd
		return true
	}
	if xo, local := m.Locals[pinfo.Uses[pp]]; local {
		xi, ok := x.(*ast.Ident)
		return ok && info.Uses[xi] == xo
	}
	if _, local := m.BodyLocals[pinfo.Uses[pp]]; local {
		return false
	}
	po := pinfo.Uses[pp]
	if _, hole := m.Holes[po]; hole && po != nil {
		if prev, ok := b[HoleKey{po, ""}]; ok {
			return types.ExprString(prev) == types.ExprString(x)
		}
		xt := info.TypeOf(x)
		if xt == nil || !info.Types[x].IsValue() {
			return false
		}
		if po == m.Recv {
			if !SameObj(BaseName(xt), BaseName(po.Type())) {
				return false
			}
		} else if !types.AssignableTo(xt, po.Type()) && types.TypeString(xt, nil) != types.TypeString(po.Type(), nil) {
			return false
		}
		b[HoleKey{po, ""}] = x
		return true
	}
	switch x := x.(type) {
	case *ast.Ident:
		return x.Name == pp.Name && SameObj(po, info.Uses[x])
	case *ast.SelectorExpr: // pkg.Name in another package
		_, pkgName := info.Uses[IdentOf(x.X)].(*types.PkgName)
		return pkgName && x.Sel.Name == pp.Name && SameObj(po, info.Uses[x.Sel])
	}
	return false
}

// MatchStmt reports whether x is the pattern's statement p.
func (m *Matcher) MatchStmt(p, x ast.Stmt, info *types.Info, b map[HoleKey]ast.Expr) bool {
	m.depth++
	ok := m.compareStmt(p, x, info, b)
	m.depth--
	m.note(ok, p, x)
	return ok
}

// compareStmt reports whether x is the pattern's statement p, comparing
// their nodes; MatchStmt calls it and keeps where it failed.
func (m *Matcher) compareStmt(p ast.Stmt, x ast.Stmt, info *types.Info, b map[HoleKey]ast.Expr) bool {
	if p == nil || x == nil {
		return p == nil && x == nil
	}
	expr := func(p, x ast.Expr) bool {
		if p == nil || x == nil {
			return p == nil && x == nil
		}
		return m.Match(p, x, info, b)
	}
	exprs := func(p, x []ast.Expr) bool {
		if len(p) != len(x) {
			return false
		}
		for i := range p {
			if !expr(p[i], x[i]) {
				return false
			}
		}
		return true
	}
	block := func(p, x *ast.BlockStmt) bool {
		if p == nil || x == nil {
			return p == nil && x == nil
		}
		if len(p.List) != len(x.List) {
			return false
		}
		for i := range p.List {
			if !m.MatchStmt(p.List[i], x.List[i], info, b) {
				return false
			}
		}
		return true
	}
	switch pp := p.(type) {
	case *ast.ExprStmt:
		xs, ok := x.(*ast.ExprStmt)
		return ok && expr(pp.X, xs.X)
	case *ast.AssignStmt:
		xs, ok := x.(*ast.AssignStmt)
		return ok && xs.Tok == pp.Tok && exprs(pp.Rhs, xs.Rhs) && exprs(pp.Lhs, xs.Lhs)
	case *ast.IncDecStmt:
		xs, ok := x.(*ast.IncDecStmt)
		return ok && xs.Tok == pp.Tok && expr(pp.X, xs.X)
	case *ast.BlockStmt:
		xs, ok := x.(*ast.BlockStmt)
		return ok && block(pp, xs)
	case *ast.IfStmt:
		xs, ok := x.(*ast.IfStmt)
		return ok && m.MatchStmt(pp.Init, xs.Init, info, b) && expr(pp.Cond, xs.Cond) && block(pp.Body, xs.Body) && m.MatchStmt(pp.Else, xs.Else, info, b)
	case *ast.ForStmt:
		xs, ok := x.(*ast.ForStmt)
		return ok && m.MatchStmt(pp.Init, xs.Init, info, b) && expr(pp.Cond, xs.Cond) && m.MatchStmt(pp.Post, xs.Post, info, b) && block(pp.Body, xs.Body)
	case *ast.RangeStmt:
		xs, ok := x.(*ast.RangeStmt)
		return ok && xs.Tok == pp.Tok && expr(pp.X, xs.X) && expr(pp.Key, xs.Key) && expr(pp.Value, xs.Value) && block(pp.Body, xs.Body)
	case *ast.DeclStmt:
		xs, ok := x.(*ast.DeclStmt)
		pg, _ := pp.Decl.(*ast.GenDecl)
		xg, _ := xs.Decl.(*ast.GenDecl)
		if !ok || pg == nil || xg == nil || pg.Tok != xg.Tok || len(pg.Specs) != len(xg.Specs) {
			return false
		}
		for i := range pg.Specs {
			ps, _ := pg.Specs[i].(*ast.ValueSpec)
			vs, _ := xg.Specs[i].(*ast.ValueSpec)
			if ps == nil || vs == nil || len(ps.Names) != len(vs.Names) || (ps.Type == nil) != (vs.Type == nil) || !exprs(ps.Values, vs.Values) {
				return false
			}
			if ps.Type != nil && !types.Identical(m.Pkg.TypesInfo.TypeOf(ps.Type), info.TypeOf(vs.Type)) {
				return false
			}
			for j := range ps.Names {
				if !expr(ps.Names[j], vs.Names[j]) {
					return false
				}
			}
		}
		return true
	case *ast.EmptyStmt:
		_, ok := x.(*ast.EmptyStmt)
		return ok
	}
	return false
}

// SameObj reports whether a and b, from any variants of their packages,
// are the same object.
func SameObj(a, b types.Object) bool {
	switch {
	case a == nil || b == nil:
		return false
	case a.Name() != b.Name():
		return false
	case a.Pkg() == nil || b.Pkg() == nil:
		return a.Pkg() == nil && b.Pkg() == nil
	}
	if pa, ok := a.(*types.PkgName); ok {
		pb, ok := b.(*types.PkgName)
		return ok && pa.Imported().Path() == pb.Imported().Path()
	}
	return a.Pkg().Path() == b.Pkg().Path() && a.Pos() == b.Pos()
}

// BaseName is the type name of t, T or *T, or nil.
func BaseName(t types.Type) types.Object {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := t.(*types.Named); ok {
		return n.Obj()
	}
	return nil
}

// IdentOf is e's identifier, if it is one.
func IdentOf(e ast.Expr) *ast.Ident {
	id, _ := ast.Unparen(e).(*ast.Ident)
	return id
}
