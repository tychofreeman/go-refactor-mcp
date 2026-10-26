package changesig

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
)

// checkEvaluation checks that evaluating expr at the call gives what the
// body got, and does what the body did: a move of expr from where the
// body evaluates it to the call, before everything the body does first,
// evaluated once on every call. The value may not have side effects at
// all: they belong to the body, not to every call.
func (t *target) checkEvaluation(info *types.Info, file *ast.File, expr ast.Expr, q string) error {
	m := analysis.Move{Effects: t.ws.Effects(), Func: analysis.AnalyzeFunc(info, t.decl), Expr: expr}
	if s := m.Effects.Of(m.Func, expr); s.Effect != "" { // the value is evaluated at the call, where the body's effects do not belong
		return refactor.Refusef("%s has side effects (%s); evaluated at each call instead, they would happen where the body would not", q, t.brief(s.At.Effect))
	}
	where, repeated := t.notOnEveryCall(analysis.PathTo(file, expr))
	if usesGoto(t.decl.Body) {
		where, repeated = fmt.Sprintf("%s uses goto", t.obj.Name()), true
	}
	switch {
	case repeated:
		m.Times = analysis.Several
	case where != "":
		m.Times = analysis.Maybe
	}
	m.Past = analysis.EvaluatedBefore(t.decl.Body, expr.Pos(), nil)
	o := m.Check()
	switch {
	case o == nil:
		return nil
	case o.Past == nil && repeated:
		return refactor.Refusef("%s %s, and it can be evaluated more than once per call (%s)", q, o.Does, where)
	case o.Past == nil:
		return refactor.Refusef("%s %s, and it is not evaluated on every call (%s)", q, o.Does, where)
	case o.PastDoes == "leaves":
		return refactor.Refusef("%s %s, and it is not evaluated on every call (%s (at %s) returns before it)", q, o.Does, t.brief(o.Past), t.ws.Position(o.Past.Pos()))
	case o.Kind == analysis.Reads:
		return refactor.Refusef("%s reads memory that %s (at %s) could change before it", q, t.brief(o.Past), t.ws.Position(o.Past.Pos()))
	case o.Kind == analysis.Panics && strings.HasPrefix(o.PastDoes, "could panic"):
		return refactor.Refusef("%s %s, and %s (at %s) could panic before it", q, o.Does, t.brief(o.Past), t.ws.Position(o.Past.Pos()))
	default:
		return refactor.Refusef("%s %s, and %s (at %s) has side effects before it (it %s)", q, o.Does, t.brief(o.Past), t.ws.Position(o.Past.Pos()), o.PastDoes)
	}
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
	if end := slices.Index(path, ast.Node(t.decl.Body)); end >= 0 {
		path = path[:end+1]
	}
	times, why := analysis.Reach(path)
	if times == analysis.Once {
		return "", false
	}
	return "it is " + why, times == analysis.Several
}

// brief returns n's source for a message, shortened to its start if long.
func (t *target) brief(n ast.Node) string {
	s := strings.Join(strings.Fields(t.text(n)), " ")
	if len(s) > 40 {
		return s[:37] + "..."
	}
	return s
}
