package analysis

import (
	"go/ast"
	"go/token"
	"go/types"
)

// Times says how often an expression would be evaluated after a move,
// for each time it is evaluated now.
type Times int

const (
	Once    Times = iota // as often as now
	Maybe                // once or not at all (it moves under a condition)
	Several              // once for each of several uses, each evaluating it again
	Shared               // once, with the one value standing for several uses
)

// Move is a change to where, or how often, an expression is evaluated:
// Expr, an expression in the body of Func, would be evaluated past the
// code in Past (statements or expressions that run between its old and
// new places, in either direction), and Times as often as now. Check says
// whether the program would still do the same.
type Move struct {
	Effects      *Effects
	Func         *Func
	Expr         ast.Expr
	Past         []ast.Node
	Times        Times
	Dereferenced bool // every use dereferences the value, so a nil dereference in Expr panics there anyway and may move
}

// Obstacle is why a Move would change behavior: Kind, what the
// expression does that cannot move; In, the part of it that does so;
// Past, what it cannot move past (nil when the number of evaluations
// alone is the problem); and Why, a sentence.
type Obstacle struct {
	Kind     Problem
	In, Past ast.Node
	Why      string
}

// Problem is what an expression does that keeps it from moving.
type Problem int

const (
	Acts   Problem = iota // it has a side effect
	Panics                // it can panic
	Reads                 // it reads what other code can change, or a local the code it moves past writes
	Makes                 // it makes a new reference value, and one would stand for several
)

// Check returns why the move would change behavior, or nil when it
// would not. An expression keeps its meaning when it is evaluated as often
// as before and nothing it moves past could change its value, be changed
// by it, or panic or stop before it where it would have before.
func (m Move) Check() *Obstacle {
	s := m.Effects.Of(m.Func, m.Expr)
	panics, panicAt := s.Panics, s.At.Panic
	if m.Dereferenced && panics == NilPanic {
		panics, panicAt = NoPanic, nil // the use panics the same way
	} else if m.Dereferenced {
		panicAt = s.At.OtherPanic
	}
	text := func(n ast.Node) string {
		if x, ok := n.(ast.Expr); ok {
			return types.ExprString(x)
		}
		return "the code"
	}
	switch m.Times {
	case Maybe:
		if s.Effect != "" {
			return &Obstacle{Kind: Acts, In: s.At.Effect, Why: text(m.Expr) + " " + s.Effect + ", which would then not always happen"}
		}
		if panics != NoPanic {
			return &Obstacle{Kind: Panics, In: panicAt, Why: text(m.Expr) + " can panic (" + s.Unsafe + "), which would then not always happen"}
		}
	case Several, Shared:
		if s.Effect != "" {
			return &Obstacle{Kind: Acts, In: s.At.Effect, Why: text(m.Expr) + " " + s.Effect + ", which would then happen another number of times"}
		}
		if s.Reads != "" {
			return &Obstacle{Kind: Reads, In: s.At.Reads, Why: text(m.Expr) + " " + s.Reads + ", which could change between evaluations"}
		}
		if panics != NoPanic {
			return &Obstacle{Kind: Panics, In: panicAt, Why: text(m.Expr) + " can panic (" + s.Unsafe + "), which would then happen another number of times"}
		}
		if m.Times == Shared && s.Makes != "" {
			return &Obstacle{Kind: Makes, In: s.At.Makes, Why: text(m.Expr) + " " + s.Makes + ", and one value would stand for several"}
		}
	}
	if len(m.Past) == 0 {
		return nil
	}
	p := m.Effects.OfNodes(m.Func, m.Past)
	reads := m.Func.LocalsRead(m.Expr)
	written, where := writtenLocal(m.Func, m.Past, reads)
	switch {
	case s.Effect != "":
		if first := p.first(); first != nil || where != nil {
			if first == nil {
				first = where
			}
			return &Obstacle{Kind: Acts, In: s.At.Effect, Past: first, Why: text(m.Expr) + " " + s.Effect + ", and moving it past " + text(first) + " changes the order of what happens"}
		}
	}
	if written != nil {
		return &Obstacle{Kind: Reads, In: useOf(m.Func.Info, m.Expr, written), Past: where, Why: text(m.Expr) + " reads " + written.Name() + ", which " + text(where) + " changes"}
	}
	if s.Reads != "" && p.Effect != "" {
		return &Obstacle{Kind: Reads, In: s.At.Reads, Past: p.At.Effect, Why: text(m.Expr) + " " + s.Reads + ", which " + text(p.At.Effect) + " could change (it " + p.Effect + ")"}
	}
	if panics != NoPanic {
		switch {
		case p.Stops != "":
			return &Obstacle{Kind: Panics, In: panicAt, Past: p.At.Stops, Why: text(m.Expr) + " can panic (" + s.Unsafe + "), and " + text(p.At.Stops) + " " + p.Stops}
		case transfers(m.Past) != nil:
			t := transfers(m.Past)
			return &Obstacle{Kind: Panics, In: panicAt, Past: t, Why: text(m.Expr) + " can panic (" + s.Unsafe + "), and would no longer before " + text(t)}
		case p.Effect != "" && (panics == OtherPanic || p.Panics != NoPanic):
			return &Obstacle{Kind: Panics, In: panicAt, Past: p.At.Effect, Why: text(m.Expr) + " can panic (" + s.Unsafe + "), and " + text(p.At.Effect) + " " + p.Effect + ", which would then happen in another order"}
		case p.Panics != NoPanic && (panics == OtherPanic || p.Panics == OtherPanic):
			return &Obstacle{Kind: Panics, In: panicAt, Past: p.At.Panic, Why: text(m.Expr) + " can panic (" + s.Unsafe + "), and so can " + text(p.At.Panic) + " (it " + p.Unsafe + "); which panics first would change"}
		}
	}
	return nil
}

// EvaluatedBefore returns the code inside root that is evaluated before
// pos: the largest nodes that end before it and do not enclose it, in
// order, skipping the node skip. Bodies of function literals are kept
// (they run only when called, which a summary of them says).
func EvaluatedBefore(root ast.Node, pos token.Pos, skip ast.Node) []ast.Node {
	var out []ast.Node
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil || n.Pos() >= pos || n == skip {
			return false
		}
		if n.End() <= pos {
			out = append(out, n)
			return false
		}
		return true // it encloses pos; look inside
	})
	return out
}

// first returns where the first thing s found is, or nil when it found
// nothing that another evaluation could notice.
func (s Summary) first() ast.Node {
	var first ast.Node
	for _, n := range []ast.Node{s.At.Effect, s.At.Reads, s.At.Unsafe} {
		if n != nil && (first == nil || n.Pos() < first.Pos()) {
			first = n
		}
	}
	return first
}

// OfNodes summarizes what evaluating nodes, statements or expressions in
// the body of f, can do, as Of does for one expression.
func (e *Effects) OfNodes(f *Func, nodes []ast.Node) Summary {
	b := e.walker(f, nil)
	for _, n := range nodes {
		b.root = nil
		ast.Inspect(n, b.visit)
	}
	return b.s
}

// writtenLocal returns the first of the locals vars that the code in
// nodes assigns, and where, or nil.
func writtenLocal(f *Func, nodes []ast.Node, vars []*types.Var) (*types.Var, ast.Node) {
	var v *types.Var
	var at ast.Node
	note := func(x ast.Expr, n ast.Node) {
		if at != nil {
			return
		}
		r := f.RootVar(x)
		if r == nil {
			return
		}
		for _, w := range vars {
			if w == r {
				v, at = w, n
				return
			}
		}
	}
	for _, root := range nodes {
		ast.Inspect(root, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncLit:
				return false
			case *ast.AssignStmt:
				for _, l := range n.Lhs {
					note(l, n)
				}
			case *ast.IncDecStmt:
				note(n.X, n)
			case *ast.RangeStmt:
				for _, x := range []ast.Expr{n.Key, n.Value} {
					if x != nil && n.Tok != token.DEFINE {
						note(x, n)
					}
				}
			case *ast.UnaryExpr:
				if n.Op == token.AND {
					note(n.X, n) // a pointer to it is made; it may be written through
				}
			}
			return at == nil
		})
	}
	return v, at
}

// useOf returns the first identifier in x that refers to v.
func useOf(info *types.Info, x ast.Expr, v *types.Var) ast.Node {
	var found ast.Node
	ast.Inspect(x, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && found == nil && info.Uses[id] == v {
			found = id
		}
		return found == nil
	})
	return found
}

// transfers returns the first statement in nodes that leaves the code
// around it (return, break, continue, goto), or nil.
func transfers(nodes []ast.Node) ast.Node {
	var found ast.Node
	for _, root := range nodes {
		ast.Inspect(root, func(n ast.Node) bool {
			switch n.(type) {
			case *ast.FuncLit:
				return false
			case *ast.ReturnStmt, *ast.BranchStmt:
				if found == nil {
					found = n
				}
			}
			return found == nil
		})
	}
	return found
}
