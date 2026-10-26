package inlinefunc

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
)

// param is one parameter (or the receiver) and its argument.
type param struct {
	v    *types.Var
	name string   // "_" for unnamed and blank parameters
	arg  ast.Expr // the argument; nil for a parameter fed by a multi-value call
	addr ast.Expr // when the argument is &V for a variable V (explicitly or by an implicit &), V
	text string   // the argument as written in the caller (with & or * for an implicit receiver conversion)
	conv string   // the parameter's type, when the argument must be converted to it
	uses []*ast.Ident

	subst bool // uses are replaced by the argument
	bind  bool // declared at the top of the inlined code: name := arg
	drop  bool // unused, and the argument has no effects
	// keep: unused, but the argument has effects: _ = arg (bind with name "_")
}

// argument classes, from most to least freely substitutable.
const (
	stableArg = iota // cannot change and has no effects
	readArg          // a variable read: no effects, but other code may change it
	panicArg         // reads through pointers, indexes: no effects, but may panic
	effectArg        // calls or receives
	constArg         // a constant: substituting it can change constant arithmetic
)

func (x *inliner) decideParams() error {
	name := x.fn.Name()
	var tuple bool
	if r := x.sig.Recv(); r != nil {
		p := &param{v: r, name: paramName(r)}
		sel := x.call.Fun.(*ast.SelectorExpr)
		recvPtr, xPtr := analysis.IsPointer(r.Type()), analysis.IsPointer(x.info.TypeOf(sel.X))
		switch {
		case recvPtr == xPtr:
			p.arg, p.text = sel.X, x.text(sel.X)
			if u, ok := ast.Unparen(sel.X).(*ast.UnaryExpr); ok && u.Op == token.AND && x.isVarPath(u.X) {
				p.addr = u.X
			}
		case recvPtr: // x.m() with m on *T and x addressable: &x
			p.arg, p.text = sel.X, "&"+x.parenText(sel.X)
			if x.isVarPath(sel.X) {
				p.addr = sel.X
			}
		default: // p.m() with m on T and p a pointer: *p
			p.arg, p.text = sel.X, "*"+x.parenText(sel.X)
		}
		x.params = append(x.params, p)
	}
	params := x.sig.Params()
	if len(x.call.Args) == 1 && params.Len() > 1 {
		tuple = true
		tup, _ := x.info.TypeOf(x.call.Args[0]).(*types.Tuple)
		if x.shape == expr || tup == nil {
			return refactor.Refusef("the call to %q passes the results of %s as its arguments; this can only be inlined as a statement", name, x.quote(x.call.Args[0]))
		}
		for i := 0; i < params.Len(); i++ {
			if !types.Identical(tup.At(i).Type(), params.At(i).Type()) {
				return refactor.Refusef("the call to %q passes the results of %s, whose types differ from the parameters'; this is not supported", name, x.quote(x.call.Args[0]))
			}
		}
	}
	for i := 0; i < params.Len(); i++ {
		v := params.At(i)
		p := &param{v: v, name: paramName(v)}
		if !tuple {
			a := x.call.Args[i]
			p.arg, p.text = a, x.text(a)
			if u, ok := ast.Unparen(a).(*ast.UnaryExpr); ok && u.Op == token.AND && x.isVarPath(u.X) {
				p.addr = u.X
			}
		}
		x.params = append(x.params, p)
	}
	for _, p := range x.params {
		if p.name == "_" {
			continue
		}
		for id, o := range x.info.Uses {
			if o == p.v && x.inDecl(id.Pos()) {
				p.uses = append(p.uses, id)
			}
		}
		sortIdents(p.uses)
	}
	if tuple {
		for _, p := range x.params {
			if p.v != x.sig.Recv() {
				p.bind = true
			}
		}
	}
	for _, p := range x.params {
		if p.arg == nil || p.bind {
			continue
		}
		if err := x.decideParam(p); err != nil {
			return err
		}
	}
	// Bound arguments are evaluated before the body, in order. If one of
	// them has effects, a variable another argument reads could change
	// first, so those are bound too.
	effects := false
	for _, p := range x.params {
		if p.bind && p.arg != nil && x.classify(p) != stableArg {
			effects = true
		}
	}
	for _, p := range x.params {
		if c := x.classify(p); effects && p.subst && p.addr == nil && (c == readArg || c == panicArg) {
			p.subst, p.bind = false, true
		}
	}
	// A substituted argument must not be captured by a name the body
	// declares (or a parameter that stays declared) where it is used.
	for changed := true; changed; {
		changed = false
		for _, p := range x.params {
			if !p.subst {
				continue
			}
			if c := x.capture(p); c != "" {
				if x.shape == expr {
					return refactor.Refusef("the argument %s for %s would be captured by %s in the body of %q; rename it first", x.quote(p.arg), p.name, c, name)
				}
				p.subst, p.bind, changed = false, true, true
			}
		}
	}
	for p := x.vetClash(); p != nil; p = x.vetClash() {
		if x.shape == expr {
			return refactor.Refusef("%q gets the same argument %s for two parameters, and inlining would write expressions that go vet rejects (identical operands of && or ||, or a self-assignment); assign the argument to a variable first",
				x.fn.Name(), x.quote(p.arg))
		}
		p.subst, p.bind = false, true
	}
	if x.shape == expr {
		return x.checkExprOrder()
	}
	return nil
}

// vetClash returns a substituted parameter that should stay a variable
// because it gets the same argument as another one and substituting both
// would produce code go vet rejects: "a && a", "a || a" (also in longer
// chains) or "x = x". Returns nil if there is none.
func (x *inliner) vetClash() *param {
	byText := map[string]*param{}
	var dup []*param
	for _, p := range x.params {
		if !p.subst || p.arg == nil {
			continue
		}
		t := strings.Join(strings.Fields(p.text), "")
		if byText[t] != nil {
			dup = append(dup, p)
		}
		byText[t] = p
	}
	if len(dup) == 0 {
		return nil
	}
	norm := func(n ast.Node) string {
		return strings.Join(strings.Fields(x.rewrite(x.doff(n.Pos()), x.doff(n.End()))), "")
	}
	orig := func(n ast.Node) string { return strings.Join(strings.Fields(x.dtext(n)), "") }
	bad := false
	ast.Inspect(x.decl.Body, func(n ast.Node) bool {
		if bad {
			return false
		}
		switch n := n.(type) {
		case *ast.BinaryExpr:
			if n.Op != token.LAND && n.Op != token.LOR {
				return true
			}
			ops := flatten(n, n.Op)
			seen, seenOrig := map[string]bool{}, map[string]bool{}
			for _, o := range ops {
				t, ot := norm(o), orig(o)
				if seen[t] && !seenOrig[ot] {
					bad = true
				}
				seen[t], seenOrig[ot] = true, true
			}
			// vet's "suspect or/and": x != c1 || x != c2, x == c1 && x == c2.
			cmp := token.NEQ
			if n.Op == token.LAND {
				cmp = token.EQL
			}
			consts, constsOrig := map[string]string{}, map[string]string{}
			for _, o := range ops {
				b, ok := ast.Unparen(o).(*ast.BinaryExpr)
				if !ok || b.Op != cmp {
					continue
				}
				e, c := b.X, b.Y
				if x.info.Types[e].Value != nil {
					e, c = c, e
				}
				if x.info.Types[c].Value == nil {
					continue
				}
				t, ot, ct := norm(e), orig(e), x.info.Types[c].Value.ExactString()
				if prev, ok := consts[t]; ok && prev != ct && constsOrig[ot] == "" {
					bad = true
				}
				consts[t], constsOrig[ot] = ct, ct
			}
		case *ast.AssignStmt:
			if len(n.Lhs) == len(n.Rhs) {
				for i := range n.Lhs {
					if norm(n.Lhs[i]) == norm(n.Rhs[i]) && orig(n.Lhs[i]) != orig(n.Rhs[i]) {
						bad = true
					}
				}
			}
		}
		return !bad
	})
	if !bad {
		return nil
	}
	return dup[len(dup)-1]
}

// flatten returns the operands of a chain of op (a && b && c).
func flatten(e ast.Expr, op token.Token) []ast.Expr {
	if b, ok := ast.Unparen(e).(*ast.BinaryExpr); ok && b.Op == op {
		return append(flatten(b.X, op), flatten(b.Y, op)...)
	}
	return []ast.Expr{e}
}

// decideParam chooses between substituting, binding and dropping.
func (x *inliner) decideParam(p *param) error {
	class := x.classify(p)
	if len(p.uses) == 0 {
		switch {
		case p.addr != nil || class == stableArg || class == readArg || class == constArg:
			p.drop = true
		case x.shape == expr:
			return refactor.Refusef("the argument %s has effects but %q never uses parameter %s; inlined in an expression it would be dropped", x.quote(p.arg), x.fn.Name(), p.name)
		default:
			p.bind, p.name = true, "_"
		}
		return nil
	}
	if p.addr == nil && p.text == x.text(p.arg) && !x.sameType(p.arg, p.v.Type()) {
		t, err := x.typeString(p.v.Type())
		if err != nil {
			return err
		}
		p.conv = t
	}
	modified := len(x.calleeFA.Modified[p.v]) > 0 || x.calleeFA.Addressed[p.v]
	if p.addr != nil {
		// The address of a variable never changes: uses become V (or &V).
		if modified {
			return x.mustBind(p, "is assigned in the body")
		}
		p.subst = true
		return nil
	}
	if modified && !x.calleeFA.Escapes(p.v) && (x.updatedInPlace(p) || x.deadArg(p)) {
		p.subst = true // the body may change the caller's variable directly
		return nil
	}
	if !modified && x.calleeFA.Captured[p.v] && x.neverChanges(p.arg) {
		p.subst = true // a closure in the body may read it later, but it never changes
		return nil
	}
	if modified || x.calleeFA.Captured[p.v] {
		why := "is assigned in the body"
		if !modified {
			why = "is used in a function literal in the body, which could run after the argument's variables change"
		}
		return x.mustBind(p, why)
	}
	switch class {
	case stableArg:
		p.subst = true
	case readArg:
		if x.neverChanges(p.arg) || x.effectBefore(p) == nil {
			p.subst = true
		} else if x.shape == expr {
			return refactor.Refusef("the argument %s could change before %q uses it (it reads a variable other code can change, and the body has calls); assign it to a variable first", x.quote(p.arg), x.fn.Name())
		} else {
			p.bind = true
		}
	case constArg:
		for _, u := range p.uses {
			if !x.wholeOperand(analysis.PathTo(x.decl, u)) {
				if x.shape != expr {
					p.bind = true
					return nil
				}
				return refactor.Refusef("the argument %s is a constant, and %q uses %s in arithmetic at %s, where a constant can behave differently from a variable (overflow, division by zero); assign it to a variable first",
					x.quote(p.arg), x.fn.Name(), p.name, x.ws.Position(u.Pos()))
			}
		}
		p.subst = true
	case panicArg:
		if x.effectBefore(p) == nil && x.firstUseUnconditional(p) {
			p.subst = true
		} else if x.shape == expr {
			return refactor.Refusef("the argument %s could change or fail differently once moved into %q (the body has calls before using it, or uses it conditionally); assign it to a variable first", x.quote(p.arg), x.fn.Name())
		} else {
			p.bind = true
		}
	case effectArg:
		if x.shape != expr {
			p.bind = true
			return nil
		}
		if len(p.uses) != 1 {
			return refactor.Refusef("the argument %s has effects (because of calls or channel operations) but %q uses %s %d times; inlined in an expression it would run %d times",
				x.quote(p.arg), x.fn.Name(), p.name, len(p.uses), len(p.uses))
		}
		path := analysis.PathTo(x.decl, p.uses[0])
		for i := 1; i < len(path); i++ {
			if why := conditionalOrRepeated(path[i], path[i-1]); why != "" {
				return refactor.Refusef("the argument %s has effects, and %q uses %s %s; inlined, the argument would not run exactly once",
					x.quote(p.arg), x.fn.Name(), p.name, why)
			}
		}
		p.subst = true
	}
	return nil
}

// updatedInPlace reports whether parameter p, which the body assigns, can
// still be replaced by its argument: the argument is a local variable V
// that nothing else can reach, the body returns p, and the call assigns
// that result back to V ("v = step(v)"). Changing V directly then has the
// same effect, as long as no other argument reads V.
func (x *inliner) updatedInPlace(p *param) bool {
	as, ok := x.stmt.(*ast.AssignStmt)
	if x.shape != assign || !ok || len(x.results) != len(as.Lhs) || x.calleeFA.Escapes(p.v) {
		return false
	}
	aid, ok := ast.Unparen(p.arg).(*ast.Ident)
	if !ok {
		return false
	}
	v, ok := x.info.Uses[aid].(*types.Var)
	if !ok || !x.callerFA.IsLocal(v) || x.callerFA.Escapes(v) {
		return false
	}
	back := false
	for i, l := range as.Lhs {
		lid, ok := l.(*ast.Ident)
		if !ok || x.info.ObjectOf(lid) != v {
			continue
		}
		rid, ok := ast.Unparen(x.results[i]).(*ast.Ident)
		if !ok || x.info.Uses[rid] != p.v || back {
			return false
		}
		back = true
	}
	if !back {
		return false
	}
	for _, q := range x.params {
		if q == p || q.arg == nil {
			continue
		}
		for _, id := range analysis.ScopeRefs(x.info, q.arg) {
			if x.info.Uses[id] == v {
				return false
			}
		}
	}
	return true
}

// deadArg reports whether p's argument is a local variable of the same
// name that the caller never reads after the call and that no other
// argument reads, so the body may change it instead of a copy. (With a
// different name, "p := arg" reads better than changing arg in place.)
func (x *inliner) deadArg(p *param) bool {
	id, ok := ast.Unparen(p.arg).(*ast.Ident)
	if !ok || id.Name != p.name {
		return false
	}
	v, ok := x.info.Uses[id].(*types.Var)
	if !ok || !x.deadAfter(v) {
		return false
	}
	for _, q := range x.params {
		if q != p && q.arg != nil {
			for _, r := range analysis.ScopeRefs(x.info, q.arg) {
				if x.info.Uses[r] == v {
					return false
				}
			}
		}
	}
	return true
}

// neverChanges reports whether e is a local variable that is never
// assigned after its declaration and whose address is never taken.
func (x *inliner) neverChanges(e ast.Expr) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return false
	}
	v, ok := x.info.Uses[id].(*types.Var)
	return ok && x.callerFA.IsLocal(v) && len(x.callerFA.Modified[v]) == 0 && !x.callerFA.Addressed[v]
}

func (x *inliner) mustBind(p *param, why string) error {
	if x.shape == expr {
		return refactor.Refusef("parameter %s of %q %s, so it must stay a variable, which is not possible inside an expression; assign the call to a variable first", p.name, x.fn.Name(), why)
	}
	p.bind = true
	return nil
}

// classify sorts an argument by how freely it can be moved into the body:
// by what evaluating it does.
func (x *inliner) classify(p *param) int {
	if p.arg == nil {
		return effectArg
	}
	if p.text != x.text(p.arg) { // an implicit & or *
		if p.addr != nil {
			return stableArg
		}
		return effectArg
	}
	if tv := x.info.Types[p.arg]; tv.Value != nil {
		return constArg
	}
	s := x.ws.Effects().Of(x.callerFA, p.arg)
	switch {
	case s.Effect != "", s.Makes != "" && len(p.uses) > 1: // one new value would stand for several
		return effectArg
	case s.Panics != analysis.NoPanic:
		return panicArg
	case s.Reads != "":
		return readArg
	}
	return stableArg
}

// firstUseUnconditional reports whether p's first use in the body is
// evaluated whenever the body runs (not in a nested block, an else if, a
// for loop's post statement, a function literal or the right side of &&
// or ||), so a panic the argument would
// have raised at the call still happens, before anything else.
func (x *inliner) firstUseUnconditional(p *param) bool {
	path := analysis.PathTo(x.decl.Body, p.uses[0])
	for i := 1; i < len(path)-1; i++ {
		switch n := path[i].(type) {
		case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause, *ast.FuncLit:
			return false
		case *ast.IfStmt: // path runs from the use outward
			if outer, ok := path[i+1].(*ast.IfStmt); ok && outer.Else == n {
				return false // an else if
			}
		case *ast.ForStmt:
			if n.Post != nil && path[i-1] == n.Post {
				return false // runs only after a pass
			}
		case *ast.BinaryExpr:
			if (n.Op == token.LAND || n.Op == token.LOR) && path[i-1] == n.Y {
				return false
			}
		}
	}
	return true
}

// isVarPath reports whether e is a variable or a field of one reached
// without pointer indirection: evaluating it has no effects and cannot
// panic, and its address is fixed.
func (x *inliner) isVarPath(e ast.Expr) bool {
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		_, ok := x.info.Uses[e].(*types.Var)
		return ok
	case *ast.SelectorExpr:
		s := x.info.Selections[e]
		return s != nil && s.Kind() == types.FieldVal && !s.Indirect() && !analysis.IsPointer(x.info.TypeOf(e.X)) && x.isVarPath(e.X)
	}
	return false
}

// effectBefore returns something in the body that could change the
// argument's value or panic before p's last use (counting a use in a loop
// as the end of the loop), or nil: the argument moves into the body,
// past everything evaluated before that point and past the rest of each
// use's own statement, since Go does not order a plain read against a
// call next to it.
func (x *inliner) effectBefore(p *param) ast.Node {
	last := token.NoPos
	var past []ast.Node
	for _, u := range p.uses {
		end := u.End()
		var stmt ast.Node
		for _, n := range analysis.PathTo(x.decl.Body, u) {
			switch n.(type) {
			case *ast.ForStmt, *ast.RangeStmt:
				end = max(end, n.End())
			}
			if _, ok := n.(ast.Stmt); ok && stmt == nil {
				stmt = n
			}
		}
		if end == u.End() && stmt != nil {
			past = append(past, analysis.EvaluatedAfter(stmt, u.End())...)
		}
		last = max(last, end)
	}
	past = append(analysis.EvaluatedBefore(x.decl.Body, last, nil), past...)
	m := analysis.Move{Effects: x.ws.Effects(), Func: x.callerFA, Expr: p.arg, Past: past, PastFunc: x.calleeFA}
	if o := m.Check(); o != nil {
		return o.Past
	}
	return nil
}

// capture returns what would capture a name in p's argument at one of p's
// uses (a local of the body, a parameter that stays declared, or a renamed
// local), or "".
func (x *inliner) capture(p *param) string {
	e := p.arg
	if p.addr != nil {
		e = p.addr
	}
	refs := analysis.ScopeRefs(x.info, e)
	for _, u := range p.uses {
		scope := x.pkg.Types.Scope().Innermost(u.Pos())
		if scope == nil {
			continue
		}
		for _, id := range refs {
			if _, o := scope.LookupParent(id.Name, u.Pos()); o != nil && x.inDecl(o.Pos()) {
				if q := x.paramOf(o); q != nil && q.subst {
					continue // replaced too
				}
				return fmt.Sprintf("the %s %s declared at %s", analysis.KindOf(o), id.Name, x.ws.Position(o.Pos()))
			}
			for v, to := range x.merged {
				if to.Name == id.Name && v.Pos() < u.Pos() {
					return fmt.Sprintf("the variable %s (renamed from %s)", to.Name, v.Name())
				}
			}
		}
	}
	return ""
}

func (x *inliner) paramOf(o types.Object) *param {
	for _, p := range x.params {
		if p.v == o {
			return p
		}
	}
	return nil
}

// checkExprOrder ensures arguments with effects still run in their order
// when substituted into the result expression, and that the expression has
// no effects of its own to reorder them against.
func (x *inliner) checkExprOrder() error {
	var effectful []*param
	for _, p := range x.params {
		if p.subst && x.classify(p) == effectArg {
			effectful = append(effectful, p)
		}
	}
	if len(effectful) == 0 {
		return nil
	}
	for _, p := range x.params {
		if c := x.classify(p); p.subst && (c == readArg || c == panicArg) {
			return refactor.Refusef("the argument %s reads a variable that the argument %s could change; inlining would change the order. Assign the call to a variable first",
				x.quote(p.arg), x.quote(effectful[0].arg))
		}
	}
	for i := 1; i < len(effectful); i++ {
		if effectful[i].uses[0].Pos() < effectful[i-1].uses[0].Pos() {
			return refactor.Refusef("the arguments %s and %s have effects, and %q uses them in the opposite order; inlining would run them in a different order",
				x.quote(effectful[i-1].arg), x.quote(effectful[i].arg), x.fn.Name())
		}
	}
	if own := x.ws.Effects().Of(x.calleeFA, x.results[0]).At.Effect; own != nil {
		return refactor.Refusef("%q's result %s has calls of its own, and the argument %s has effects; inlining could change their order. Assign the call to a variable first",
			x.fn.Name(), x.dquote(own), x.quote(effectful[0].arg))
	}
	return nil
}

// wholeOperand reports whether the use at path[0] is a whole value passed
// somewhere (an argument, a result, an assigned value), not an operand of
// arithmetic, a conversion, an index or a shift, where a constant can behave
// differently from a variable.
func (x *inliner) wholeOperand(path []ast.Node) bool {
	if len(path) < 2 {
		return false
	}
	if id, ok := path[0].(*ast.Ident); ok {
		if p := x.paramOf(x.info.Uses[id]); p != nil {
			if v := x.info.Types[p.arg].Value; v != nil && (v.Kind() == constant.String || v.Kind() == constant.Bool) {
				// No overflow or division for strings and booleans; only a
				// constant index into a constant string is checked at
				// compile time.
				switch q := path[1].(type) {
				case *ast.IndexExpr:
					return q.X != path[0]
				case *ast.SliceExpr:
					return q.X != path[0]
				}
				return true
			}
		}
	}
	switch p := path[1].(type) {
	case *ast.ReturnStmt, *ast.AssignStmt, *ast.ValueSpec, *ast.KeyValueExpr, *ast.CompositeLit, *ast.SendStmt:
		return true
	case *ast.CallExpr:
		if p.Fun == path[0] {
			return false
		}
		if tv, ok := x.info.Types[p.Fun]; ok && tv.IsType() {
			return false
		}
		if b, ok := x.info.Uses[analysis.IdentOf(p.Fun)].(*types.Builtin); ok {
			switch b.Name() {
			case "append", "copy", "delete", "print", "println", "panic", "clear", "close", "len", "cap":
				return true // no constant folding of a whole argument there
			}
			return false // make, new, min, max, complex, real, imag
		}
		return true
	}
	return false
}

// conditionalOrRepeated says why code at child is not evaluated exactly
// once each time n is, or "".
func conditionalOrRepeated(n, child ast.Node) string {
	switch n := n.(type) {
	case *ast.BinaryExpr:
		if (n.Op == token.LAND || n.Op == token.LOR) && child == n.Y {
			return fmt.Sprintf("in the right operand of %s, which is only evaluated sometimes", n.Op)
		}
	case *ast.FuncLit:
		return "inside a function literal, which may run later, several times, or not at all"
	}
	return ""
}

func paramName(v *types.Var) string {
	if v.Name() == "" {
		return "_"
	}
	return v.Name()
}

func sortIdents(ids []*ast.Ident) {
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j].Pos() < ids[j-1].Pos(); j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
}
