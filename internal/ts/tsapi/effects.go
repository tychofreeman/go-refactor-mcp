package tsapi

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/checker"
	"github.com/tychofreeman/go-refactor-mcp/internal/reorder"
)

// Effects is what evaluating an expression or running statements may do,
// from their syntax and types. Conservative: what it cannot tell, it
// counts as writing and throwing.
type Effects struct {
	Writes bool // may change state or run code not known: a call, an assignment, a getter, valueOf through an operator
	Throws bool // may throw: a property of what may be null or undefined, a call
	// OtherThrows says it may throw another way than by reading through
	// what may be null or undefined; such a read alone may move past code
	// that writes (as a nil dereference in Go, the user's choice).
	OtherThrows bool
	Reads       bool // reads state other code may change: a property, a variable of the module or one a closure assigns
	Fresh       bool // makes a new object, function or array, which a second evaluation would not share
	Leaves      bool // a return, break or continue that jumps out of the code
	// Assigns and Locals are the locals it assigns and reads, each with
	// the part that does: a local is a variable or parameter of a
	// function that no function inside it assigns, so only the code
	// itself changes it.
	Assigns, Locals map[*ast.Symbol]string
	// At names the first part that does each, for a refusal: "the call
	// log(p) at bed.ts:3".
	At struct{ Write, Throw, Read, Fresh, Leave string }
	// Why names the first part that writes or throws.
	Why string
}

func (e *Effects) add(o Effects) {
	if e.Why == "" {
		e.Why = o.Why
	}
	e.Writes = e.Writes || o.Writes
	e.Throws = e.Throws || o.Throws
	e.OtherThrows = e.OtherThrows || o.OtherThrows
	e.Reads = e.Reads || o.Reads
	e.Fresh = e.Fresh || o.Fresh
	e.Leaves = e.Leaves || o.Leaves
	first := func(a *string, b string) {
		if *a == "" {
			*a = b
		}
	}
	first(&e.At.Write, o.At.Write)
	first(&e.At.Throw, o.At.Throw)
	first(&e.At.Read, o.At.Read)
	first(&e.At.Fresh, o.At.Fresh)
	first(&e.At.Leave, o.At.Leave)
	e.Assigns = union(e.Assigns, o.Assigns)
	e.Locals = union(e.Locals, o.Locals)
}

// union adds b's entries missing from a, copying a first.
func union(a, b map[*ast.Symbol]string) map[*ast.Symbol]string {
	if len(b) == 0 {
		return a
	}
	out := maps.Clone(a)
	if out == nil {
		out = map[*ast.Symbol]string{}
	}
	for s, w := range b {
		if _, ok := out[s]; !ok {
			out[s] = w
		}
	}
	return out
}

// CanDrop reports whether not evaluating it would change nothing.
func (e Effects) CanDrop() bool { return !e.Writes && !e.Throws && !e.Leaves && len(e.Assigns) == 0 }

// CanRepeat reports whether evaluating it twice gives the same as once.
func (e Effects) CanRepeat() bool { return e.CanDrop() && !e.Fresh }

// facts are what e may do, as reorder judges it.
func (e Effects) facts() reorder.Facts {
	throws := reorder.NoThrow
	switch {
	case e.Throws && e.OtherThrows:
		throws = reorder.OtherThrow
	case e.Throws:
		throws = reorder.OnNull
	}
	return reorder.Facts{Writes: e.Writes, Reads: e.Reads, Throws: throws, Makes: e.Fresh, Leaves: e.Leaves}
}

// does names the part of e that does k, and what it does: "the call
// log(p) at bed.ts:3", "may change state".
func (e Effects) does(k reorder.Kind) (string, string) {
	switch k {
	case reorder.Writing:
		return e.At.Write, "may change state"
	case reorder.Throwing:
		return e.At.Throw, "may throw"
	case reorder.Leaving:
		return e.At.Leave, "leaves"
	case reorder.Reading:
		return e.At.Read, "reads state"
	}
	return e.At.Fresh, "makes a new value each time"
}

// conflict returns why a and b may not be evaluated in the other order,
// or "": both act, one writes what the other reads (reorder.Conflict), or
// they share a local that one assigns.
func conflict(a, b Effects) string {
	if ak, bk := reorder.Conflict(a.facts(), b.facts()); ak != reorder.None {
		if ak == reorder.Reading { // the writer first
			a, b, ak, bk = b, a, bk, ak
		}
		ap, av := a.does(ak)
		bp, bv := b.does(bk)
		return fmt.Sprintf("%s %s, and %s %s", ap, av, bp, bv)
	}
	for _, s := range sortedSymbols(a.Assigns) {
		if w, ok := b.Assigns[s]; ok {
			return fmt.Sprintf("%s and %s both assign %s", a.Assigns[s], w, s.Name)
		}
		if w, ok := b.Locals[s]; ok {
			return fmt.Sprintf("%s assigns %s, and %s reads it", a.Assigns[s], s.Name, w)
		}
	}
	for _, s := range sortedSymbols(b.Assigns) {
		if w, ok := a.Locals[s]; ok {
			return fmt.Sprintf("%s assigns %s, and %s reads it", b.Assigns[s], s.Name, w)
		}
	}
	return ""
}

// sortedSymbols returns m's symbols by name, so that refusals do not
// change from run to run.
func sortedSymbols(m map[*ast.Symbol]string) []*ast.Symbol {
	out := slices.Collect(maps.Keys(m))
	slices.SortFunc(out, func(x, y *ast.Symbol) int { return strings.Compare(x.Name+m[x], y.Name+m[y]) })
	return out
}

// canReorder returns why the expressions may not be evaluated in another
// order, or "": no two of them conflict.
func canReorder(all []Effects) string {
	for i := range all {
		for _, b := range all[i+1:] {
			if why := conflict(all[i], b); why != "" {
				return why
			}
		}
	}
	return ""
}

// Times says how often an expression would be evaluated after a move,
// for each time it is evaluated now.
type Times = reorder.Times

const (
	Once    = reorder.Once
	Maybe   = reorder.Maybe
	Several = reorder.Several
	Shared  = reorder.Shared
	Unused  = reorder.Unused
)

// Move is a change to where, or how often, code is evaluated: Expr, an
// expression or a statement, would be evaluated past Past (statements or
// expressions that run between its old and new places, in either
// direction, and between its evaluations: a loop's other parts), and
// Times as often as now.
type Move struct {
	Expr  *ast.Node
	Past  []*ast.Node
	Times Times
}

// Check returns why the move would change what the program does, or ""
// when it would not.
func (a *effects) Check(m Move) string {
	e := a.ofNodes([]*ast.Node{m.Expr})
	q := a.lined(m.Expr).source(m.Expr)
	assigns := ""
	if s := sortedSymbols(e.Assigns); len(s) > 0 {
		assigns = e.Assigns[s[0]]
	}
	then := map[Times]string{Maybe: "then not always be evaluated", Several: "be evaluated another number of times",
		Shared: "be evaluated once for several uses", Unused: "be evaluated another number of times"}[m.Times]
	f := e.facts()
	f.Writes = f.Writes || assigns != ""
	if k := reorder.Repeat(m.Times, f, true); k != reorder.None {
		if k == reorder.Writing && !e.Writes {
			return fmt.Sprintf("%s would %s, and %s assigns a variable", q, then, assigns)
		}
		p, v := e.does(k)
		return fmt.Sprintf("%s would %s, and %s %s", q, then, p, v)
	}
	if len(m.Past) == 0 {
		return ""
	}
	if why := conflict(e, a.ofNodes(m.Past)); why != "" {
		return fmt.Sprintf("%s cannot move past what runs between: %s", q, why)
	}
	return ""
}

// effects is the analysis of code of one program.
type effects struct {
	c     *checker.Checker
	l     *lined
	files map[*ast.SourceFile]*lined
	local map[*ast.Symbol]bool
	sums  map[*ast.Node]*Effects // a module function's body; nil while it is summarized
}

func newEffects(c *checker.Checker, l *lined) *effects {
	return &effects{c: c, l: l, files: map[*ast.SourceFile]*lined{l.f: l}, local: map[*ast.Symbol]bool{}, sums: map[*ast.Node]*Effects{}}
}

// lined returns the file of n.
func (a *effects) lined(n *ast.Node) *lined {
	f := ast.GetSourceFileOfNode(n)
	if l, ok := a.files[f]; ok {
		return l
	}
	l := newLined(f)
	a.files[f] = l
	return l
}

// part names n for a refusal: "the call log(p) at bed.ts:3".
func (a *effects) part(n *ast.Node, what string) string {
	l := a.lined(n)
	if what != "" {
		what += " "
	}
	return fmt.Sprintf("%s%s at %s", what, l.source(n), l.where(n))
}

// unknown is what code does whose effects are not known.
func (a *effects) unknown(n *ast.Node, what string) Effects {
	p := a.part(n, what)
	e := Effects{Writes: true, Throws: true, OtherThrows: true, Reads: true, Why: p}
	e.At.Write, e.At.Throw, e.At.Read = p, p, p
	return e
}

// throws is what code does that may throw and nothing else.
func (a *effects) throws(n *ast.Node, why string) Effects {
	p := fmt.Sprintf("%s, %s, at %s", a.lined(n).source(n), why, a.lined(n).where(n))
	e := Effects{Throws: true, OtherThrows: true, Why: p}
	e.At.Throw = p
	return e
}

// nullish is what code does that may throw by reading through what may
// be null or undefined, and nothing else.
func (a *effects) nullish(n *ast.Node, why string) Effects {
	e := a.throws(n, why)
	e.OtherThrows = false
	return e
}

// isLocal reports whether s is a local: a variable or parameter of a
// function that no function inside that function assigns.
func (a *effects) isLocal(s *ast.Symbol) bool {
	if v, ok := a.local[s]; ok {
		return v
	}
	d := s.ValueDeclaration
	fn := (*ast.Node)(nil)
	if d != nil && (ast.IsVariableDeclaration(d) || ast.IsParameterDeclaration(d)) {
		fn = ast.GetContainingFunction(d)
	}
	ok := fn != nil && fn.Body() != nil && !a.assignedInside(fn.Body(), s, false)
	a.local[s] = ok
	return ok
}

// assignedInside reports whether code in n, inside a function nested in
// it when nested is false at first, assigns s.
func (a *effects) assignedInside(n *ast.Node, s *ast.Symbol, nested bool) bool {
	found := false
	var visit func(x *ast.Node, nested bool) bool
	visit = func(x *ast.Node, nested bool) bool {
		if found {
			return true
		}
		if ast.IsFunctionLike(x) {
			nested = true
		}
		if nested {
			var target *ast.Node
			switch {
			case ast.IsBinaryExpression(x) && ast.IsAssignmentOperator(x.AsBinaryExpression().OperatorToken.Kind):
				target = x.AsBinaryExpression().Left
			case ast.IsPrefixUnaryExpression(x) && isUpdate(x.AsPrefixUnaryExpression().Operator):
				target = x.AsPrefixUnaryExpression().Operand
			case ast.IsPostfixUnaryExpression(x):
				target = x.AsPostfixUnaryExpression().Operand
			}
			if target != nil && slices.Contains(symbolsOf(a.c, target), s) {
				found = true
				return true
			}
		}
		x.ForEachChild(func(y *ast.Node) bool { return visit(y, nested) })
		return found
	}
	n.ForEachChild(func(y *ast.Node) bool { return visit(y, nested) })
	return found
}

func isUpdate(op ast.Kind) bool { return op == ast.KindPlusPlusToken || op == ast.KindMinusMinusToken }

// primitive reports whether every value of n's type is a number, bigint,
// string, boolean or enum, which operators coerce without running code.
func (a *effects) primitive(n *ast.Node) bool {
	t := a.c.GetTypeAtLocation(n)
	if t == nil {
		return false
	}
	types := []*checker.Type{t}
	if t.Flags()&checker.TypeFlagsUnion != 0 {
		types = t.Types()
	}
	for _, u := range types {
		if u.Flags()&(checker.TypeFlagsNumberLike|checker.TypeFlagsBigIntLike|checker.TypeFlagsStringLike|checker.TypeFlagsBooleanLike|checker.TypeFlagsEnumLike) == 0 {
			return false
		}
	}
	return true
}

// maybeNullish reports whether n's type admits null or undefined, or is
// not known (any, unknown). A non-null assertion does not count: it
// checks nothing at run time.
func (a *effects) maybeNullish(n *ast.Node) bool {
	for ast.IsNonNullExpression(n) || ast.IsParenthesizedExpression(n) {
		n = n.Expression()
	}
	t := a.c.GetTypeAtLocation(n)
	if t == nil {
		return true
	}
	types := []*checker.Type{t}
	if t.Flags()&checker.TypeFlagsUnion != 0 {
		types = t.Types()
	}
	for _, u := range types {
		if u.Flags()&(checker.TypeFlagsNullable|checker.TypeFlagsVoid|checker.TypeFlagsAnyOrUnknown) != 0 {
			return true
		}
	}
	return false
}

// pureMethods are methods of strings and arrays that change nothing and
// do not throw on values of their types; callbacks are judged apart.
var pureMethods = map[string]bool{
	"charAt": true, "charCodeAt": true, "codePointAt": true, "slice": true, "substring": true, "toUpperCase": true, "toLowerCase": true,
	"trim": true, "trimStart": true, "trimEnd": true, "includes": true, "startsWith": true, "endsWith": true, "indexOf": true,
	"lastIndexOf": true, "padStart": true, "padEnd": true, "split": true, "at": true, "repeat": true, "concat": true,
}

// callbackMethods are array methods that call their callback and change
// nothing else.
var callbackMethods = map[string]bool{"map": true, "filter": true, "some": true, "every": true, "find": true, "findIndex": true}

// pureMath are the functions of Math that change no state (not random).
func pureMath(name string) bool { return name != "random" }

// Of returns the effects of evaluating n.
func (a *effects) Of(n *ast.Node) Effects {
	var out Effects
	switch {
	case n == nil:
		return out
	case ast.IsLiteralExpression(n), ast.IsLiteralKind(n.Kind), n.Kind == ast.KindThisKeyword, n.Kind == ast.KindNullKeyword,
		n.Kind == ast.KindTrueKeyword, n.Kind == ast.KindFalseKeyword, n.Kind == ast.KindNoSubstitutionTemplateLiteral:
		return out
	case ast.IsIdentifier(n):
		if n.Text() == "undefined" {
			return out
		}
		s := a.c.GetSymbolAtLocation(n)
		if s == nil {
			return a.unknown(n, "the name")
		}
		if s.Flags&(ast.SymbolFlagsBlockScopedVariable|ast.SymbolFlagsFunctionScopedVariable) != 0 && !isConst(s.ValueDeclaration) {
			if a.isLocal(s) {
				out.Locals = map[*ast.Symbol]string{s: a.part(n, "")}
			} else {
				out.Reads, out.At.Read = true, a.part(n, "")
			}
		}
		return out
	case ast.IsParenthesizedExpression(n), ast.IsAsExpression(n), ast.IsSatisfiesExpression(n), ast.IsNonNullExpression(n), ast.IsTypeAssertion(n):
		return a.Of(n.Expression())
	case ast.IsArrowFunction(n), ast.IsFunctionExpression(n), ast.IsClassExpression(n):
		out.Fresh, out.At.Fresh = true, a.part(n, "")
		return out
	case ast.IsArrayLiteralExpression(n):
		out.Fresh, out.At.Fresh = true, a.part(n, "")
		for _, el := range n.AsArrayLiteralExpression().Elements.Nodes {
			if ast.IsSpreadElement(el) && !a.c.IsArrayType(a.c.GetTypeAtLocation(el.Expression())) {
				out.add(a.unknown(el, "the spread"))
			}
			out.add(a.Of(el))
		}
		return out
	case ast.IsSpreadElement(n):
		return a.Of(n.Expression())
	case ast.IsObjectLiteralExpression(n):
		out.Fresh, out.At.Fresh = true, a.part(n, "")
		for _, p := range n.AsObjectLiteralExpression().Properties.Nodes {
			switch {
			case ast.IsPropertyAssignment(p):
				if nm := p.Name(); nm != nil && ast.IsComputedPropertyName(nm) {
					out.add(a.Of(nm.Expression()))
				}
				out.add(a.Of(p.Initializer()))
			case ast.IsShorthandPropertyAssignment(p):
				out.add(a.Of(p.Name()))
			case ast.IsSpreadAssignment(p):
				out.add(a.Of(p.Expression()))
				out.add(a.unknown(p, "the spread")) // runs the getters of what it spreads
			case ast.IsMethodDeclaration(p), ast.IsGetAccessorDeclaration(p), ast.IsSetAccessorDeclaration(p):
			default:
				out.add(a.unknown(p, "the property"))
			}
		}
		return out
	case ast.IsPropertyAccessExpression(n), ast.IsElementAccessExpression(n):
		obj := n.Expression()
		out.add(a.Of(obj))
		if ast.IsElementAccessExpression(n) {
			out.add(a.Of(n.AsElementAccessExpression().ArgumentExpression))
		}
		out.Reads = true
		if out.At.Read == "" {
			out.At.Read = a.part(n, "")
		}
		optional := n.Flags&ast.NodeFlagsOptionalChain != 0
		if a.maybeNullish(obj) && !optional {
			out.add(a.nullish(n, "which reads a property of what may be null or undefined"))
		}
		if ast.IsPropertyAccessExpression(n) {
			if s := a.c.GetSymbolAtLocation(n.Name()); s != nil && s.Flags&ast.SymbolFlagsGetAccessor != 0 {
				d := a.ownGetter(n)
				if d == nil {
					out.add(a.unknown(n, "the getter"))
					return out
				}
				out.add(a.summary(n, d))
				if _, fill := cacheOf(d); fill == nil && !a.primitive(n) {
					out.Fresh = true
					if out.At.Fresh == "" {
						out.At.Fresh = a.part(n, "the getter")
					}
				}
			}
		}
		return out
	case ast.IsTemplateExpression(n):
		for _, span := range n.AsTemplateExpression().TemplateSpans.Nodes {
			e := span.Expression()
			out.add(a.Of(e))
			if !a.primitive(e) {
				out.add(a.unknown(e, "the conversion of"))
			}
		}
		return out
	case ast.IsPrefixUnaryExpression(n):
		u := n.AsPrefixUnaryExpression()
		if isUpdate(u.Operator) {
			return a.assign(n, u.Operand, true)
		}
		out.add(a.Of(u.Operand))
		if u.Operator != ast.KindExclamationToken && !a.primitive(u.Operand) {
			out.add(a.unknown(u.Operand, "the conversion of"))
		}
		return out
	case ast.IsPostfixUnaryExpression(n):
		return a.assign(n, n.AsPostfixUnaryExpression().Operand, true)
	case ast.IsTypeOfExpression(n), ast.IsVoidExpression(n):
		return a.Of(n.Expression())
	case ast.IsConditionalExpression(n):
		ce := n.AsConditionalExpression()
		out.add(a.Of(ce.Condition))
		out.add(a.Of(ce.WhenTrue))
		out.add(a.Of(ce.WhenFalse))
		return out
	case ast.IsBinaryExpression(n):
		b := n.AsBinaryExpression()
		op := b.OperatorToken.Kind
		if ast.IsAssignmentOperator(op) {
			out = a.assign(n, b.Left, op != ast.KindEqualsToken)
			out.add(a.Of(b.Right))
			if op != ast.KindEqualsToken && !ast.IsLogicalOrCoalescingAssignmentOperator(op) && !(a.primitive(b.Left) && a.primitive(b.Right)) {
				out.add(a.unknown(n, "the conversion in"))
			}
			return out
		}
		out.add(a.Of(b.Left))
		out.add(a.Of(b.Right))
		switch {
		case op == ast.KindAmpersandAmpersandToken, op == ast.KindBarBarToken, op == ast.KindQuestionQuestionToken, op == ast.KindCommaToken,
			op == ast.KindEqualsEqualsEqualsToken, op == ast.KindExclamationEqualsEqualsToken:
		case op == ast.KindInstanceOfKeyword, op == ast.KindInKeyword:
			out.add(a.throws(n, "which throws when its right side is not an object"))
		default:
			for _, side := range []*ast.Node{b.Left, b.Right} {
				if !a.primitive(side) {
					out.add(a.unknown(side, "the conversion of"))
				}
			}
		}
		return out
	case ast.IsCallExpression(n):
		return a.call(n)
	}
	return a.unknown(n, "the expression")
}

// assign returns the effects of n assigning target, which it reads first
// when reads: a local, or else a change of state not known.
func (a *effects) assign(n, target *ast.Node, reads bool) Effects {
	var out Effects
	for ast.IsParenthesizedExpression(target) || ast.IsNonNullExpression(target) {
		target = target.Expression()
	}
	if ast.IsIdentifier(target) {
		if s := a.c.GetSymbolAtLocation(target); s != nil && a.isLocal(s) {
			if reads {
				out.add(a.Of(target))
			}
			out.Assigns = map[*ast.Symbol]string{s: a.part(n, "")}
			if !a.primitive(target) && reads && !ast.IsBinaryExpression(n) {
				out.add(a.unknown(target, "the conversion of"))
			}
			return out
		}
	}
	if ast.IsPropertyAccessExpression(target) || ast.IsElementAccessExpression(target) {
		out.add(a.Of(target.Expression()))
	}
	out.add(a.unknown(n, "the assignment"))
	return out
}

// call returns the effects of a call: known for pure methods of strings
// and arrays and of Math, and for the module's own functions by their
// bodies, whose arguments are judged too.
func (a *effects) call(n *ast.Node) Effects {
	callee := n.Expression()
	var out Effects
	for _, arg := range n.Arguments() {
		out.add(a.Of(arg))
	}
	if body := a.ownFunction(callee); body != nil {
		if ast.IsPropertyAccessExpression(callee) {
			out.add(a.Of(callee.Expression()))
			if a.maybeNullish(callee.Expression()) && n.Flags&ast.NodeFlagsOptionalChain == 0 {
				out.add(a.nullish(n, "a method of what may be null or undefined"))
			}
		}
		out.add(a.summary(n, body))
		_, fill := cacheOf(body)
		if t := a.c.GetTypeAtLocation(n); fill == nil && !a.primitive(n) && (t == nil || t.Flags()&(checker.TypeFlagsVoid|checker.TypeFlagsUndefined|checker.TypeFlagsNull) == 0) {
			out.Fresh = true
			if out.At.Fresh == "" {
				out.At.Fresh = a.part(n, "the call")
			}
		}
		return out
	}
	if !ast.IsPropertyAccessExpression(callee) {
		out.add(a.unknown(n, "the call"))
		return out
	}
	recv, method := callee.Expression(), callee.Name().Text()
	switch {
	case ast.IsIdentifier(recv) && recv.Text() == "Math" && a.global(recv) && pureMath(method):
		for _, arg := range n.Arguments() {
			if !a.primitive(arg) {
				out.add(a.unknown(arg, "the conversion of"))
			}
		}
		return out
	case pureMethods[method] && a.knownReceiver(recv):
	case (method == "replace" || method == "replaceAll") && a.knownReceiver(recv) && len(n.Arguments()) == 2 &&
		(a.primitive(n.Arguments()[0]) || n.Arguments()[0].Kind == ast.KindRegularExpressionLiteral) && a.primitive(n.Arguments()[1]):
		// with a string or a regular expression written there, and a
		// string for what replaces it (not a function, which it would call)
	case callbackMethods[method] && a.c.IsArrayType(a.c.GetTypeAtLocation(recv)) && len(n.Arguments()) > 0:
		cb := n.Arguments()[0]
		body := cb.Body()
		if !ast.IsArrowFunction(cb) || body == nil || ast.IsBlock(body) {
			out.add(a.unknown(n, "the call"))
			return out
		}
		// The callback runs for each element: its body's effects count.
		be := a.Of(body)
		be.Fresh, be.At.Fresh = false, ""
		out.add(be)
	default:
		out.add(a.unknown(n, "the call"))
		return out
	}
	out.add(a.Of(recv))
	if a.maybeNullish(recv) && n.Flags&ast.NodeFlagsOptionalChain == 0 {
		out.add(a.nullish(n, "a method of what may be null or undefined"))
	}
	return out
}

// ownFunction returns the declaration of the function callee names, when
// the module declares it with a body and no other declaration could run
// instead: a function, a const bound to a function, or a method that is
// static or private, which no subclass overrides.
func (a *effects) ownFunction(callee *ast.Node) *ast.Node {
	var name *ast.Node
	switch {
	case ast.IsIdentifier(callee):
		name = callee
	case ast.IsPropertyAccessExpression(callee):
		name = callee.Name()
	default:
		return nil
	}
	s := a.c.GetSymbolAtLocation(name)
	if s != nil && s.Flags&ast.SymbolFlagsAlias != 0 {
		s = a.c.GetAliasedSymbol(s)
	}
	if s == nil || len(s.Declarations) != 1 {
		return nil // overloads are judged by the implementation; none to keep it simple
	}
	d := s.Declarations[0]
	if ast.GetSourceFileOfNode(d).IsDeclarationFile {
		return nil
	}
	switch {
	case ast.IsFunctionDeclaration(d) && ast.IsIdentifier(callee):
	case ast.IsVariableDeclaration(d) && isConst(d) && ast.IsIdentifier(callee) && d.Initializer() != nil &&
		(ast.IsArrowFunction(d.Initializer()) || ast.IsFunctionExpression(d.Initializer())):
		d = d.Initializer()
	case ast.IsMethodDeclaration(d) && ast.IsPropertyAccessExpression(callee) &&
		(d.ModifierFlags()&(ast.ModifierFlagsStatic|ast.ModifierFlagsPrivate) != 0 || ast.IsPrivateIdentifier(d.Name())):
	default:
		return nil
	}
	if d.Body() == nil || ast.GetFunctionFlags(d)&(ast.FunctionFlagsAsync|ast.FunctionFlagsGenerator) != 0 {
		return nil
	}
	return d
}

// summary returns what calling fn may do: what its body does, but for
// its own locals and its returns. A call while fn is being summarized (a
// recursive one) is not known.
func (a *effects) summary(call, fn *ast.Node) Effects {
	if s, ok := a.sums[fn]; ok {
		if s == nil {
			return a.unknown(call, "the call")
		}
		return a.named(*s, call)
	}
	a.sums[fn] = nil
	body := fn.Body()
	var e Effects
	if field, fill := cacheOf(fn); fill != nil {
		e = a.cached(fn, field, fill)
	} else if ast.IsBlock(body) {
		e = a.ofNodes(body.Statements())
	} else {
		e = a.Of(body)
	}
	e.Leaves, e.At.Leave = false, ""
	e.Fresh, e.At.Fresh = false, ""
	own := func(m map[*ast.Symbol]string) map[*ast.Symbol]string {
		var out map[*ast.Symbol]string
		for s, w := range m {
			if d := s.ValueDeclaration; d != nil && d.Pos() >= fn.Pos() && d.End() <= fn.End() && ast.GetSourceFileOfNode(d) == ast.GetSourceFileOfNode(fn) {
				continue
			}
			if out == nil {
				out = map[*ast.Symbol]string{}
			}
			out[s] = w
		}
		return out
	}
	e.Assigns, e.Locals = own(e.Assigns), own(e.Locals)
	a.sums[fn] = &e
	return a.named(e, call)
}

// ownGetter returns the get accessor that the property access n reads,
// when the module declares it with a body and no subclass could override
// it: static or private, as ownFunction takes a method.
func (a *effects) ownGetter(n *ast.Node) *ast.Node {
	s := a.c.GetSymbolAtLocation(n.Name())
	if s == nil {
		return nil
	}
	for _, d := range s.Declarations {
		if ast.IsGetAccessorDeclaration(d) && d.Body() != nil && !ast.GetSourceFileOfNode(d).IsDeclarationFile &&
			(d.ModifierFlags()&(ast.ModifierFlagsStatic|ast.ModifierFlagsPrivate) != 0 || ast.IsPrivateIdentifier(d.Name())) {
			return d
		}
	}
	return nil
}

// cacheOf returns the property of this that the getter or method fn
// caches its result in, and the block that makes the result, when its
// body is if (this.f === undefined) { ... } return this.f; (or == null,
// === null, !this.f). Its assignments to this.f only fill the cache:
// whenever the block runs, later calls return what it made, so they do
// not count as writes, and the result is not a new value each time.
func cacheOf(fn *ast.Node) (field string, fill *ast.Node) {
	body := fn.Body()
	if body == nil || !ast.IsBlock(body) || len(body.Statements()) != 2 {
		return "", nil
	}
	ifs, ret := body.Statements()[0], body.Statements()[1]
	if !ast.IsIfStatement(ifs) || ifs.AsIfStatement().ElseStatement != nil || !ast.IsReturnStatement(ret) || ret.Expression() == nil {
		return "", nil
	}
	field = thisProperty(ret.Expression())
	cond := ast.SkipParentheses(ifs.Expression())
	var tested *ast.Node
	switch {
	case ast.IsPrefixUnaryExpression(cond) && cond.AsPrefixUnaryExpression().Operator == ast.KindExclamationToken:
		tested = cond.AsPrefixUnaryExpression().Operand
	case ast.IsBinaryExpression(cond):
		b := cond.AsBinaryExpression()
		switch b.OperatorToken.Kind {
		case ast.KindEqualsEqualsEqualsToken, ast.KindEqualsEqualsToken:
		default:
			return "", nil
		}
		if r := b.Right; r.Kind == ast.KindNullKeyword || ast.IsIdentifier(r) && r.Text() == "undefined" {
			tested = b.Left
		}
	}
	if field == "" || tested == nil || thisProperty(tested) != field {
		return "", nil
	}
	return field, ifs.AsIfStatement().ThenStatement
}

// thisProperty returns f when e is this.f, or "".
func thisProperty(e *ast.Node) string {
	e = ast.SkipParentheses(e)
	if ast.IsPropertyAccessExpression(e) && e.Expression().Kind == ast.KindThisKeyword && ast.IsIdentifier(e.Name()) {
		return e.Name().Text()
	}
	return ""
}

// cached returns the effects of running the body of fn, a getter that
// caches its result in this.field (cacheOf), whose block fill makes it:
// its assignments to this.field count only for what they evaluate.
func (a *effects) cached(fn *ast.Node, field string, fill *ast.Node) Effects {
	stmts := []*ast.Node{fill}
	if ast.IsBlock(fill) {
		stmts = fill.Statements()
	}
	ifs := fn.Body().Statements()[0]
	e := a.Of(ifs.Expression())
	for _, s := range stmts {
		if ast.IsExpressionStatement(s) && ast.IsBinaryExpression(s.Expression()) {
			b := s.Expression().AsBinaryExpression()
			if b.OperatorToken.Kind == ast.KindEqualsToken && thisProperty(b.Left) == field {
				e.add(a.Of(b.Right))
				continue
			}
		}
		e.add(a.ofNodes([]*ast.Node{s}))
	}
	e.add(a.Of(fn.Body().Statements()[1].Expression()))
	return e
}

// named returns a body's effects e with every part named as call (or the
// property access that runs a getter), which refusals name rather than
// code in the body.
func (a *effects) named(e Effects, call *ast.Node) Effects {
	at := a.part(call, "the call")
	if !ast.IsCallExpression(call) {
		at = a.part(call, "the getter")
	}
	for _, f := range []*string{&e.At.Write, &e.At.Throw, &e.At.Read, &e.Why} {
		if *f != "" {
			*f = at
		}
	}
	rename := func(m map[*ast.Symbol]string) map[*ast.Symbol]string {
		if m == nil {
			return nil
		}
		out := map[*ast.Symbol]string{}
		for s := range m {
			out[s] = at
		}
		return out
	}
	e.Assigns, e.Locals = rename(e.Assigns), rename(e.Locals)
	return e
}

// ofNodes returns the effects of running nodes in order: statements or
// expressions.
func (a *effects) ofNodes(nodes []*ast.Node) Effects {
	w := walk{a: a, labels: map[string]bool{}}
	var out Effects
	for _, n := range nodes {
		if ast.IsExpression(n) {
			out.add(a.Of(n))
		} else {
			out.add(w.stmt(n))
		}
	}
	return out
}

// walk is the effects of statements, knowing which jumps stay inside
// them: a break or continue of a loop, switch or label they hold.
type walk struct {
	a               *effects
	loops, switches int
	labels          map[string]bool
}

func (w *walk) stmts(list []*ast.Node) Effects {
	var out Effects
	for _, s := range list {
		out.add(w.stmt(s))
	}
	return out
}

func (w *walk) leaves(n *ast.Node) Effects {
	e := Effects{Leaves: true}
	e.At.Leave = w.a.part(n, "")
	return e
}

func (w *walk) stmt(n *ast.Node) Effects {
	a := w.a
	var out Effects
	switch {
	case n == nil, n.Kind == ast.KindEmptyStatement, ast.IsFunctionDeclaration(n), ast.IsInterfaceDeclaration(n), ast.IsTypeAliasDeclaration(n):
		return out
	case ast.IsExpressionStatement(n):
		return a.Of(n.Expression())
	case ast.IsVariableStatement(n):
		return w.declare(n.AsVariableStatement().DeclarationList, false)
	case ast.IsBlock(n):
		return w.stmts(n.Statements())
	case ast.IsIfStatement(n):
		s := n.AsIfStatement()
		out.add(a.Of(s.Expression))
		out.add(w.stmt(s.ThenStatement))
		out.add(w.stmt(s.ElseStatement))
		return out
	case ast.IsReturnStatement(n):
		out.add(a.Of(n.Expression()))
		out.add(w.leaves(n))
		return out
	case ast.IsThrowStatement(n):
		out.add(a.Of(n.Expression()))
		t := Effects{Throws: true, OtherThrows: true, Why: a.part(n, "")}
		t.At.Throw = t.Why
		out.add(t)
		return out
	case n.Kind == ast.KindBreakStatement, n.Kind == ast.KindContinueStatement:
		label := n.Label()
		switch {
		case label != nil && w.labels[label.Text()]:
		case label == nil && n.Kind == ast.KindBreakStatement && w.loops+w.switches > 0:
		case label == nil && n.Kind == ast.KindContinueStatement && w.loops > 0:
		default:
			out.add(w.leaves(n))
		}
		return out
	case ast.IsLabeledStatement(n):
		ls := n.AsLabeledStatement()
		had := w.labels[ls.Label.Text()]
		w.labels[ls.Label.Text()] = true
		out = w.stmt(ls.Statement)
		w.labels[ls.Label.Text()] = had
		return out
	case ast.IsForStatement(n):
		f := n.AsForStatement()
		if f.Initializer != nil && ast.IsVariableDeclarationList(f.Initializer) {
			out.add(w.declare(f.Initializer, false))
		} else {
			out.add(a.Of(f.Initializer))
		}
		out.add(a.Of(f.Condition))
		out.add(a.Of(f.Incrementor))
		out.add(w.loop(f.Statement))
		return out
	case ast.IsWhileStatement(n), ast.IsDoStatement(n):
		out.add(a.Of(n.Expression()))
		out.add(w.loop(n.Statement()))
		return out
	case ast.IsForInOrOfStatement(n):
		f := n.AsForInOrOfStatement()
		out.add(a.Of(f.Expression))
		if ast.IsForOfStatement(n) && !a.c.IsArrayType(a.c.GetTypeAtLocation(f.Expression)) {
			out.add(a.unknown(f.Expression, "the iteration of"))
		}
		out.add(Effects{Reads: true})
		if out.At.Read == "" {
			out.At.Read = a.part(f.Expression, "")
		}
		if ast.IsVariableDeclarationList(f.Initializer) {
			out.add(w.declare(f.Initializer, true))
		} else {
			out.add(a.assign(f.Initializer, f.Initializer, false))
		}
		out.add(w.loop(f.Statement))
		return out
	case ast.IsSwitchStatement(n):
		s := n.AsSwitchStatement()
		out.add(a.Of(s.Expression))
		w.switches++
		for _, c := range s.CaseBlock.AsCaseBlock().Clauses.Nodes {
			out.add(a.Of(c.Expression()))
			out.add(w.stmts(c.Statements()))
		}
		w.switches--
		return out
	case ast.IsTryStatement(n):
		t := n.AsTryStatement()
		out.add(w.stmt(t.TryBlock))
		if t.CatchClause != nil {
			out.add(w.stmt(t.CatchClause.AsCatchClause().Block))
		}
		out.add(w.stmt(t.FinallyBlock))
		return out
	}
	return a.unknown(n, "the statement")
}

// declare returns the effects of a list of variable declarations, given
// their values or, when element, each element of a loop: a local is
// assigned, and anything else (a pattern, a variable of the module) is
// not known.
func (w *walk) declare(list *ast.Node, element bool) Effects {
	a := w.a
	var out Effects
	for _, d := range list.AsVariableDeclarationList().Declarations.Nodes {
		if !element {
			out.add(a.Of(d.Initializer()))
		}
		if nm := d.Name(); ast.IsIdentifier(nm) {
			if s := a.c.GetSymbolAtLocation(nm); s != nil && a.isLocal(s) {
				out.add(Effects{Assigns: map[*ast.Symbol]string{s: a.part(d, "")}})
				continue
			}
		}
		out.add(a.unknown(d, "the declaration"))
	}
	return out
}

// loop returns the effects of a loop's body.
func (w *walk) loop(body *ast.Node) Effects {
	w.loops++
	defer func() { w.loops-- }()
	return w.stmt(body)
}

// global reports whether n names something the standard library
// declares, not a name of the module's own.
func (a *effects) global(n *ast.Node) bool {
	s := a.c.GetSymbolAtLocation(n)
	return s != nil && s.ValueDeclaration != nil && ast.GetSourceFileOfNode(s.ValueDeclaration).IsDeclarationFile
}

// knownReceiver reports whether recv is a string or an array, whose pure
// methods are known.
func (a *effects) knownReceiver(recv *ast.Node) bool {
	t := a.c.GetTypeAtLocation(recv)
	if t == nil {
		return false
	}
	return t.Flags()&checker.TypeFlagsStringLike != 0 || a.c.IsArrayType(t)
}
