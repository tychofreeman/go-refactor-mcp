package changesig

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
)

// risk is something that evaluating one node could do, or depend on.
type risk struct {
	node ast.Node
	why  string
	// effect: a call or channel operation.
	effect bool
	// shared: it reads memory that other code can change (through a
	// pointer, a slice or map element, a package variable).
	shared bool
	// nilDeref: it could panic, only by dereferencing a nil pointer.
	// Every such panic is the same runtime error.
	nilDeref bool
	// panics: it could panic some other way.
	panics bool
}

// riskOf classifies the node n itself, not its children; ok is false when
// evaluating it is harmless.
func riskOf(info *types.Info, n ast.Node) (r risk, ok bool) {
	r.node = n
	switch n := n.(type) {
	case *ast.StarExpr:
		if !info.Types[n].IsType() {
			r.why, r.shared, r.nilDeref = "could panic: it dereferences a pointer", true, true
		}
	case *ast.SelectorExpr:
		if sel := info.Selections[n]; sel != nil && sel.Kind() == types.FieldVal && sel.Indirect() {
			r.why, r.shared, r.nilDeref = "could panic: it reads a field through a pointer", true, true
		}
	case *ast.IndexExpr:
		tv := info.Types[n.X]
		if tv.IsType() {
			break
		}
		switch tv.Type.Underlying().(type) {
		case *types.Map:
			r.why, r.shared = "reads a map", true
		case *types.Slice, *types.Pointer: // a pointer to an array
			r.why, r.shared, r.panics = "could panic: it indexes", true, true
		default:
			if info.Types[n.Index].Value == nil {
				r.why, r.panics = "could panic: it indexes", true
			}
		}
	case *ast.SliceExpr:
		r.why, r.panics = "could panic: it slices", true
	case *ast.TypeAssertExpr:
		if n.Type != nil { // x.(type) in a type switch cannot panic
			r.why, r.panics = "could panic: it is a type assertion", true
		}
	case *ast.BinaryExpr:
		if (n.Op == token.QUO || n.Op == token.REM) && info.Types[n.Y].Value == nil {
			r.why, r.panics = "could panic: it divides by a value that could be zero", true
		}
	case *ast.CallExpr:
		if b, isBuiltin := info.Uses[analysis.IdentOf(n.Fun)].(*types.Builtin); isBuiltin && (b.Name() == "len" || b.Name() == "cap") && len(n.Args) == 1 {
			if analysis.IsMapOrChan(info.TypeOf(n.Args[0])) {
				r.why, r.shared = "reads the length of a map or channel", true
			}
			break
		}
		if known, panics := analysis.KnownCall(info, n); known {
			switch panics {
			case analysis.NilPanic:
				r.why, r.nilDeref = "could panic: it calls "+types.ExprString(n.Fun)+" on what could be nil", true
			case analysis.OtherPanic:
				r.why, r.panics = "could panic: it calls "+types.ExprString(n.Fun), true
			}
			break
		}
		if analysis.HasEffect(info, n) {
			r.why, r.effect = "has side effects", true
		}
	case *ast.UnaryExpr:
		if analysis.HasEffect(info, n) {
			r.why, r.effect = "has side effects", true
		}
	case *ast.Ident:
		if v, isVar := info.Uses[n].(*types.Var); isVar && v.Pkg() != nil && v.Parent() == v.Pkg().Scope() {
			r.why, r.shared = fmt.Sprintf("reads the package variable %s", v.Name()), true
		}
	}
	return r, r.why != ""
}

// risks lists what evaluating e could do or depend on, in source order.
// Function literals are not evaluated (defining one runs nothing), nor
// are constant expressions.
func risks(info *types.Info, e ast.Node) []risk {
	var out []risk
	ast.Inspect(e, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if ex, ok := n.(ast.Expr); ok && info.Types[ex].Value != nil {
			return false
		}
		if r, ok := riskOf(info, n); ok {
			out = append(out, r)
		}
		return true
	})
	return out
}

// firstRisk returns the first risk of e that matches, or nil.
func firstRisk(info *types.Info, e ast.Node, match func(risk) bool) *risk {
	for _, r := range risks(info, e) {
		if match(r) {
			return &r
		}
	}
	return nil
}

// checkEvaluation checks that evaluating expr at the call gives what the
// body got, and does what the body did. expr must have no side effects.
// If it can panic or reads memory that other code can change, it must be
// evaluated at entry: on every call, once, and after nothing that could
// change that memory or make the outcome differ. A panic before it is
// allowed only when both could only be nil dereferences, which panic
// alike.
func (t *target) checkEvaluation(info *types.Info, file *ast.File, expr ast.Expr, q string) error {
	// canPanic is the first way expr could panic other than a nil
	// dereference, or else the first nil dereference.
	var shared, canPanic *risk
	for _, r := range risks(info, expr) {
		switch {
		case r.effect:
			return refactor.Refusef("%s has side effects (%s); evaluated at each call instead, they would happen where the body would not", q, t.brief(r.node))
		case r.shared && shared == nil:
			shared = &r
		}
		if r.panics && (canPanic == nil || canPanic.nilDeref) || r.nilDeref && canPanic == nil {
			canPanic = &r
		}
	}
	if shared == nil && canPanic == nil {
		return nil
	}
	first := canPanic
	if first == nil {
		first = shared
	}
	where, repeated := t.notOnEveryCall(analysis.PathTo(file, expr))
	if usesGoto(t.decl.Body) {
		where, repeated = fmt.Sprintf("%s uses goto", t.obj.Name()), true
	}
	switch {
	case repeated:
		return refactor.Refusef("%s %s, and it can be evaluated more than once per call (%s)", q, first.why, where)
	case where != "" && canPanic != nil:
		return refactor.Refusef("%s %s, and it is not evaluated on every call (%s)", q, canPanic.why, where)
	}

	fa := analysis.AnalyzeFunc(info, t.decl)
	var err error
	ast.Inspect(t.decl.Body, func(n ast.Node) bool {
		if n == nil || err != nil || n.Pos() >= expr.Pos() {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if n.End() > expr.Pos() {
			return true // it encloses expr, and finishes after it
		}
		r, risky := riskOf(info, n)
		write := t.writes(fa, n)
		switch {
		case (r.effect || write) && shared != nil:
			err = refactor.Refusef("%s reads memory that %s (at %s) could change before it", q, t.brief(n), t.ws.Position(n.Pos()))
		case r.effect && canPanic != nil:
			err = refactor.Refusef("%s %s, and %s (at %s) has side effects before it", q, canPanic.why, t.brief(n), t.ws.Position(n.Pos()))
		case risky && canPanic != nil && (r.panics || r.nilDeref && canPanic.panics):
			err = refactor.Refusef("%s %s, and %s (at %s) could panic before it", q, canPanic.why, t.brief(n), t.ws.Position(n.Pos()))
		}
		return err == nil
	})
	return err
}

// writes reports whether the statement n assigns anything other than a
// local variable whose address is never taken nor captured.
func (t *target) writes(fa *analysis.Func, n ast.Node) bool {
	harmless := func(e ast.Expr) bool {
		id, ok := ast.Unparen(e).(*ast.Ident)
		if !ok {
			return false
		}
		if obj, defined := fa.Info.Defs[id]; id.Name == "_" || defined && obj == nil {
			return true // nothing, or the name a type switch declares in each case
		}
		v, ok := fa.Info.ObjectOf(id).(*types.Var)
		return ok && fa.IsLocal(v) && !fa.Escapes(v)
	}
	switch n := n.(type) {
	case *ast.AssignStmt:
		for _, l := range n.Lhs {
			if !harmless(l) {
				return true
			}
		}
	case *ast.IncDecStmt:
		return !harmless(n.X)
	case *ast.RangeStmt:
		if n.Tok == token.ASSIGN {
			return n.Key != nil && !harmless(n.Key) || n.Value != nil && !harmless(n.Value)
		}
	}
	return false
}

// usesGoto reports whether body has a goto, which could jump back and
// evaluate an expression again.
func usesGoto(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if b, ok := n.(*ast.BranchStmt); ok && b.Tok == token.GOTO {
			found = true
		}
		return !found
	})
	return found
}

// notOnEveryCall says where on path (from an expression up to the body)
// the expression is evaluated conditionally, or "" if it is evaluated on
// every call; repeated means it can be evaluated more than once.
func (t *target) notOnEveryCall(path []ast.Node) (where string, repeated bool) {
	for i := 1; i < len(path) && path[i] != t.decl.Body; i++ {
		child := path[i-1]
		switch p := path[i].(type) {
		case *ast.FuncLit:
			return "it is inside a function literal", true
		case *ast.ForStmt:
			if child != p.Init {
				return "it is inside a for loop", true
			}
		case *ast.RangeStmt:
			if child == p.Body {
				return "it is inside a range loop", true
			}
		case *ast.IfStmt:
			if child == p.Body || child == p.Else {
				where = "it is inside an if"
			}
		case *ast.CaseClause:
			where = "it is inside a case"
		case *ast.CommClause:
			where = "it is inside a select case"
		case *ast.BinaryExpr:
			if (p.Op == token.LAND || p.Op == token.LOR) && child == p.Y {
				where = fmt.Sprintf("it is the right operand of %s", p.Op)
			}
		}
	}
	return where, false
}

// brief returns n's source for a message, shortened to its start if long.
func (t *target) brief(n ast.Node) string {
	s := strings.Join(strings.Fields(t.text(n)), " ")
	if len(s) > 40 {
		return s[:37] + "..."
	}
	return s
}
