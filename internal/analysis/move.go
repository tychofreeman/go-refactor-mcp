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
	Shared               // once, with the one value standing for several evaluations
	Unused               // once more or once less, where nothing uses the value (an argument added or dropped)
)

// Move is a change to where, or how often, an expression is evaluated:
// Expr, an expression in the body of Func, would be evaluated past the
// code in Past (statements or expressions that run between its old and
// new places, in either direction), and Times as often as now. Check says
// whether the program would still do the same.
type Move struct {
	Effects      *Effects
	Func         *Func
	Expr         ast.Node // what moves: an expression, or a statement
	Past         []ast.Node
	PastFunc     *Func // the function Past belongs to, when not Func: an argument moving into an inlined body
	Times        Times
	Dereferenced bool // every use dereferences the value, so a nil dereference in Expr panics there anyway and may move
}

// Obstacle is why a Move would change behavior: Kind, what the
// expression does that cannot move, and In, the part of it that does
// so, with Does saying what ("reads p.n through a pointer"); Past, what
// it cannot move past, with PastDoes saying what that does ("has side
// effects", "leaves"), or nil when the number of evaluations alone is
// the problem; and Why, the whole as a sentence.
type Obstacle struct {
	Kind     Problem
	In, Past ast.Node
	Does     string
	PastDoes string
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
	var s Summary
	if x, ok := m.Expr.(ast.Expr); ok {
		s = m.Effects.Of(m.Func, x)
	} else {
		s = m.Effects.OfNodes(m.Func, []ast.Node{m.Expr})
	}
	panics, panicAt := s.Panics, s.At.Panic
	if m.Dereferenced && panics == NilPanic {
		panics, panicAt = NoPanic, nil // the use panics the same way
	} else if m.Dereferenced {
		panicAt = s.At.OtherPanic
	}
	q := nodeText(m.Expr)
	acts := Obstacle{Kind: Acts, In: s.At.Effect, Does: "has side effects (" + s.Effect + ")"}
	panicking := Obstacle{Kind: Panics, In: panicAt, Does: "could panic: it " + s.Unsafe}
	reads := Obstacle{Kind: Reads, In: s.At.Reads, Does: s.Reads}
	switch m.Times {
	case Maybe, Unused:
		then := ", which would then not always happen"
		if m.Times == Unused {
			then = ", which would then happen another number of times"
		}
		if s.Effect != "" {
			return acts.why(q + " " + acts.Does + then)
		}
		if panics != NoPanic {
			return panicking.why(q + " " + panicking.Does + then)
		}
	case Several, Shared:
		then := ", which would then happen another number of times"
		if s.Effect != "" {
			return acts.why(q + " " + acts.Does + then)
		}
		if s.Reads != "" {
			return reads.why(q + " " + s.Reads + ", which could change between evaluations")
		}
		if panics != NoPanic && m.Times == Several { // shared, it panics the first time either way
			return panicking.why(q + " " + panicking.Does + then)
		}
		if s.Makes != "" {
			return (&Obstacle{Kind: Makes, In: s.At.Makes, Does: s.Makes}).why(q + " " + s.Makes + ", and each evaluation makes another")
		}
	}
	if len(m.Past) == 0 {
		return nil
	}
	pf := m.PastFunc
	if pf == nil {
		pf = m.Func
	}
	p := m.Effects.OfNodes(pf, m.Past)
	written, where := writtenLocal(pf, m.Func, m.Past, m.Expr)
	before := func(o Obstacle, past ast.Node, does string) *Obstacle {
		o.Past, o.PastDoes = past, does
		return o.why(q + " " + o.Does + ", and " + nodeText(past) + " " + does + " before it")
	}
	if s.Effect != "" {
		switch {
		case p.Effect != "":
			return before(acts, p.At.Effect, p.Effect)
		case p.Reads != "":
			return before(acts, p.At.Reads, p.Reads)
		case p.Stops != "":
			return before(acts, p.At.Stops, p.Stops)
		case p.Panics == OtherPanic: // a nil dereference panics either way, only what is printed before differs
			return before(acts, p.At.OtherPanic, "could panic (it "+p.Unsafe+")")
		case where != nil:
			return before(acts, where, "assigns "+written.Name())
		}
	}
	if written != nil {
		reads.In, reads.Does = useOf(m.Func.Info, m.Expr, written), "reads "+written.Name()
		return before(reads, where, "assigns "+written.Name())
	}
	if s.Reads != "" && p.Effect != "" {
		return before(reads, p.At.Effect, p.Effect)
	}
	if panics != NoPanic {
		switch {
		case p.Stops != "":
			return before(panicking, p.At.Stops, p.Stops)
		case transfers(m.Past) != nil:
			return before(panicking, transfers(m.Past), "leaves")
		case p.Effect != "" && panics == OtherPanic:
			return before(panicking, p.At.Effect, p.Effect)
		case p.Panics != NoPanic && (panics == OtherPanic || p.Panics == OtherPanic):
			return before(panicking, p.At.Panic, "could panic (it "+p.Unsafe+")")
		}
	}
	return nil
}

// why returns o with its sentence.
func (o Obstacle) why(sentence string) *Obstacle {
	o.Why = sentence
	return &o
}

// nodeText is the source-like text of an expression, or a word for a
// statement.
func nodeText(n ast.Node) string {
	switch n := n.(type) {
	case ast.Expr:
		return types.ExprString(n)
	case *ast.ReturnStmt:
		return "return"
	case *ast.BranchStmt:
		return n.Tok.String()
	case *ast.ExprStmt:
		return types.ExprString(n.X)
	case *ast.AssignStmt:
		return types.ExprString(n.Lhs[0]) + " " + n.Tok.String() + " " + types.ExprString(n.Rhs[0])
	case *ast.IncDecStmt:
		return types.ExprString(n.X) + n.Tok.String()
	}
	return "the code"
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

// EvaluatedAfter returns the code inside root that is evaluated after
// pos: the largest nodes that start at or after it, in order.
func EvaluatedAfter(root ast.Node, pos token.Pos) []ast.Node {
	var out []ast.Node
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil || n.End() <= pos {
			return false
		}
		if n.Pos() >= pos {
			out = append(out, n)
			return false
		}
		return true // it encloses pos; look inside
	})
	return out
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

// writtenLocal returns the first write in nodes to a part of a local
// variable that x reads (outside function literals, which run later if
// at all), and where: an assignment, ++ or --, a range clause, taking its
// address, or a method with a pointer receiver called on it.
func writtenLocal(pf, f *Func, nodes []ast.Node, x ast.Node) (*types.Var, ast.Node) {
	reads := map[*types.Var][]string{}
	parents := ParentsIn(x)
	ast.Inspect(x, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if id, ok := n.(*ast.Ident); ok {
			if v, ok := f.Info.Uses[id].(*types.Var); ok && f.IsLocal(v) {
				reads[v] = append(reads[v], PathAbove(f.Info, id, parents))
			}
		}
		return true
	})
	var v *types.Var
	var at ast.Node
	note := func(e ast.Expr, n ast.Node) {
		if at != nil {
			return
		}
		r := pf.RootVar(e)
		if r == nil {
			return
		}
		path := PathOf(pf.Info, e)
		for _, p := range reads[r] {
			if Overlaps(p, path) {
				v, at = r, n
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
				for _, e := range []ast.Expr{n.Key, n.Value} {
					if e != nil && n.Tok != token.DEFINE {
						note(e, n)
					}
				}
			case *ast.UnaryExpr:
				if n.Op == token.AND {
					note(n.X, n) // a pointer to it is made; it may be written through
				}
			case *ast.CallExpr:
				if sel, ok := ast.Unparen(n.Fun).(*ast.SelectorExpr); ok {
					if s := pf.Info.Selections[sel]; s != nil && s.Kind() == types.MethodVal && !IsPointer(pf.Info.TypeOf(sel.X)) && !valueReceiver(s) {
						note(sel.X, n) // called through its address
					}
				}
			}
			return at == nil
		})
	}
	return v, at
}

// useOf returns the first identifier in x that refers to v.
func useOf(info *types.Info, x ast.Node, v *types.Var) ast.Node {
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
