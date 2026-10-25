// Package pattern matches Go code against a pattern with holes: an
// expression or statements in which some names stand for whatever the
// code has in their place, by the type checker's view of both.
package pattern

import (
	"go/ast"
	"go/token"
	"go/types"
	"reflect"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"golang.org/x/tools/go/packages"
)

// Matcher is a pattern, code of package Pkg, with what each of its names
// is: a hole, a field of a struct parameter, a local the pattern
// declares. At starts an attempt to match it against code. What each
// name is, is decided the first time it is matched, so the facts are set
// before the first attempt.
type Matcher struct {
	Pkg        *packages.Package             // the pattern's
	BodyLocals map[types.Object]types.Object // the pattern's locals
	Holes      map[types.Object]int          // the holes (a function's parameters and receiver), and how often the pattern uses each
	// Fields are the struct holes the pattern uses only by their fields:
	// the fields, in the order the pattern first uses them. Each field is
	// a hole of its own.
	Fields  map[types.Object][]string
	Recv    types.Object        // the hole that is a method's receiver, or nil
	Relax   Relaxations         // rules for code of another shape, or nil
	Compare Comparer            // how nodes are compared: ByKind if nil
	names   map[*ast.Ident]name // what each identifier of the pattern matched so far is
}

// Attempt is a match of the pattern against code of one package: what
// it has bound the holes to, the code's variables standing for the
// pattern's locals, and where the last match failed.
type Attempt struct {
	*Matcher
	Info  *types.Info          // the code's
	Bound map[HoleKey]ast.Expr // the code each hole stands for
	// Locals are the code's variables standing for the pattern's locals,
	// or nil where the pattern may declare none (matching an expression).
	Locals map[types.Object]types.Object
	depth  int   // how deep the comparison under way is
	Miss   *Miss // where the last match failed, or nil
}

// At starts an attempt to match the pattern against code whose info is
// info, with no hole bound.
func (m *Matcher) At(info *types.Info) *Attempt {
	return &Attempt{Matcher: m, Info: info, Bound: map[HoleKey]ast.Expr{}}
}

// comparer is how the pattern's nodes are compared.
func (m *Matcher) comparer() Comparer {
	if m.Compare == nil {
		return ByKind{}
	}
	return m.Compare
}

// Relaxations match code that does what the pattern does in another
// shape: rules a user of the matcher adds (fold's), which matching
// itself does not have. Without them, "a" + "b" does not match "ab".
type Relaxations interface {
	// Call matches a call whose arguments differ in number from the pattern's.
	Call(a *Attempt, p, x *ast.CallExpr) bool
	// Concat matches a string concatenation by its operands.
	Concat(a *Attempt, p, x *ast.BinaryExpr) bool
	// Address matches *p against an expression that is not a dereference.
	Address(a *Attempt, p *ast.StarExpr, x ast.Expr) bool
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
	P, X ast.Node
	// For identifiers, the innermost expressions around P and X, if any.
	AroundP, AroundX ast.Expr
	Depth            int
}

// Comparer compares a node of the pattern with one of the code: rules for
// each kind of node, calling back into the attempt (Match, MatchStmt) for
// the nodes inside. What a name is, is the attempt's to say.
type Comparer interface {
	Expr(a *Attempt, p, x ast.Expr) bool
	Stmt(a *Attempt, p, x ast.Stmt) bool
}

// ByKind compares nodes by rules for each kind of node, which cover the
// shapes fold folds, with the Matcher's relaxations. A Matcher without a
// comparer uses it.
type ByKind struct{}

// Match reports whether x, code of the attempt's package, is the
// pattern's p with its holes bound as in Bound, binding the others.
func (a *Attempt) Match(p, x ast.Expr) bool {
	a.depth++
	ok := a.comparer().Expr(a, p, x)
	a.depth--
	a.note(ok, p, x)
	return ok
}

// sub reports whether x matches the pattern's p, or both are missing.
func (a *Attempt) sub(p, x ast.Expr) bool {
	if p == nil || x == nil {
		return p == nil && x == nil
	}
	return a.Match(p, x)
}

// note keeps where a match failed: the innermost comparison that
// failed, unless a comparison around it then matched another way.
func (a *Attempt) note(ok bool, p, x ast.Node) {
	if ok {
		a.Miss = nil
	} else if a.Miss == nil {
		a.Miss = &Miss{P: p, X: x, Depth: a.depth}
	} else if _, id := a.Miss.P.(*ast.Ident); id && a.Miss.AroundP == nil {
		pe, okP := p.(ast.Expr)
		xe, okX := x.(ast.Expr)
		if okP && okX {
			a.Miss.AroundP, a.Miss.AroundX = pe, xe
		}
	}
}

func (k ByKind) Expr(a *Attempt, p ast.Expr, x ast.Expr) bool {
	p, x = ast.Unparen(p), ast.Unparen(x)
	pinfo := a.Pkg.TypesInfo
	switch pp := p.(type) {
	case *ast.Ident:
		return a.matchIdent(pp, x)
	case *ast.BasicLit:
		xb, ok := x.(*ast.BasicLit)
		return ok && xb.Kind == pp.Kind && xb.Value == pp.Value
	case *ast.SelectorExpr:
		return a.matchSelector(pp, x)
	case *ast.CallExpr:
		return a.matchCall(pp, x)
	case *ast.BinaryExpr:
		return a.matchBinary(pp, x)
	case *ast.UnaryExpr:
		xu, ok := x.(*ast.UnaryExpr)
		return ok && xu.Op == pp.Op && a.sub(pp.X, xu.X)
	case *ast.StarExpr:
		if xs, ok := x.(*ast.StarExpr); ok {
			return a.sub(pp.X, xs.X)
		}
		return a.Relax != nil && a.Relax.Address(a, pp, x)
	case *ast.IndexExpr:
		xi, ok := x.(*ast.IndexExpr)
		return ok && a.sub(pp.X, xi.X) && a.sub(pp.Index, xi.Index)
	case *ast.SliceExpr:
		xs, ok := x.(*ast.SliceExpr)
		return ok && xs.Slice3 == pp.Slice3 && a.sub(pp.X, xs.X) && a.sub(pp.Low, xs.Low) && a.sub(pp.High, xs.High) && a.sub(pp.Max, xs.Max)
	case *ast.TypeAssertExpr:
		xt, ok := x.(*ast.TypeAssertExpr)
		return ok && pp.Type != nil && xt.Type != nil && a.sub(pp.X, xt.X) && types.Identical(pinfo.TypeOf(pp.Type), a.Info.TypeOf(xt.Type))
	case *ast.CompositeLit:
		return a.matchComposite(pp, x)
	case *ast.KeyValueExpr:
		xk, ok := x.(*ast.KeyValueExpr)
		return ok && a.sub(pp.Key, xk.Key) && a.sub(pp.Value, xk.Value)
	}
	return false
}

// matchComposite reports whether x matches the composite literal pp of the
// pattern, element by element.
func (a *Attempt) matchComposite(pp *ast.CompositeLit, x ast.Expr) bool {
	pinfo := a.Pkg.TypesInfo
	xc, ok := x.(*ast.CompositeLit)
	if !ok || len(xc.Elts) != len(pp.Elts) || types.TypeString(pinfo.TypeOf(pp), nil) != types.TypeString(a.Info.TypeOf(xc), nil) {
		return false
	}
	for i := range pp.Elts {
		if !a.sub(pp.Elts[i], xc.Elts[i]) {
			return false
		}
	}
	return true
}

// matchBinary reports whether x matches the binary expression pp of the
// pattern; string concatenations match by their operands.
func (a *Attempt) matchBinary(pp *ast.BinaryExpr, x ast.Expr) bool {
	pinfo := a.Pkg.TypesInfo
	if a.Relax != nil && pp.Op == token.ADD && analysis.IsString(pinfo.TypeOf(pp)) {
		xb, ok := x.(*ast.BinaryExpr)
		return ok && xb.Op == token.ADD && a.Relax.Concat(a, pp, xb)
	}
	xb, ok := x.(*ast.BinaryExpr)
	return ok && xb.Op == pp.Op && a.sub(pp.X, xb.X) && a.sub(pp.Y, xb.Y)
}

// matchCall reports whether x matches the call pp of the pattern, argument
// by argument or as a format string.
func (a *Attempt) matchCall(pp *ast.CallExpr, x ast.Expr) bool {
	xc, ok := x.(*ast.CallExpr)
	if !ok || xc.Ellipsis.IsValid() != pp.Ellipsis.IsValid() || !a.sub(pp.Fun, xc.Fun) {
		return false
	}
	if len(xc.Args) != len(pp.Args) {
		return a.Relax != nil && a.Relax.Call(a, pp, xc)
	}
	for i := range pp.Args {
		if !a.sub(pp.Args[i], xc.Args[i]) {
			return false
		}
	}
	return true
}

// matchSelector reports whether x matches the selector pp of the pattern: a
// field of a struct parameter, a qualified name, or the same field or
// method of matching operands.
func (a *Attempt) matchSelector(pp *ast.SelectorExpr, x ast.Expr) bool {
	pinfo := a.Pkg.TypesInfo
	if id := IdentOf(pp.X); id != nil {
		po := a.nameOf(id).obj
		if _, byField := a.Fields[po]; byField {
			k := HoleKey{po, pp.Sel.Name}
			if prev, ok := a.Bound[k]; ok {
				return types.ExprString(prev) == types.ExprString(x)
			}
			xt := a.Info.TypeOf(x)
			if xt == nil || !a.Info.Types[x].IsValue() || !types.AssignableTo(xt, pinfo.TypeOf(pp)) && types.TypeString(xt, nil) != types.TypeString(pinfo.TypeOf(pp), nil) {
				return false
			}
			a.Bound[k] = x
			return true
		}
		if _, pkgName := po.(*types.PkgName); pkgName {
			if xi, ok := x.(*ast.Ident); ok { // Name in the package itself
				return xi.Name == pp.Sel.Name && SameObj(pinfo.Uses[pp.Sel], a.Info.Uses[xi])
			}
		}
	}
	xs, ok := x.(*ast.SelectorExpr)
	return ok && xs.Sel.Name == pp.Sel.Name && a.sub(pp.X, xs.X) && SameObj(pinfo.Uses[pp.Sel], a.Info.Uses[xs.Sel])
}

// name is what an identifier of the pattern is, by the Matcher's facts.
type name struct {
	kind nameKind
	obj  types.Object // what it declares or uses, or nil
}

// nameKind is the kind of a name of the pattern.
type nameKind int

const (
	plain     nameKind = iota // a name the code must use too
	declared                  // a local the pattern declares there
	bodyLocal                 // a local of the pattern's, declared outside what is matched
	hole                      // a hole, which stands for what the code has in its place
)

// nameOf is what the pattern's identifier id is: decided the first time
// it is matched, and kept for every site after.
func (m *Matcher) nameOf(id *ast.Ident) name {
	if n, ok := m.names[id]; ok {
		return n
	}
	pinfo := m.Pkg.TypesInfo
	n := name{plain, pinfo.Uses[id]}
	if d := pinfo.Defs[id]; d != nil || id.Name == "_" {
		n = name{declared, d}
	} else if _, ok := m.BodyLocals[n.obj]; ok {
		n.kind = bodyLocal
	} else if _, ok := m.Holes[n.obj]; ok && n.obj != nil {
		n.kind = hole
	}
	if m.names == nil {
		m.names = map[*ast.Ident]name{}
	}
	m.names[id] = n
	return n
}

// matchIdent reports whether x matches the identifier pp of the pattern,
// by what pp is: a local the pattern declares, one it binds, a hole, or
// the same object.
func (a *Attempt) matchIdent(pp *ast.Ident, x ast.Expr) bool {
	n := a.nameOf(pp)
	if n.kind == declared {
		// The occurrence's own name for it.
		xi, ok := x.(*ast.Ident)
		if !ok || pp.Name == "_" {
			return ok && xi.Name == "_"
		}
		xd := a.Info.Defs[xi]
		if xd == nil || a.Locals == nil {
			return false
		}
		a.Locals[n.obj] = xd
		return true
	}
	if xo, local := a.Locals[n.obj]; local {
		xi, ok := x.(*ast.Ident)
		return ok && a.Info.Uses[xi] == xo
	}
	switch n.kind {
	case bodyLocal:
		return false
	case hole:
		return a.bindHole(n.obj, x)
	}
	switch x := x.(type) {
	case *ast.Ident:
		// Names the type checker gives no object (a type switch's symbol)
		// are the same when spelled the same.
		return x.Name == pp.Name && (SameObj(n.obj, a.Info.Uses[x]) || n.obj == nil && a.Info.Uses[x] == nil)
	case *ast.SelectorExpr: // pkg.Name in another package
		_, pkgName := a.Info.Uses[IdentOf(x.X)].(*types.PkgName)
		return pkgName && x.Sel.Name == pp.Name && SameObj(n.obj, a.Info.Uses[x.Sel])
	}
	return false
}

// bindHole reports whether x can stand for the hole po: what po is already
// bound to, or else a value of po's type, which it binds.
func (a *Attempt) bindHole(po types.Object, x ast.Expr) bool {
	if prev, ok := a.Bound[HoleKey{po, ""}]; ok {
		return types.ExprString(prev) == types.ExprString(x)
	}
	xt := a.Info.TypeOf(x)
	if xt == nil || !a.Info.Types[x].IsValue() {
		return false
	}
	if po == a.Recv {
		if !SameObj(BaseName(xt), BaseName(po.Type())) {
			return false
		}
	} else if !types.AssignableTo(xt, po.Type()) && types.TypeString(xt, nil) != types.TypeString(po.Type(), nil) {
		return false
	}
	a.Bound[HoleKey{po, ""}] = x
	return true
}

// MatchStmt reports whether x is the pattern's statement p.
func (a *Attempt) MatchStmt(p, x ast.Stmt) bool {
	a.depth++
	ok := a.compareStmt(p, x)
	a.depth--
	a.note(ok, p, x)
	return ok
}

// compareStmt reports whether x is the pattern's statement p, comparing
// their nodes; MatchStmt calls it and keeps where it failed.
func (a *Attempt) compareStmt(p ast.Stmt, x ast.Stmt) bool {
	if p == nil || x == nil {
		return p == nil && x == nil
	}
	return a.comparer().Stmt(a, p, x)
}

func (k ByKind) Stmt(a *Attempt, p ast.Stmt, x ast.Stmt) bool {
	exprs := func(p, x []ast.Expr) bool {
		if len(p) != len(x) {
			return false
		}
		for i := range p {
			if !a.sub(p[i], x[i]) {
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
			if !a.MatchStmt(p.List[i], x.List[i]) {
				return false
			}
		}
		return true
	}
	switch pp := p.(type) {
	case *ast.ExprStmt:
		xs, ok := x.(*ast.ExprStmt)
		return ok && a.sub(pp.X, xs.X)
	case *ast.AssignStmt:
		xs, ok := x.(*ast.AssignStmt)
		return ok && xs.Tok == pp.Tok && exprs(pp.Rhs, xs.Rhs) && exprs(pp.Lhs, xs.Lhs)
	case *ast.IncDecStmt:
		xs, ok := x.(*ast.IncDecStmt)
		return ok && xs.Tok == pp.Tok && a.sub(pp.X, xs.X)
	case *ast.BlockStmt:
		xs, ok := x.(*ast.BlockStmt)
		return ok && block(pp, xs)
	case *ast.IfStmt:
		xs, ok := x.(*ast.IfStmt)
		return ok && a.MatchStmt(pp.Init, xs.Init) && a.sub(pp.Cond, xs.Cond) && block(pp.Body, xs.Body) && a.MatchStmt(pp.Else, xs.Else)
	case *ast.ForStmt:
		xs, ok := x.(*ast.ForStmt)
		return ok && a.MatchStmt(pp.Init, xs.Init) && a.sub(pp.Cond, xs.Cond) && a.MatchStmt(pp.Post, xs.Post) && block(pp.Body, xs.Body)
	case *ast.RangeStmt:
		xs, ok := x.(*ast.RangeStmt)
		return ok && xs.Tok == pp.Tok && a.sub(pp.X, xs.X) && a.sub(pp.Key, xs.Key) && a.sub(pp.Value, xs.Value) && block(pp.Body, xs.Body)
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
			if ps.Type != nil && !types.Identical(a.Pkg.TypesInfo.TypeOf(ps.Type), a.Info.TypeOf(vs.Type)) {
				return false
			}
			for j := range ps.Names {
				if !a.sub(ps.Names[j], vs.Names[j]) {
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

// Exactly compares code of any shape exactly: every node but names and
// literals field by field. Literal, if set, decides whether literals of
// the same kind match, in place of comparing their values.
type Exactly struct {
	Literal func(p, x *ast.BasicLit) bool
}

// Expr compares names by what they are, literals by Literal, and the
// rest field by field.
func (e Exactly) Expr(a *Attempt, p, x ast.Expr) bool {
	switch pp := p.(type) {
	case *ast.Ident:
		return a.matchIdent(pp, ast.Unparen(x))
	case *ast.BasicLit:
		xb, ok := ast.Unparen(x).(*ast.BasicLit)
		if !ok || xb.Kind != pp.Kind {
			return false
		}
		if e.Literal != nil {
			return e.Literal(pp, xb)
		}
		return xb.Value == pp.Value
	}
	return a.fields(p, x)
}

// Stmt compares statements field by field.
func (Exactly) Stmt(a *Attempt, p, x ast.Stmt) bool {
	return a.fields(p, x)
}

// node compares two nodes of any kind, for Exactly: expressions and
// statements by Match and MatchStmt, the rest field by field.
func (a *Attempt) node(p, x ast.Node) bool {
	switch p := p.(type) {
	case *ast.CommentGroup:
		return true
	case ast.Expr:
		if x, ok := x.(ast.Expr); ok {
			return a.Match(p, x)
		}
	case ast.Stmt:
		if x, ok := x.(ast.Stmt); ok {
			return a.MatchStmt(p, x)
		}
	}
	a.depth++
	ok := a.fields(p, x)
	a.depth--
	a.note(ok, p, x)
	return ok
}

var posType = reflect.TypeOf(token.NoPos)

// fields compares p and x, nodes of one kind, field by field: the nodes
// in them by node, other values by equality; positions do not count.
func (a *Attempt) fields(p, x ast.Node) bool {
	if reflect.TypeOf(p) != reflect.TypeOf(x) {
		return false
	}
	vp, vx := reflect.ValueOf(p).Elem(), reflect.ValueOf(x).Elem()
	for i := range vp.NumField() {
		fp, fx := vp.Field(i), vx.Field(i)
		if fp.Type() == posType {
			continue
		}
		switch fp.Kind() {
		case reflect.Interface, reflect.Pointer:
			if fp.IsNil() || fx.IsNil() {
				if fp.IsNil() != fx.IsNil() {
					return false
				}
				continue
			}
			np, okP := fp.Interface().(ast.Node)
			nx, okX := fx.Interface().(ast.Node)
			if okP && okX && !a.node(np, nx) {
				return false
			}
		case reflect.Slice:
			if fp.Len() != fx.Len() {
				return false
			}
			for j := range fp.Len() {
				np, okP := fp.Index(j).Interface().(ast.Node)
				nx, okX := fx.Index(j).Interface().(ast.Node)
				if okP && okX && !a.node(np, nx) {
					return false
				}
			}
		case reflect.Int, reflect.Bool, reflect.String:
			if fp.Interface() != fx.Interface() {
				return false
			}
		}
	}
	return true
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
