package slidestmt

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
)

// facts are what one statement does that its order relative to another
// could change.
type facts struct {
	line int
	stmt ast.Stmt
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
	accesses  map[*types.Var][]string
	writes    []access
	jump      string // how it leaves the block's flow ("returns")
	crossable bool   // a statement may move past its jump
}

// access is a part of a local variable.
type access struct {
	v    *types.Var
	path string
}

func (a access) String() string { return a.v.Name() + a.path }

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
		stmt:     s,
		declares: map[types.Object]bool{},
		outer:    map[string]bool{},
		accesses: map[*types.Var][]string{},
	}
	ft.addDeclared(info, s)
	parents := analysis.ParentsIn(s)
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
			ft.accesses[v] = append(ft.accesses[v], analysis.PathAbove(info, id, parents))
		}
	}
	for v, nodes := range f.Modified {
		if escapes(f, v) {
			continue
		}
		for _, n := range nodes {
			if n.Pos() >= s.Pos() && n.End() <= s.End() {
				ft.writes = append(ft.writes, access{v, analysis.PathOf(info, n.(ast.Expr))})
			}
		}
	}
	sort.Slice(ft.writes, func(i, j int) bool { return ft.writes[i].v.Pos() < ft.writes[j].v.Pos() })
	ft.jump, ft.crossable = leaves(s)
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

// checkJumps refuses to swap a and b when one of them leaves the block's
// flow and the other could tell whether it ran: only a statement that
// does nothing but declare new variables, with no side effects and
// nothing that could panic or not finish, may move past a return, break
// or continue. (An assignment could be seen through a named result, or
// after the loop.)
func checkJumps(a, b *facts, f *analysis.Func, effects *analysis.Effects) error {
	for _, p := range [][2]*facts{{a, b}, {b, a}} {
		j, o := p[0], p[1]
		why := ""
		m := analysis.Move{Effects: effects, Func: f, Expr: o.stmt, Times: analysis.Maybe}
		switch obs := m.Check(); {
		case j.jump == "":
			continue
		case !j.crossable || o.jump != "":
			return refactor.Refusef("the statement on line %d %s, so moving it would change which code runs", j.line, j.jump)
		case obs != nil:
			why = obs.Does
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
// change what the code does: each is a move of one statement past the
// other, and what one declares or assigns the other may not use.
func checkIndependent(a, b *facts, f *analysis.Func, effects *analysis.Effects) error {
	if err := checkJumps(a, b, f, effects); err != nil {
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
				if analysis.Overlaps(wr.path, path) {
					return refactor.Refusef("line %d assigns %s, which line %d uses", w.line, wr, o.line)
				}
			}
		}
	}
	for _, p := range [][2]*facts{{a, b}, {b, a}} {
		e, o := p[0], p[1]
		m := analysis.Move{Effects: effects, Func: f, Expr: e.stmt, Past: []ast.Node{o.stmt}}
		if obs := m.Check(); obs != nil {
			return refactor.Refusef("line %d %s, and line %d %s, so swapping them would change what happens", e.line, obs.Does, o.line, obs.PastDoes)
		}
	}
	return nil
}
