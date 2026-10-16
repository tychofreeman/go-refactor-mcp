package extractfunc

// Return statements in the selection.
//
// A return in the selected statements would return from the new function
// instead of the enclosing one, so the call site has to pass it on. Two
// shapes can be passed on without changing behavior:
//
//   - terminal: every path through the statements ends in a return (or
//     another terminating statement, such as panic). The new function gets
//     the enclosing function's results and the statements move unchanged;
//     the call site becomes `return f(...)`.
//
//   - error exit: the enclosing function's last result is an error, and
//     every return in the statements is an early exit with a non-nil error
//     and the same constant-like values for the other results:
//
//         if err != nil {
//             return nil, err
//         }
//
//     The new function returns its results and an error; the returns
//     become `return <zero results>, err`, falling off the end returns
//     `..., nil`, and the call site checks the error and returns
//     `nil, err` itself.
//
// Anything else (an early return of a success value, say) is refused: the
// caller could not tell whether the statements returned or fell through.

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
)

type returnMode int

const (
	noReturns returnMode = iota
	terminal             // every path returns: call site is `return f(...)`
	errorExit            // early returns only with an error: call site checks it
)

// enclosingFunc returns the innermost function containing the selection:
// the one its return statements return from.
func (x *extraction) enclosingFunc() (ft *ast.FuncType, body *ast.BlockStmt, sig *types.Signature) {
	for _, n := range analysis.PathTo(x.file, x.stmts[0]) {
		switch n := n.(type) {
		case *ast.FuncLit:
			return n.Type, n.Body, typeOf[*types.Signature](x.info.TypeOf(n))
		case *ast.FuncDecl:
			if obj, ok := x.info.Defs[n.Name].(*types.Func); ok {
				return n.Type, n.Body, obj.Signature()
			}
			return n.Type, n.Body, nil
		}
	}
	return nil, nil, nil
}

// decideReturns chooses how the statements' return statements (and
// terminating statements) are passed on, or refuses.
func (x *extraction) decideReturns() error {
	_, _, sig := x.enclosingFunc()
	if sig == nil {
		return fmt.Errorf("internal error: no signature for the function containing line %d", x.line(x.stmts[0].Pos()))
	}
	term := x.terminatingList(x.stmts, false)
	needed := sig.Results().Len() > 0 && x.checkTermination() != nil
	switch {
	case len(x.returns) == 0 && !needed:
		x.mode = noReturns
		return nil
	case term:
		if err := x.checkNamedResults(sig); err != nil {
			return err
		}
		for i := 0; i < sig.Results().Len(); i++ {
			if _, err := x.typeString(sig.Results().At(i).Name(), sig.Results().At(i).Type()); err != nil {
				return err
			}
		}
		x.mode = terminal
		return nil
	}
	return x.checkErrorExits(sig)
}

// checkNamedResults refuses statements that use the enclosing function's
// named results or return without values: the new function has results of
// its own.
func (x *extraction) checkNamedResults(sig *types.Signature) error {
	for _, r := range x.returns {
		if len(r.Results) == 0 && sig.Results().Len() > 0 {
			return refactor.Refusef("the return at line %d returns %s's named results implicitly; the new function would have to share them. Write the values out in the return, or leave it out of the selection",
				x.line(r.Pos()), x.enclosingName())
		}
	}
	for i := 0; i < sig.Results().Len(); i++ {
		v := sig.Results().At(i)
		if v.Name() == "" || v.Name() == "_" {
			continue
		}
		for id, obj := range x.info.Uses {
			if obj == v && x.inSelection(id) {
				return refactor.Refusef("the selected lines return from %s and use its named result %s (line %d); the new function would have its own results, so returning through %s is not supported",
					x.enclosingName(), v.Name(), x.line(id.Pos()), v.Name())
			}
		}
	}
	return nil
}

// checkErrorExits accepts returns that are all early exits with a non-nil
// error and the same constant-like other values, in a function whose last
// result is an error.
func (x *extraction) checkErrorExits(sig *types.Signature) error {
	first := x.returns[0]
	n := sig.Results().Len()
	if n == 0 || !types.Identical(sig.Results().At(n-1).Type(), types.Universe.Lookup("error").Type()) {
		return refactor.Refusef("the selected lines return at line %d, but not on every path, and %s does not return an error. The new function could not tell the call site whether it returned; select statements that always return, or leave the return out",
			x.line(first.Pos()), x.enclosingName())
	}
	if err := x.checkNamedResults(sig); err != nil {
		return err
	}
	var want []string // the non-error results' tokens, which must match
	for _, r := range x.returns {
		if len(r.Results) != n {
			return refactor.Refusef("the return at line %d returns the results of a call; only returns that write out each value (like `return nil, err`) can be passed on",
				x.line(r.Pos()))
		}
		for _, e := range r.Results[:n-1] {
			if why := x.notConstantLike(e); why != "" {
				return refactor.Refusef("the return at line %d returns %s %s; only early returns of an error with constant values for the other results (like `return nil, err`) can be passed on, because the call site must return the same values",
					x.line(r.Pos()), x.quoteNode(e), why)
			}
		}
		toks := x.tokenText(r.Results[:n-1])
		if want == nil {
			want = toks
		} else if strings.Join(want, " ") != strings.Join(toks, " ") {
			return refactor.Refusef("the returns at lines %d and %d return different values besides the error; the call site can only return one set of values", x.line(first.Pos()), x.line(r.Pos()))
		}
		if !x.nonNilError(r.Results[n-1], r) {
			return refactor.Refusef("the return at line %d returns %s, which might be nil; the call site tells an early return from falling through by a non-nil error. Only errors that are known to be non-nil can be passed on: a variable checked with `!= nil` around the return, or a value from errors.New or fmt.Errorf",
				x.line(r.Pos()), x.quoteNode(r.Results[n-1]))
		}
	}
	x.mode = errorExit
	var parts []string
	for _, e := range first.Results[:n-1] {
		parts = append(parts, string(x.src[x.off(e.Pos()):x.off(e.End())]))
	}
	x.exitValues = parts
	return nil
}

// notConstantLike explains why e cannot be repeated at the call site, or
// returns "": it must not call anything (other than conversions) or use
// local names.
func (x *extraction) notConstantLike(e ast.Expr) string {
	why := ""
	ast.Inspect(e, func(n ast.Node) bool {
		if why != "" {
			return false
		}
		switch n := n.(type) {
		case *ast.FuncLit:
			why = "a function literal"
		case *ast.CallExpr:
			if tv, ok := x.info.Types[n.Fun]; !ok || !tv.IsType() {
				why = "a function call"
			}
		case *ast.UnaryExpr:
			if n.Op == token.ARROW {
				why = "a channel receive"
			}
		case *ast.Ident:
			if o := x.info.Uses[n]; o != nil && isLocal(o) {
				why = fmt.Sprintf("a local %s, %s", kindOf(o), n.Name)
			}
		}
		return true
	})
	if why != "" {
		return "(" + why + ")"
	}
	return ""
}

// nonNilError reports whether e, returned by r as an error, is certainly
// not nil.
func (x *extraction) nonNilError(e ast.Expr, r *ast.ReturnStmt) bool {
	e = ast.Unparen(e)
	if call, ok := e.(*ast.CallExpr); ok {
		if fn, ok := x.info.Uses[analysis.IdentOf(call.Fun)].(*types.Func); ok && fn.Pkg() != nil {
			switch fn.Pkg().Path() + "." + fn.Name() {
			case "errors.New", "fmt.Errorf":
				return true
			}
		}
	}
	tv, ok := x.info.Types[e]
	if !ok || tv.IsNil() || tv.Type == nil {
		return false // the untyped nil is not a "non-pointer value"
	}
	t := tv.Type
	switch t.Underlying().(type) {
	case *types.Interface:
		// A variable checked against nil around the return.
		if id, ok := e.(*ast.Ident); ok {
			if v, ok := x.info.Uses[id].(*types.Var); ok {
				return x.checkedNonNil(v, r)
			}
		}
		return false
	case *types.Pointer:
		if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
			_, lit := ast.Unparen(u.X).(*ast.CompositeLit)
			return lit
		}
		return false
	case *types.Map, *types.Slice, *types.Chan, *types.Signature:
		return false
	}
	return true // a non-pointer value converted to error is never nil
}

// checkedNonNil reports whether r is inside the body of an if statement
// whose condition requires v != nil, with no write to v in between and no
// other code that can change it.
func (x *extraction) checkedNonNil(v *types.Var, r *ast.ReturnStmt) bool {
	if x.fa.Escapes(v) {
		return false
	}
	for _, n := range analysis.PathTo(x.file, r) {
		switch n := n.(type) {
		case *ast.FuncLit, *ast.FuncDecl:
			return false
		case *ast.IfStmt:
			if !(n.Body.Pos() <= r.Pos() && r.End() <= n.Body.End()) || !x.requiresNonNil(n.Cond, v) {
				continue
			}
			for _, m := range x.fa.Modified[v] {
				if m.Pos() >= n.Body.Pos() && m.Pos() < r.Pos() {
					return false
				}
			}
			return true
		}
	}
	return false
}

// requiresNonNil reports whether cond can only be true when v != nil:
// it is `v != nil` (either way round) or a && chain containing that.
func (x *extraction) requiresNonNil(cond ast.Expr, v *types.Var) bool {
	b, ok := ast.Unparen(cond).(*ast.BinaryExpr)
	if !ok {
		return false
	}
	switch b.Op {
	case token.LAND:
		return x.requiresNonNil(b.X, v) || x.requiresNonNil(b.Y, v)
	case token.NEQ:
		isV := func(e ast.Expr) bool {
			id, ok := ast.Unparen(e).(*ast.Ident)
			return ok && x.info.Uses[id] == v
		}
		isNil := func(e ast.Expr) bool {
			return x.info.Types[e].IsNil()
		}
		return isV(b.X) && isNil(b.Y) || isNil(b.X) && isV(b.Y)
	}
	return false
}

// zeroValue spells the zero value of t, for the early exits of the new
// function's own results.
func (x *extraction) zeroValue(t types.Type) (string, error) {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch {
		case u.Info()&types.IsBoolean != 0:
			return "false", nil
		case u.Info()&types.IsString != 0:
			return `""`, nil
		case u.Info()&types.IsNumeric != 0:
			return "0", nil
		case u.Kind() == types.UnsafePointer:
			return "nil", nil
		}
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan, *types.Signature, *types.Interface:
		return "nil", nil
	case *types.Struct, *types.Array:
		s, err := x.typeString("", t)
		if err != nil {
			return "", err
		}
		return s + "{}", nil
	}
	return "", fmt.Errorf("internal error: no zero value for %s", t)
}

// errorName picks a name for the error variable at the call site: one
// that nothing in scope there uses, other than names that move with the
// selection.
func (x *extraction) errorName(results []*types.Var) string {
	taken := map[string]bool{x.name: true}
	for _, v := range append(append([]*types.Var(nil), x.flow.params...), results...) {
		taken[v.Name()] = true
	}
	scope := x.innermostScope(x.stmts[0].Pos())
	free := func(name string) bool {
		if taken[name] {
			return false
		}
		for s := scope; s != nil; s = s.Parent() {
			if o := s.Lookup(name); o != nil && !x.inSelectionPos(o.Pos()) {
				return false
			}
		}
		return true
	}
	name := "err"
	for i := 2; !free(name); i++ {
		name = fmt.Sprintf("err%d", i)
	}
	return name
}

// rewriteExits returns the edits that turn each early return into
// `return <zero results>, <error>` in the new function. It replaces the
// values before the error and leaves the error expression itself alone,
// so that other edits (pointer parameters) can still apply inside it.
func (x *extraction) rewriteExits(zeros []string) []edit.TextEdit {
	var edits []edit.TextEdit
	for _, r := range x.returns {
		n := len(r.Results)
		text := strings.Join(zeros, ", ")
		if text != "" {
			text += ", "
		}
		start, end := x.off(r.Results[0].Pos()), x.off(r.Results[n-1].Pos())
		if start == end && text == "" {
			continue
		}
		edits = append(edits, edit.TextEdit{Start: start, End: end, NewText: text})
	}
	return edits
}

// tokenText returns the tokens of the given expressions, so that
// differences in spacing do not matter.
func (x *extraction) tokenText(es []ast.Expr) []string {
	var out []string
	for _, e := range es {
		for _, t := range x.tokens() {
			if t.off >= x.off(e.Pos()) && t.end <= x.off(e.End()) && t.tok != token.COMMENT {
				out = append(out, string(x.src[t.off:t.end]))
			}
		}
		out = append(out, ",")
	}
	return out
}

func (x *extraction) quoteNode(n ast.Node) string {
	return quoteText(string(x.src[x.off(n.Pos()):x.off(n.End())]))
}
