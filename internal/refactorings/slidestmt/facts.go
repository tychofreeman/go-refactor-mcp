package slidestmt

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
)

// facts are what one statement does that its order relative to another
// could change.
type facts struct {
	line int
	// declares holds what it declares in its block's scope, visible to
	// the statements after it.
	declares map[types.Object]bool
	// refs lists the objects its identifiers refer to, in order; outer
	// holds the names of those it does not declare itself.
	refs  []types.Object
	outer map[string]bool
	// accesses and writes hold, for each local variable that only this
	// function's own code can reach, the parts it uses and assigns: ""
	// for the whole variable, ".f" for its field f.
	accesses map[*types.Var][]string
	writes   []access
	// The first thing it does of each kind, described ("calls f"), or "":
	effect    string // a side effect
	shared    string // a read of what other code can change
	unsafe    string // something that could panic or not finish
	jump      string // how it leaves the block's flow ("returns")
	crossable bool   // a statement may move past its jump
}

// access is a part of a local variable.
type access struct {
	v    *types.Var
	path string
}

func (a access) String() string { return a.v.Name() + a.path }

// overlaps reports whether two parts of a variable share memory: one is
// the other or contains it.
func overlaps(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+".") || strings.HasPrefix(b, a+".")
}

// escapes reports whether code outside the statement that uses v can
// reach v: its address is taken, or a closure that can run later uses it.
// A closure that runs only during its statement (passed to ast.Inspect,
// say) is part of that statement.
func escapes(f *analysis.Func, v *types.Var) bool {
	return f.Addressed[v] || f.CapturedAsync[v]
}

// factsOf computes the facts for s, a statement of f's body.
func factsOf(f *analysis.Func, tf *token.File, s ast.Stmt, effects *analysis.Effects) *facts {
	info := f.Info
	ft := &facts{
		line:     tf.Line(s.Pos()),
		declares: map[types.Object]bool{},
		outer:    map[string]bool{},
		accesses: map[*types.Var][]string{},
	}
	ft.addDeclared(info, s)
	parents := parentsIn(s)
	seen := map[types.Object]bool{}
	for _, id := range analysis.ScopeRefs(info, s) {
		o := info.Uses[id]
		if o == nil {
			continue
		}
		if !seen[o] {
			seen[o] = true
			ft.refs = append(ft.refs, o)
		}
		if o.Pos() < s.Pos() || o.Pos() >= s.End() {
			ft.outer[o.Name()] = true
		}
		if v, ok := o.(*types.Var); ok && f.IsLocal(v) && !escapes(f, v) {
			ft.accesses[v] = append(ft.accesses[v], pathAbove(info, id, parents))
		}
	}
	for v, nodes := range f.Modified {
		if escapes(f, v) {
			continue
		}
		for _, n := range nodes {
			if n.Pos() >= s.Pos() && n.End() <= s.End() {
				ft.writes = append(ft.writes, access{v, pathOf(info, n.(ast.Expr))})
			}
		}
	}
	sort.Slice(ft.writes, func(i, j int) bool { return ft.writes[i].v.Pos() < ft.writes[j].v.Pos() })
	ft.jump, ft.crossable = leaves(s)
	ft.addEffects(f, s, effects)
	return ft
}

// addDeclared records what s declares in its block's scope.
func (ft *facts) addDeclared(info *types.Info, s ast.Stmt) {
	def := func(id *ast.Ident) {
		if o := info.Defs[id]; o != nil {
			ft.declares[o] = true
		}
	}
	switch s := s.(type) {
	case *ast.AssignStmt:
		if s.Tok == token.DEFINE {
			for _, l := range s.Lhs {
				if id, ok := l.(*ast.Ident); ok {
					def(id)
				}
			}
		}
	case *ast.DeclStmt:
		for _, spec := range s.Decl.(*ast.GenDecl).Specs {
			switch spec := spec.(type) {
			case *ast.ValueSpec:
				for _, id := range spec.Names {
					def(id)
				}
			case *ast.TypeSpec:
				def(spec.Name)
			}
		}
	}
}

// parentsIn maps each node inside root to its parent.
func parentsIn(root ast.Node) map[ast.Node]ast.Node {
	parents := map[ast.Node]ast.Node{}
	var stack []ast.Node
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if len(stack) > 0 {
			parents[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})
	return parents
}

// pathAbove returns the part of its variable that id is used for: the
// fields selected from it (.f.g), or "" for the whole variable.
func pathAbove(info *types.Info, id *ast.Ident, parents map[ast.Node]ast.Node) string {
	path := ""
	var n ast.Node = id
	for {
		p := parents[n]
		if paren, ok := p.(*ast.ParenExpr); ok {
			n = paren
			continue
		}
		sel, ok := p.(*ast.SelectorExpr)
		if !ok || sel.X != n || !isFieldOfValue(info, sel) {
			return path
		}
		path += "." + sel.Sel.Name
		n = sel
	}
}

// pathOf returns the part of a local variable that the assigned
// expression e is: its fields (.f.g), or "" for all of it.
func pathOf(info *types.Info, e ast.Expr) string {
	switch e := ast.Unparen(e).(type) {
	case *ast.SelectorExpr:
		if isFieldOfValue(info, e) {
			return pathOf(info, e.X) + "." + e.Sel.Name
		}
	case *ast.IndexExpr:
		return pathOf(info, e.X)
	}
	return ""
}

// isFieldOfValue reports whether sel selects a field of a struct value,
// not through a pointer.
func isFieldOfValue(info *types.Info, sel *ast.SelectorExpr) bool {
	s := info.Selections[sel]
	return s != nil && s.Kind() == types.FieldVal && !s.Indirect() && !analysis.IsPointer(info.TypeOf(sel.X))
}

// leaves describes how s transfers control out of itself, or returns "";
// crossable reports whether a statement may move past it (it returns,
// breaks or continues, rather than using goto or being a label).
func leaves(s ast.Stmt) (how string, crossable bool) {
	if _, ok := s.(*ast.LabeledStmt); ok {
		return "is labeled", false
	}
	labels := map[string]bool{}
	ast.Inspect(s, func(n ast.Node) bool {
		if l, ok := n.(*ast.LabeledStmt); ok {
			labels[l.Label.Name] = true
		}
		return true
	})
	crossable = true
	var stack []ast.Node
	ast.Inspect(s, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok || how != "" {
			return false
		}
		switch n := n.(type) {
		case *ast.ReturnStmt:
			how = "returns"
		case *ast.BranchStmt:
			switch {
			case n.Tok == token.GOTO || n.Tok == token.FALLTHROUGH:
				how, crossable = "jumps out of it ("+n.Tok.String()+")", false
			case n.Label != nil:
				if !labels[n.Label.Name] {
					how = "jumps out of it (" + n.Tok.String() + " " + n.Label.Name + ")"
				}
			case !encloses(stack, n.Tok):
				how = "jumps out of it (" + n.Tok.String() + ")"
			}
		}
		stack = append(stack, n)
		return true
	})
	return how, crossable
}

// encloses reports whether a statement on the stack is what an
// unlabeled break or continue (tok) refers to.
func encloses(stack []ast.Node, tok token.Token) bool {
	for _, n := range stack {
		switch n.(type) {
		case *ast.ForStmt, *ast.RangeStmt:
			return true
		case *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
			if tok == token.BREAK {
				return true
			}
		}
	}
	return false
}

// addEffects records s's first side effect, read of shared state, and
// operation that could panic or not finish. Defining a function literal
// runs nothing, so their bodies are skipped, except for those passed to a
// function that runs them before it returns (ast.Inspect, sort.Slice).
func (ft *facts) addEffects(f *analysis.Func, s ast.Stmt, effects *analysis.Effects) {
	info := f.Info
	commaOK := commaOKAssertions(s)
	set := func(field *string, what string) {
		if *field == "" {
			*field = what
		}
	}
	assigns := func(e ast.Expr) {
		if id, ok := e.(*ast.Ident); ok && id.Name == "_" {
			return
		}
		if v := f.RootVar(e); v == nil || escapes(f, v) {
			set(&ft.effect, "assigns to "+types.ExprString(e))
		}
	}
	walk := map[*ast.FuncLit]bool{} // literals that run during s
	ast.Inspect(s, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return walk[n]
		case *ast.GoStmt:
			set(&ft.effect, "starts a goroutine")
		case *ast.DeferStmt:
			set(&ft.effect, "defers a call")
		case *ast.SendStmt:
			set(&ft.effect, "sends on a channel")
		case *ast.SelectStmt:
			set(&ft.effect, "selects on channels")
		case *ast.ForStmt:
			set(&ft.unsafe, "is a loop that might not finish")
		case *ast.RangeStmt:
			switch info.TypeOf(n.X).Underlying().(type) {
			case *types.Chan:
				set(&ft.effect, "receives from a channel")
			case *types.Signature:
				set(&ft.effect, "calls an iterator")
			case *types.Slice, *types.Map, *types.Pointer:
				set(&ft.shared, "reads the elements of "+types.ExprString(n.X))
			}
			if n.Tok == token.ASSIGN {
				for _, e := range []ast.Expr{n.Key, n.Value} {
					if e != nil {
						assigns(e)
					}
				}
			}
		case *ast.AssignStmt:
			for _, l := range n.Lhs {
				if id, ok := l.(*ast.Ident); ok && n.Tok == token.DEFINE && info.Defs[id] != nil {
					continue
				}
				assigns(l)
			}
		case *ast.IncDecStmt:
			assigns(n.X)
		case *ast.CallExpr:
			c := effects.Call(info, n)
			set(&ft.effect, c.Effect)
			set(&ft.shared, c.Reads)
			set(&ft.unsafe, c.Unsafe)
			for _, lit := range analysis.SyncLits(info, n) {
				walk[lit] = true // it runs now: what it does is part of s
			}
		case *ast.UnaryExpr:
			if n.Op == token.ARROW {
				set(&ft.effect, "receives from a channel")
			}
		case *ast.StarExpr:
			if !info.Types[n].IsType() {
				set(&ft.shared, "reads through the pointer "+types.ExprString(n.X))
				set(&ft.unsafe, "dereferences "+types.ExprString(n.X))
			}
		case *ast.SelectorExpr:
			if sel := info.Selections[n]; sel != nil && sel.Kind() == types.FieldVal && !isFieldOfValue(info, n) {
				set(&ft.shared, "reads "+types.ExprString(n)+" through a pointer")
				set(&ft.unsafe, "reads "+types.ExprString(n)+" through a pointer")
			}
		case *ast.IndexExpr:
			tv := info.Types[n.X]
			if tv.IsType() {
				break
			}
			switch tv.Type.Underlying().(type) {
			case *types.Signature: // instantiating a generic function
			case *types.Map:
				set(&ft.shared, "reads the map element "+types.ExprString(n))
			case *types.Array:
				if info.Types[n.Index].Value == nil {
					set(&ft.unsafe, "indexes "+types.ExprString(n.X))
				}
			case *types.Basic: // a string
				set(&ft.unsafe, "indexes "+types.ExprString(n.X))
			default:
				set(&ft.shared, "reads the element "+types.ExprString(n))
				set(&ft.unsafe, "indexes "+types.ExprString(n.X))
			}
		case *ast.SliceExpr:
			set(&ft.unsafe, "slices "+types.ExprString(n.X))
		case *ast.TypeAssertExpr:
			if n.Type != nil && !commaOK[n] {
				set(&ft.unsafe, "asserts the type of "+types.ExprString(n.X))
			}
		case *ast.BinaryExpr:
			if n.Op == token.QUO || n.Op == token.REM {
				if b, ok := info.TypeOf(n).Underlying().(*types.Basic); ok && b.Info()&types.IsInteger != 0 && info.Types[n.Y].Value == nil {
					set(&ft.unsafe, "divides by "+types.ExprString(n.Y))
				}
			}
		case *ast.Ident:
			if v, ok := info.Uses[n].(*types.Var); ok && !v.IsField() {
				if !f.IsLocal(v) {
					set(&ft.shared, "reads the package variable "+v.Name())
				} else if escapes(f, v) {
					set(&ft.shared, "reads "+v.Name()+", which a closure or pointer can change")
				}
			}
		}
		return true
	})
}

// commaOKAssertions returns the type assertions in s that are the value
// of a two-variable assignment or declaration (v, ok := x.(T)), which
// cannot panic.
func commaOKAssertions(s ast.Stmt) map[*ast.TypeAssertExpr]bool {
	commaOK := map[*ast.TypeAssertExpr]bool{}
	ast.Inspect(s, func(n ast.Node) bool {
		var lhs, rhs int
		var value ast.Expr
		switch n := n.(type) {
		case *ast.AssignStmt:
			lhs, rhs = len(n.Lhs), len(n.Rhs)
			if rhs == 1 {
				value = n.Rhs[0]
			}
		case *ast.ValueSpec:
			lhs, rhs = len(n.Names), len(n.Values)
			if rhs == 1 {
				value = n.Values[0]
			}
		}
		if ta, ok := ast.Unparen(value).(*ast.TypeAssertExpr); ok && lhs == 2 && rhs == 1 {
			commaOK[ta] = true
		}
		return true
	})
	return commaOK
}

// checkJumps refuses to swap a and b when one of them leaves the block's
// flow and the other could tell whether it ran: only a statement that
// does nothing but declare new variables, with no side effects and
// nothing that could panic or not finish, may move past a return, break
// or continue. (An assignment could be seen through a named result, or
// after the loop.)
func checkJumps(a, b *facts) error {
	for _, p := range [][2]*facts{{a, b}, {b, a}} {
		j, o := p[0], p[1]
		why := ""
		switch {
		case j.jump == "":
			continue
		case !j.crossable || o.jump != "":
			return refactor.Refusef("the statement on line %d %s, so moving it would change which code runs", j.line, j.jump)
		case o.effect != "":
			why = "has side effects (it " + o.effect + ")"
		case o.unsafe != "":
			why = "could panic or not finish (it " + o.unsafe + ")"
		case len(o.writes) > 0:
			why = "assigns " + o.writes[0].String()
		default:
			continue
		}
		return refactor.Refusef("the statement on line %d %s, so line %d would run in a case it did not, or the reverse, and it %s", j.line, j.jump, o.line, why)
	}
	return nil
}

// checkIndependent refuses to swap a and b (a first) when that could
// change what the code does.
func checkIndependent(a, b *facts) error {
	if err := checkJumps(a, b); err != nil {
		return err
	}
	for _, o := range b.refs {
		if a.declares[o] {
			return refactor.Refusef("line %d uses %s, which line %d declares", b.line, o.Name(), a.line)
		}
	}
	for o := range b.declares {
		if a.outer[o.Name()] {
			return refactor.Refusef("line %d declares %s, which line %d refers to; after the move, %s there would be the new one", b.line, o.Name(), a.line, o.Name())
		}
	}
	for _, p := range [][2]*facts{{a, b}, {b, a}} {
		w, o := p[0], p[1]
		for _, wr := range w.writes {
			for _, path := range o.accesses[wr.v] {
				if overlaps(wr.path, path) {
					return refactor.Refusef("line %d assigns %s, which line %d uses", w.line, wr, o.line)
				}
			}
		}
	}
	for _, p := range [][2]*facts{{a, b}, {b, a}} {
		e, o := p[0], p[1]
		switch {
		case e.effect == "":
		case o.effect != "":
			return refactor.Refusef("line %d has side effects (it %s), and so does line %d (it %s), so their order matters", e.line, e.effect, o.line, o.effect)
		case o.shared != "":
			return refactor.Refusef("line %d has side effects (it %s) that could change what line %d reads (it %s)", e.line, e.effect, o.line, o.shared)
		case o.unsafe != "":
			return refactor.Refusef("line %d has side effects (it %s), and line %d could panic or not finish (it %s), so the side effects would happen in a case they did not, or the reverse", e.line, e.effect, o.line, o.unsafe)
		}
	}
	if a.unsafe != "" && b.unsafe != "" {
		return refactor.Refusef("both could panic or not finish (line %d %s; line %d %s), and which one does first would change", a.line, a.unsafe, b.line, b.unsafe)
	}
	return nil
}
