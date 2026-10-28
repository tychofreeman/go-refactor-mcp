package tsapi

import (
	"fmt"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/checker"
)

// Effects is what evaluating an expression may do, from its syntax and
// types. Conservative: what it cannot tell, it counts as writing and
// throwing.
type Effects struct {
	Writes bool // may change state or run code not known: a call, an assignment, a getter, valueOf through an operator
	Throws bool // may throw: a property of what may be null or undefined, a call
	Reads  bool // reads state that may change: a let, a var, a parameter, a property
	Fresh  bool // makes a new object, function or array, which a second evaluation would not share
	// Why names the first part that writes or throws, for a refusal:
	// "the call log(p) at bed.ts:3".
	Why string
}

func (e *Effects) add(o Effects) {
	if e.Why == "" {
		e.Why = o.Why
	}
	e.Writes = e.Writes || o.Writes
	e.Throws = e.Throws || o.Throws
	e.Reads = e.Reads || o.Reads
	e.Fresh = e.Fresh || o.Fresh
}

// CanDrop reports whether not evaluating it would change nothing.
func (e Effects) CanDrop() bool { return !e.Writes && !e.Throws }

// CanRepeat reports whether evaluating it twice gives the same as once.
func (e Effects) CanRepeat() bool { return e.CanDrop() && !e.Fresh }

// effects is the analysis for one file.
type effects struct {
	c *checker.Checker
	l *lined
}

// unknown is what an expression does whose effects are not known.
func (a effects) unknown(n *ast.Node, what string) Effects {
	return Effects{Writes: true, Throws: true, Reads: true, Why: fmt.Sprintf("%s %s at %s", what, a.l.source(n), a.l.where(n))}
}

// primitive reports whether every value of n's type is a number, bigint,
// string, boolean or enum, which operators coerce without running code.
func (a effects) primitive(n *ast.Node) bool {
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
func (a effects) maybeNullish(n *ast.Node) bool {
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
func (a effects) Of(n *ast.Node) Effects {
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
			out.Reads = true
		}
		return out
	case ast.IsParenthesizedExpression(n), ast.IsAsExpression(n), ast.IsSatisfiesExpression(n), ast.IsNonNullExpression(n), ast.IsTypeAssertion(n):
		return a.Of(n.Expression())
	case ast.IsArrowFunction(n), ast.IsFunctionExpression(n), ast.IsClassExpression(n):
		return Effects{Fresh: true}
	case ast.IsArrayLiteralExpression(n):
		out.Fresh = true
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
		out.Fresh = true
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
		optional := n.Flags&ast.NodeFlagsOptionalChain != 0
		if a.maybeNullish(obj) && !optional {
			out.add(Effects{Throws: true, Why: fmt.Sprintf("%s, which reads a property of what may be null or undefined, at %s", a.l.source(n), a.l.where(n))})
		}
		if ast.IsPropertyAccessExpression(n) {
			if s := a.c.GetSymbolAtLocation(n.Name()); s != nil && s.Flags&ast.SymbolFlagsGetAccessor != 0 {
				out.add(a.unknown(n, "the getter"))
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
		out.add(a.Of(u.Operand))
		switch u.Operator {
		case ast.KindPlusPlusToken, ast.KindMinusMinusToken:
			out.add(a.unknown(n, "the update"))
		case ast.KindExclamationToken:
		default:
			if !a.primitive(u.Operand) {
				out.add(a.unknown(u.Operand, "the conversion of"))
			}
		}
		return out
	case ast.IsPostfixUnaryExpression(n):
		out.add(a.Of(n.AsPostfixUnaryExpression().Operand))
		out.add(a.unknown(n, "the update"))
		return out
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
		out.add(a.Of(b.Left))
		out.add(a.Of(b.Right))
		switch op := b.OperatorToken.Kind; {
		case ast.IsAssignmentOperator(op):
			out.add(a.unknown(n, "the assignment"))
		case op == ast.KindAmpersandAmpersandToken, op == ast.KindBarBarToken, op == ast.KindQuestionQuestionToken, op == ast.KindCommaToken,
			op == ast.KindEqualsEqualsEqualsToken, op == ast.KindExclamationEqualsEqualsToken:
		case op == ast.KindInstanceOfKeyword, op == ast.KindInKeyword:
			out.add(Effects{Throws: true, Why: fmt.Sprintf("%s, which throws when its right side is not an object, at %s", a.l.source(n), a.l.where(n))})
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

// call returns the effects of a call: known only for pure methods of
// strings and arrays and of Math, whose arguments are judged too.
func (a effects) call(n *ast.Node) Effects {
	callee := n.Expression()
	var out Effects
	for _, arg := range n.Arguments() {
		out.add(a.Of(arg))
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
	case callbackMethods[method] && a.c.IsArrayType(a.c.GetTypeAtLocation(recv)) && len(n.Arguments()) > 0:
		cb := n.Arguments()[0]
		body := cb.Body()
		if !ast.IsArrowFunction(cb) || body == nil || ast.IsBlock(body) {
			out.add(a.unknown(n, "the call"))
			return out
		}
		// The callback runs for each element: its body's effects count.
		be := a.Of(body)
		be.Fresh = false
		out.add(be)
	default:
		out.add(a.unknown(n, "the call"))
		return out
	}
	out.add(a.Of(recv))
	if a.maybeNullish(recv) && n.Flags&ast.NodeFlagsOptionalChain == 0 {
		out.add(Effects{Throws: true, Why: fmt.Sprintf("%s, a method of what may be null or undefined, at %s", a.l.source(n), a.l.where(n))})
	}
	return out
}

// global reports whether n names something the standard library
// declares, not a name of the module's own.
func (a effects) global(n *ast.Node) bool {
	s := a.c.GetSymbolAtLocation(n)
	return s != nil && s.ValueDeclaration != nil && ast.GetSourceFileOfNode(s.ValueDeclaration).IsDeclarationFile
}

// knownReceiver reports whether recv is a string or an array, whose pure
// methods are known.
func (a effects) knownReceiver(recv *ast.Node) bool {
	t := a.c.GetTypeAtLocation(recv)
	if t == nil {
		return false
	}
	return t.Flags()&checker.TypeFlagsStringLike != 0 || a.c.IsArrayType(t)
}

// canReorder reports whether the expressions may be evaluated in another
// order: at most one of them writes or throws, and no other reads state
// while one writes. It returns why not.
func canReorder(all []Effects) string {
	var acting []Effects
	reads := false // whether one that neither writes nor throws reads
	for _, e := range all {
		if e.Writes || e.Throws {
			acting = append(acting, e)
		} else {
			reads = reads || e.Reads
		}
	}
	switch {
	case len(acting) > 1:
		return acting[0].Why + " and " + acting[1].Why
	case len(acting) == 1 && acting[0].Writes && reads:
		return acting[0].Why
	}
	return ""
}
