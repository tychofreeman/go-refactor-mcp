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
//     `nil, err` itself. A string last result (a refusal) works the
//     same way, with "" for nil: each early return must give a string
//     known not to be empty.
//
// Anything else (an early return of a success value, say) is refused: the
// caller could not tell whether the statements returned or fell through.

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"regexp"
	"slices"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
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
	for _, n := range analysis.PathTo(x.File, x.stmts[0]) {
		switch n := n.(type) {
		case *ast.FuncLit:
			return n.Type, n.Body, typeOf[*types.Signature](x.Info.TypeOf(n))
		case *ast.FuncDecl:
			if obj, ok := x.Info.Defs[n.Name].(*types.Func); ok {
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
	term := analysis.TerminatingList(x.Info, x.stmts, nil)
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
		for id, obj := range x.Info.Uses {
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
	if n == 0 || !exitKind(sig.Results().At(n-1).Type()) {
		return refactor.Refusef("the selected lines return at line %d, but not on every path, and %s does not return an error, nor a string whose empty value could say it did not return. The new function could not tell the call site whether it returned; select statements that always return, or leave the return out",
			x.line(first.Pos()), x.enclosingName())
	}
	x.exitType = sig.Results().At(n - 1).Type()
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
		if !x.isError() && !x.exitNonZero(r.Results[n-1], r) {
			return refactor.Refusef("the return at line %d returns %s, which might be empty; the call site tells an early return from falling through by a string that is not empty. Only strings known not to be are passed on: a constant, fmt.Sprintf of a format with text besides its verbs, a variable checked against \"\" around the return, or a call of a function whose every return passes such a value",
				x.line(r.Pos()), x.quoteNode(r.Results[n-1]))
		}
		if x.isError() && !x.nonNilError(r.Results[n-1], r) {
			return refactor.Refusef("the return at line %d returns %s, which might be nil; the call site tells an early return from falling through by a non-nil error. Only errors that are known to be non-nil can be passed on: a variable checked with `!= nil` around the return, a value from errors.New or fmt.Errorf, or a call of a function whose every return passes such a value",
				x.line(r.Pos()), x.quoteNode(r.Results[n-1]))
		}
	}
	x.mode = errorExit
	var parts []string
	for _, e := range first.Results[:n-1] {
		parts = append(parts, string(x.Src[x.Off(e.Pos()):x.Off(e.End())]))
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
			if tv, ok := x.Info.Types[n.Fun]; !ok || !tv.IsType() {
				why = "a function call"
			}
		case *ast.UnaryExpr:
			if n.Op == token.ARROW {
				why = "a channel receive"
			}
		case *ast.Ident:
			if o := x.Info.Uses[n]; o != nil && isLocal(o) {
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
// not nil: a variable checked against nil around the return, or a value
// nonNilValue accepts.
func (x *extraction) nonNilError(e ast.Expr, r *ast.ReturnStmt) bool {
	e = ast.Unparen(e)
	if id, ok := e.(*ast.Ident); ok {
		if v, ok := x.Info.Uses[id].(*types.Var); ok && types.IsInterface(v.Type()) && x.checkedNonNil(v, r) {
			return true
		}
	}
	return x.nonNilValue(x.Info, e)
}

// exitKind reports whether t can carry an early exit as a function's last
// result: an error (non-nil means it returned), or a string, whose empty
// value means it did not (a refusal, say).
func exitKind(t types.Type) bool {
	if types.Identical(t, types.Universe.Lookup("error").Type()) {
		return true
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsString != 0 && b.Info()&types.IsUntyped == 0
}

// isError reports whether the early exits carry an error.
func (x *extraction) isError() bool {
	return types.Identical(x.exitType, types.Universe.Lookup("error").Type())
}

// exitZero spells the value of the last result that means the code did
// not return: nil for an error, the zero value otherwise.
func (x *extraction) exitZero() string {
	if x.isError() {
		return "nil"
	}
	z, err := x.zeroValue(x.exitType)
	if err != nil {
		return "nil"
	}
	return z
}

// exitNonZero reports whether e, returned by r as the last result, is
// certainly not the value that means the code did not return.
func (x *extraction) exitNonZero(e ast.Expr, r *ast.ReturnStmt) bool {
	if x.isError() {
		return x.nonNilError(e, r)
	}
	e = ast.Unparen(e)
	if id, ok := e.(*ast.Ident); ok {
		if v, ok := x.Info.Uses[id].(*types.Var); ok && x.checkedNonNil(v, r) {
			return true
		}
	}
	return x.nonZeroValue(x.Info, e)
}

// nonZeroValue reports whether e, a string or number type-checked in
// info, is certainly not zero without looking at the code around it: a
// constant that is not zero, fmt.Sprintf of a constant format with text
// besides its verbs, or a call of a function whose every return passes
// such a value (alwaysNonZero).
func (x *extraction) nonZeroValue(info *types.Info, e ast.Expr) bool {
	e = ast.Unparen(e)
	if tv, ok := info.Types[e]; ok && tv.Value != nil {
		return !isZeroConst(tv.Value)
	}
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	fn, ok := info.Uses[analysis.IdentOf(call.Fun)].(*types.Func)
	if !ok {
		return false
	}
	if fn.Pkg() != nil && fn.Pkg().Path() == "fmt" && fn.Name() == "Sprintf" && len(call.Args) > 0 {
		if tv, ok := info.Types[call.Args[0]]; ok && tv.Value != nil && tv.Value.Kind() == constant.String {
			return verbs.ReplaceAllString(constant.StringVal(tv.Value), "") != ""
		}
		return false
	}
	return x.alwaysNonZero(fn)
}

// verbs matches the verbs of a format and the doubled percent sign.
var verbs = regexp.MustCompile(`%[-+# 0]*(\[\d+\])?(\d+|\*)?(\.(\d+|\*)?)?(\[\d+\])?[a-zA-Z%]`)

// isZeroConst reports whether v is the zero value of its kind.
func isZeroConst(v constant.Value) bool {
	switch v.Kind() {
	case constant.String:
		return constant.StringVal(v) == ""
	case constant.Int, constant.Float, constant.Complex:
		return constant.Sign(v) == 0 && constant.Sign(constant.Imag(v)) == 0
	}
	return false
}

// alwaysNonZero reports whether every call of fn, a function in the
// loaded source with unnamed results that defers nothing, returns a last
// result that nonZeroValue accepts. A recursive call proves nothing.
func (x *extraction) alwaysNonZero(fn *types.Func) bool {
	fn = fn.Origin()
	if fn.Pkg() == nil {
		return false
	}
	if known, ok := x.nonNilFuncs[fn]; ok {
		return known
	}
	if x.nonNilFuncs == nil {
		x.nonNilFuncs = map[*types.Func]bool{}
	}
	x.nonNilFuncs[fn] = false // while deciding
	n := fn.Signature().Results().Len()
	info, decl := x.funcDecl(fn)
	if n == 0 || decl == nil || decl.Body == nil {
		return false
	}
	ok := everyReturn(info, decl, n, x.nonZeroValue)
	x.nonNilFuncs[fn] = ok
	return ok
}

// isString reports whether the early exits carry a string.
func (x *extraction) isString() bool {
	b, ok := x.exitType.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsString != 0
}

// nonNilValue reports whether e (type-checked in info), converted to an
// error, is certainly not nil without looking at the code around it: a
// call of a function that always returns a non-nil error (see
// alwaysNonNil), a pointer to a composite literal, or a value that is not
// a pointer, interface or other nilable type.
func (x *extraction) nonNilValue(info *types.Info, e ast.Expr) bool {
	e = ast.Unparen(e)
	if call, ok := e.(*ast.CallExpr); ok {
		if fn, ok := info.Uses[analysis.IdentOf(call.Fun)].(*types.Func); ok && x.alwaysNonNil(fn) {
			return true
		}
	}
	tv, ok := info.Types[e]
	if !ok || tv.IsNil() || tv.Type == nil {
		return false // the untyped nil is not a "non-pointer value"
	}
	switch tv.Type.Underlying().(type) {
	case *types.Interface:
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

// alwaysNonNil reports whether every call of fn returns a non-nil error as
// its last result: errors.New and fmt.Errorf do, and so does a function in
// the loaded source whose results are unnamed (a deferred function could
// otherwise change them) and whose every return passes a value that
// nonNilValue accepts, such as `return &Refusal{...}`, and that defers
// nothing (a deferred recover() returns the zero values instead). A
// function that calls itself proves nothing by doing so.
func (x *extraction) alwaysNonNil(fn *types.Func) bool {
	fn = fn.Origin()
	if fn.Pkg() == nil {
		return false
	}
	sig := fn.Signature()
	if sig.Recv() == nil {
		switch fn.Pkg().Path() + "." + fn.Name() {
		case "errors.New", "fmt.Errorf":
			return true
		}
	}
	if known, ok := x.nonNilFuncs[fn]; ok {
		return known
	}
	if x.nonNilFuncs == nil {
		x.nonNilFuncs = map[*types.Func]bool{}
	}
	x.nonNilFuncs[fn] = false // while deciding: a recursive call does not count
	n := sig.Results().Len()
	if n == 0 || !types.Identical(sig.Results().At(n-1).Type(), types.Universe.Lookup("error").Type()) {
		return false
	}
	info, decl := x.funcDecl(fn)
	if decl == nil || decl.Body == nil {
		return false
	}
	ok := everyReturn(info, decl, n, x.nonNilValue)
	x.nonNilFuncs[fn] = ok
	return ok
}

// everyReturn reports whether decl, a function of n unnamed results that
// defers nothing, returns a last result that accept takes at each return
// of its own (not of a function literal in it). `return g()` with several
// results is not looked into.
func everyReturn(info *types.Info, decl *ast.FuncDecl, n int, accept func(*types.Info, ast.Expr) bool) bool {
	for _, f := range decl.Type.Results.List {
		if len(f.Names) > 0 {
			return false
		}
	}
	ok := true
	ast.Inspect(decl.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.DeferStmt:
			ok = false // a deferred recover() returns the zero values: a nil error
		case *ast.ReturnStmt:
			if len(node.Results) != n || !accept(info, node.Results[n-1]) {
				ok = false
			}
		}
		return ok
	})
	return ok
}

// funcDecl finds fn's declaration in the loaded source, and the type
// information for the package that declares it.
func (x *extraction) funcDecl(fn *types.Func) (*types.Info, *ast.FuncDecl) {
	posn := x.WS.Fset.Position(fn.Pos())
	if !posn.IsValid() {
		return nil, nil
	}
	for _, p := range x.WS.Pkgs {
		if p.PkgPath != fn.Pkg().Path() || p.TypesInfo == nil {
			continue
		}
		for _, f := range p.Syntax {
			if x.WS.Fset.File(f.Pos()).Name() != posn.Filename {
				continue
			}
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if ok && fd.Name.Name == fn.Name() && x.WS.Fset.Position(fd.Name.Pos()).Line == posn.Line {
					return p.TypesInfo, fd
				}
			}
		}
	}
	return nil, nil
}

// checkedNonNil reports whether r is inside the body of an if statement
// whose condition requires v != nil, with no write to v in between (nor
// after r in the body, if a loop or goto there can run r again) and no
// other code that can change it.
func (x *extraction) checkedNonNil(v *types.Var, r *ast.ReturnStmt) bool {
	if x.fa.Escapes(v) {
		return false
	}
	again := false // r can run again after code later in the if body
	for _, n := range analysis.PathTo(x.File, r) {
		switch n := n.(type) {
		case *ast.FuncLit, *ast.FuncDecl:
			return false
		case *ast.ForStmt, *ast.RangeStmt:
			again = true
		case *ast.IfStmt:
			if !(n.Body.Pos() <= r.Pos() && r.End() <= n.Body.End()) || !x.requiresNonNil(n.Cond, v) {
				continue
			}
			again = again || hasGoto(n.Body)
			for _, m := range x.fa.Modified[v] {
				if m.Pos() >= n.Body.Pos() && (m.Pos() < r.Pos() || again && m.Pos() < n.Body.End()) {
					return false
				}
			}
			return true
		}
	}
	return false
}

// hasGoto reports whether n holds a goto outside function literals.
func hasGoto(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(c ast.Node) bool {
		if _, ok := c.(*ast.FuncLit); ok {
			return false
		}
		if b, ok := c.(*ast.BranchStmt); ok && b.Tok == token.GOTO {
			found = true
		}
		return !found
	})
	return found
}

// requiresNonNil reports whether cond can only be true when v != nil (or
// v is not its zero value, "" or 0): it is `v != nil` (either way round)
// or a && chain containing that.
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
			return ok && x.Info.Uses[id] == v
		}
		isNil := func(e ast.Expr) bool {
			tv := x.Info.Types[e]
			return tv.IsNil() || tv.Value != nil && isZeroConst(tv.Value)
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
	base := "err"
	if !x.isError() {
		base = "why"
		if _, _, sig := x.enclosingFunc(); sig != nil {
			if v := sig.Results().At(sig.Results().Len() - 1); v.Name() != "" && v.Name() != "_" {
				base = v.Name()
			}
		}
	}
	name := base
	for i := 2; !free(name); i++ {
		name = fmt.Sprintf("%s%d", base, i)
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
		start, end := x.Off(r.Results[0].Pos()), x.Off(r.Results[n-1].Pos())
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
			if t.off >= x.Off(e.Pos()) && t.end <= x.Off(e.End()) && t.tok != token.COMMENT {
				out = append(out, string(x.Src[t.off:t.end]))
			}
		}
		out = append(out, ",")
	}
	return out
}

func (x *extraction) quoteNode(n ast.Node) string {
	return quoteText(string(x.Src[x.Off(n.Pos()):x.Off(n.End())]))
}

// finalReturn returns the statement after the selection when the call
// site can return in its place: the selection is an error exit with no
// other results, and that statement is "return <exit values>, nil", with
// no comment in between. Then "return <exit values>, f(...)" does what the
// call, its check and that return did together.
func (x *extraction) finalReturn(f *flow) *ast.ReturnStmt {
	if x.mode != errorExit || len(f.results) > 0 {
		return nil
	}
	last := x.stmts[len(x.stmts)-1]
	i := slices.Index(x.list, last)
	if i < 0 || i+1 >= len(x.list) {
		return nil
	}
	r, ok := x.list[i+1].(*ast.ReturnStmt)
	if !ok || len(r.Results) != len(x.exitValues)+1 {
		return nil
	}
	if tv := x.Info.Types[r.Results[len(r.Results)-1]]; !tv.IsNil() && (tv.Value == nil || !isZeroConst(tv.Value)) {
		return nil
	}
	for j, v := range x.exitValues {
		got, err1 := locate.Tokens(x.Src[x.Off(r.Results[j].Pos()):x.Off(r.Results[j].End())])
		want, err2 := locate.Tokens([]byte(v))
		if err1 != nil || err2 != nil || !locate.EqualTokens(got, want) {
			return nil
		}
	}
	for _, cg := range x.File.Comments {
		if cg.Pos() >= last.End() && cg.Pos() < r.End() {
			return nil
		}
	}
	return r
}

// identsIn returns the identifiers in the Go source src.
func identsIn(src string) []string {
	var out []string
	for _, t := range scanTokens([]byte(src)) {
		if t.tok == token.IDENT {
			out = append(out, src[t.off:t.end])
		}
	}
	return out
}
