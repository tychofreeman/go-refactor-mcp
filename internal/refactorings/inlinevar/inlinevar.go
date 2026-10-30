// Package inlinevar implements "inline variable": replace every use of a
// local variable (or constant) with its initializer and delete the
// declaration. It is the inverse of extract_variable.
//
// Inlining moves the initializer's evaluation from the declaration to each
// use, so it can change how many times, when, and in which scope the
// expression is evaluated, and what type it has. The preconditions:
//
//   - the variable is never written and its address is never taken;
//   - if the initializer is "stable" (no calls, nothing other code can
//     change, no new slices or maps) it may be evaluated any number of times
//     anywhere, provided the variables it reads are not modified before a
//     use; otherwise the variable must have exactly one use, evaluated
//     exactly once in the same block, with no calls in between;
//   - every name in the initializer means the same thing at each use;
//   - the value keeps the variable's type: where the initializer's own type
//     differs (an untyped constant, an interface variable), the inlined
//     expression is converted to it.
package inlinevar

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"sort"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/site"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// InlineVariable is the "inline_variable" refactoring.
type InlineVariable struct{}

// New returns the inline_variable refactoring.
func New() *InlineVariable { return &InlineVariable{} }

func (*InlineVariable) Name() string { return "inline_variable" }

func (*InlineVariable) Description() string {
	return `Replace every use of a local variable or constant with its initializer, and delete the declaration: the inverse of extract_variable. name := u.First; return greet(name) becomes return greet(u.First). A pointer alias (p := &e.f) is inlined as e.f where it is dereferenced. Where the variable's type differs from its initializer's (an untyped constant, an interface), a conversion keeps it.
Identify the variable by file, line and name at its declaration or any use, or by file, function ("Name" or "Type.Method") and name, which stays valid later in a batch.
Refuses, saying why: a variable assigned after its declaration or whose address is taken; an initializer that would be evaluated a different number of times or in another order (one with calls needs a single use, with no calls in between); something it reads changed before a use; a name that would mean something else at a use.`
}

func (*InlineVariable) InputSchema() json.RawMessage {
	return refactor.Schema[args]()
}

// LocalsOnly marks it as finding only locals: without line, by function.
func (*InlineVariable) LocalsOnly() {}

type args struct {
	File     string `json:"file" desc:"Path of the file, relative to the module root."`
	Line     int    `json:"line,omitempty" desc:"1-based line of the variable's declaration or of any use."`
	Name     string `json:"name" desc:"The variable's name."`
	Function string `json:"function,omitempty" desc:"Instead of line: the function (Name or Type.Method) of file that declares the variable."`
	Column   int    `json:"column,omitempty" desc:"Optional 1-based column, only needed if the name occurs more than once on the line with different meanings."`
}

type inliner struct {
	site.Site
	fa *analysis.Func

	obj      types.Object
	init     ast.Expr
	declStmt ast.Stmt // the statement (or if/switch Init) that declares obj
	spec     ast.Node // the ValueSpec to delete from a grouped declaration, or nil
	owner    ast.Stmt // the if/switch whose Init is declStmt, or nil
	block    ast.Node // the block or case clause containing declStmt (or owner)
	uses     []*ast.Ident
}

// Plan checks preconditions and computes the edits.
func (*InlineVariable) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	pkg, id, err := locate.Ident(ws, locate.IdentQuery{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name, Function: a.Function})
	if err != nil {
		return nil, err
	}
	obj := pkg.TypesInfo.ObjectOf(id)
	if obj == nil {
		return nil, refactor.Refusef("%q at %s:%d is not a variable or constant that can be inlined", a.Name, a.File, a.Line)
	}
	s, err := site.At(ws, ws.Fset.Position(id.Pos()).Filename)
	if err != nil {
		return nil, err
	}
	x := &inliner{Site: *s, obj: obj}
	return x.plan()
}

func (x *inliner) plan() (*refactor.Plan, error) {
	if err := x.findDeclaration(); err != nil {
		return nil, err
	}
	if err := x.checkVariable(); err != nil {
		return nil, err
	}
	if err := x.checkReadsUnchanged(); err != nil {
		return nil, err
	}
	if err := x.checkEvaluation(); err != nil {
		return nil, err
	}
	if err := x.checkScopes(); err != nil {
		return nil, err
	}

	var edits []edit.TextEdit
	converted := false
	for _, u := range x.uses {
		n, text, conv, err := x.replacement(u)
		if err != nil {
			return nil, err
		}
		converted = converted || conv
		edits = append(edits, edit.TextEdit{Start: x.Off(n.Pos()), End: x.Off(n.End()), NewText: text})
	}
	edits = append(edits, x.deletion())

	kind := "variable"
	if _, ok := x.obj.(*types.Const); ok {
		kind = "constant"
	}
	var notes []string
	if converted {
		notes = append(notes, fmt.Sprintf("the expression was converted to %s where needed to keep the %s's type", x.typeName(), kind))
	}
	if c := x.commentAbove(); c != "" {
		notes = append(notes, fmt.Sprintf("the comment above the removed declaration (%s) was kept; delete or move it if it described %q", c, x.obj.Name()))
	}
	return &refactor.Plan{
		Summary: fmt.Sprintf("inlined %s %q as %s (%d use%s replaced, declaration removed)",
			kind, x.obj.Name(), x.Quote(x.init), len(x.uses), textutil.Plural(len(x.uses))),
		Edits: []edit.FileEdit{{Path: x.TF.Name(), Edits: edits}},
		Notes: notes,
	}, nil
}

// ---------------------------------------------------------------------------
// The declaration

func (x *inliner) findDeclaration() error {
	name := x.obj.Name()
	switch o := x.obj.(type) {
	case *types.Var:
		if o.IsField() {
			return refactor.Refusef("%q is a struct field; only local variables can be inlined", name)
		}
	case *types.Const:
	default:
		return refactor.Refusef("%q is a %s; only local variables and constants can be inlined", name, analysis.KindOf(x.obj))
	}
	if x.obj.Pkg() == nil || x.obj.Parent() == x.obj.Pkg().Scope() {
		return refactor.Refusef("%q is declared at package level; only variables and constants declared inside a function can be inlined", name)
	}
	if x.obj.Parent() == nil {
		return refactor.Refusef("%q has no declaring scope (it may be a type switch variable) and cannot be inlined", name)
	}
	var declID *ast.Ident
	ast.Inspect(x.File, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Pos() == x.obj.Pos() {
			declID = id
		}
		return declID == nil
	})
	if declID == nil {
		return refactor.Refusef("the declaration of %q is not in this file", name)
	}
	path := analysis.PathTo(x.File, declID)
	if _, ok := path[1].(*ast.Field); ok {
		return refactor.Refusef("%q is a parameter or result name; only local variables with an initializer can be inlined", name)
	}
	fn := analysis.OutermostFunc(path)
	if fn == nil {
		return refactor.Refusef("%q is not declared inside a function", name)
	}
	x.fa = analysis.AnalyzeFunc(x.Info, fn)

	together := fmt.Sprintf("%q is declared together with other names; only a variable declared on its own (x := expr) can be inlined", name)
	stmtIndex := -1
	switch d := path[1].(type) {
	case *ast.AssignStmt:
		if d.Tok != token.DEFINE {
			return refactor.Refusef("could not find the declaration of %q", name)
		}
		if len(d.Lhs) != 1 || len(d.Rhs) != 1 {
			return refactor.Refusef("%s", together)
		}
		x.init, x.declStmt, stmtIndex = d.Rhs[0], d, 1
	case *ast.ValueSpec:
		if len(d.Names) != 1 {
			return refactor.Refusef("%s", together)
		}
		if len(d.Values) == 0 {
			return refactor.Refusef("%q has no initializer (it starts as the zero value), so there is no expression to inline", name)
		}
		gd := path[2].(*ast.GenDecl)
		ds, ok := path[3].(*ast.DeclStmt)
		if !ok {
			return refactor.Refusef("could not find the declaration statement of %q", name)
		}
		x.init, x.declStmt, stmtIndex = d.Values[0], ds, 3
		if len(gd.Specs) > 1 {
			x.spec = d
		}
	case *ast.RangeStmt:
		return refactor.Refusef("%q is a range loop variable; only local variables with an initializer can be inlined", name)
	default:
		return refactor.Refusef("%q is not declared by a simple declaration (x := expr, var x = expr or const x = expr)", name)
	}

	switch p := path[stmtIndex+1].(type) {
	case *ast.BlockStmt, *ast.CaseClause:
		x.block = p
	case *ast.CommClause:
		if p.Comm == x.declStmt {
			return refactor.Refusef("%q is received by a select case and cannot be inlined", name)
		}
		x.block = p
	case *ast.IfStmt:
		x.owner = p
	case *ast.SwitchStmt:
		x.owner = p
	case *ast.TypeSwitchStmt:
		if p.Assign == x.declStmt {
			return refactor.Refusef("%q is a type switch variable and cannot be inlined", name)
		}
		x.owner = p
	case *ast.ForStmt:
		return refactor.Refusef("%q is declared in a for loop header and cannot be inlined", name)
	case *ast.LabeledStmt:
		return refactor.Refusef("%q is declared by a labeled statement (%s:), which is a goto or break target; removing it would move the label", name, p.Label.Name)
	default:
		return refactor.Refusef("cannot remove the declaration of %q from its context", name)
	}
	if x.owner != nil {
		x.block = path[stmtIndex+2]
	}

	tv := x.Info.Types[x.init]
	if _, ok := tv.Type.(*types.Tuple); ok {
		return refactor.Refusef("the initializer of %q returns several values", name)
	}

	for id, o := range x.Info.Uses {
		if o == x.obj {
			x.uses = append(x.uses, id)
		}
	}
	sort.Slice(x.uses, func(i, j int) bool { return x.uses[i].Pos() < x.uses[j].Pos() })
	if len(x.uses) == 0 {
		return refactor.Refusef("%q is never used; there is nothing to inline (delete the declaration instead)", name)
	}
	return nil
}

// checkVariable refuses variables that are written or whose identity
// matters.
func (x *inliner) checkVariable() error {
	v, ok := x.obj.(*types.Var)
	if !ok {
		return nil
	}
	if mods := x.fa.Modified[v]; len(mods) > 0 {
		return refactor.Refusef("%q is assigned at %s; only variables that are never changed after their declaration can be inlined",
			v.Name(), x.WS.Position(mods[0].Pos()))
	}
	if x.fa.Addressed[v] {
		return refactor.Refusef("%q must stay a variable: its address is taken, it is sliced, or a pointer-receiver method is called on it", v.Name())
	}
	return nil
}

// ---------------------------------------------------------------------------
// Evaluation count and order

// checkEvaluation ensures the initializer is evaluated as often and in the
// same order relative to other effects after inlining: a move of the
// initializer to its use, or, for several uses, one value shared by all.
func (x *inliner) checkEvaluation() error {
	m := x.move()
	m.Times = analysis.Several // each use evaluates it again
	if len(x.uses) > 1 && x.owner == nil {
		uses := make([]ast.Node, len(x.uses))
		for i, u := range x.uses {
			uses[i] = u
		}
		m.Past = analysis.Spanning(x.block, x.declStmt.End(), x.uses[len(x.uses)-1].Pos(), uses...)
		if m.Past != nil {
			// What it reads must not change between the uses; it may
			// panic only where the declaration would have.
			m.Between, m.Times = true, analysis.Shared
		}
	}
	o := m.Check()
	if o == nil && m.Between {
		o = x.firstUse()
	}
	if o == nil {
		return nil // same value and no effects however often and wherever it is evaluated
	}
	if len(x.uses) > 1 {
		if o.Kind == analysis.Panics {
			return refactor.Refusef("%q is used %d times, and its initializer %s can panic (%s), which inlining would move to the first use; inlining requires a single use",
				x.obj.Name(), len(x.uses), x.Quote(x.init), x.Quote(o.In))
		}
		return refactor.Refusef("%q is used %d times, and its initializer %s could give a different value or do something each time it is evaluated (because of %s); inlining requires a single use",
			x.obj.Name(), len(x.uses), x.Quote(x.init), x.Quote(o.In))
	}
	use := x.uses[0]
	stmt, err := x.evaluatedOnce(use)
	if err != nil {
		return err
	}
	m.Times, m.Past, m.Between = analysis.Once, nil, false
	if x.owner == nil {
		m.Past = x.between(stmt)
	}
	between := len(m.Past)
	m.Past = append(m.Past, analysis.EvaluatedBefore(stmt, use.Pos(), x.declStmt)...)
	o = m.Check()
	if o == nil {
		return nil
	}
	for _, s := range m.Past[:between] {
		if s.Pos() <= o.Past.Pos() && o.Past.End() <= s.End() {
			return refactor.Refusef("%s (at %s) runs between the declaration of %q and its use; inlining would move %s after it, which could change behavior (%s)",
				x.Quote(o.Past), x.WS.Position(o.Past.Pos()), x.obj.Name(), x.Quote(x.init), o.Why)
		}
	}
	return refactor.Refusef("inlining would evaluate %s after %s (at %s), which comes first in the statement that uses %q; that could change behavior (%s)",
		x.Quote(x.init), x.Quote(o.Past), x.WS.Position(o.Past.Pos()), x.obj.Name(), o.Why)
}

// move is the initializer's move from the declaration to its use: of
// the initializer, or, for a pointer alias, of the address it holds.
func (x *inliner) move() analysis.Move {
	moved := x.init
	if e := x.aliased(); e != nil {
		moved = &ast.UnaryExpr{Op: token.AND, X: e}
	}
	return analysis.Move{Effects: x.WS.Effects(), Func: x.fa, Expr: moved, Dereferenced: moved != x.init}
}

// firstUse returns why evaluating the initializer at its first use,
// rather than at the declaration, would change what the code does: the
// code between them could keep it from panicking where it did.
func (x *inliner) firstUse() *analysis.Obstacle {
	use := x.uses[0]
	stmts := analysis.Spanning(x.block, x.declStmt.End(), use.Pos())
	if len(stmts) == 0 {
		return nil
	}
	last := stmts[len(stmts)-1]
	m := x.move()
	m.Past = append(stmts[:len(stmts)-1:len(stmts)-1], analysis.EvaluatedBefore(last, use.Pos(), x.declStmt)...)
	return m.Check()
}

// between returns the statements of the block between the declaration
// and stmt.
func (x *inliner) between(stmt ast.Stmt) []ast.Node {
	var list []ast.Stmt
	switch b := x.block.(type) {
	case *ast.BlockStmt:
		list = b.List
	case *ast.CaseClause:
		list = b.Body
	case *ast.CommClause:
		list = b.Body
	}
	var out []ast.Node
	for _, s := range list {
		if s.Pos() > x.declStmt.Pos() && s.Pos() < stmt.Pos() {
			out = append(out, s)
		}
	}
	return out
}

// evaluatedOnce checks that use is evaluated exactly once each time the
// declaration is, at the top level of a statement in the same block (or in
// the if/switch header that declares the variable), and returns that
// statement.
func (x *inliner) evaluatedOnce(use *ast.Ident) (ast.Stmt, error) {
	scope := x.block
	if x.owner != nil {
		scope = x.owner
	}
	path := analysis.PathTo(x.File, use)
	end := slices.Index(path, scope)
	if end < 0 {
		return nil, refactor.Refusef("the use of %q at %s is not directly in the statement after its declaration, so the initializer %s would no longer run exactly once there",
			x.obj.Name(), x.WS.Position(use.Pos()), x.Quote(x.init))
	}
	if times, why := analysis.Reach(path[:end+1]); times != analysis.Once {
		return nil, refactor.Refusef("the use of %q at %s is %s, so the initializer %s would no longer run exactly once, right after the declaration",
			x.obj.Name(), x.WS.Position(use.Pos()), why, x.Quote(x.init))
	}
	child := path[end-1]
	switch o := x.owner.(type) {
	case nil:
		if s, ok := child.(ast.Stmt); ok {
			return s, nil
		}
	case *ast.IfStmt:
		if child == o.Cond {
			return o, nil
		}
	case *ast.SwitchStmt:
		if child == o.Tag {
			return o, nil
		}
	case *ast.TypeSwitchStmt:
		if child == o.Assign {
			return o, nil
		}
	}
	if x.owner != nil {
		return nil, refactor.Refusef("the use of %q at %s is not in the condition of the statement that declares it, so the initializer %s would no longer run exactly once",
			x.obj.Name(), x.WS.Position(use.Pos()), x.Quote(x.init))
	}
	return nil, refactor.Refusef("the use of %q at %s is not directly in the statement after its declaration, so the initializer %s would no longer run exactly once there",
		x.obj.Name(), x.WS.Position(use.Pos()), x.Quote(x.init))
}

// checkReadsUnchanged refuses if a local the initializer reads is modified
// after the declaration but before a use, or anywhere in a loop or closure
// that contains a use but not the declaration (where the use is evaluated
// again later).
func (x *inliner) checkReadsUnchanged() error {
	last := x.uses[len(x.uses)-1].Pos()
	for _, v := range x.reads() {
		for _, m := range x.fa.Modified[v] {
			if m.Pos() <= x.declStmt.End() {
				continue
			}
			if x.writtenAt(m) < last {
				return refactor.Refusef("%q, which the initializer of %q reads, is modified at %s before a use; the inlined expression would see the new value",
					v.Name(), x.obj.Name(), x.WS.Position(m.Pos()))
			}
			for _, u := range x.uses {
				if loop := x.repeatingAncestor(u); loop != nil && loop.Pos() <= m.Pos() && m.Pos() < loop.End() {
					return refactor.Refusef("%q, which the initializer of %q reads, is modified at %s inside the loop or function literal that uses it at %s; the inlined expression could see the new value",
						v.Name(), x.obj.Name(), x.WS.Position(m.Pos()), x.WS.Position(u.Pos()))
				}
			}
		}
	}
	return nil
}

// writtenAt returns the position at which the write m takes effect. The
// left side of an assignment is written after the whole right side has
// been evaluated, so "out = append(out, v)" reads out before writing it.
func (x *inliner) writtenAt(m ast.Node) token.Pos {
	path := analysis.PathTo(x.File, m)
	if len(path) > 1 {
		switch s := path[1].(type) {
		case *ast.AssignStmt:
			for _, l := range s.Lhs {
				if l == m {
					return s.End()
				}
			}
		case *ast.IncDecStmt:
			return s.End()
		}
	}
	return m.Pos()
}

// repeatingAncestor returns the outermost loop or function literal that
// contains u but not the declaration, or nil.
func (x *inliner) repeatingAncestor(u ast.Node) ast.Node {
	var out ast.Node
	for _, n := range analysis.PathTo(x.File, u) {
		if n.Pos() <= x.declStmt.Pos() && x.declStmt.End() <= n.End() {
			break
		}
		switch n.(type) {
		case *ast.ForStmt, *ast.RangeStmt, *ast.FuncLit:
			out = n
		}
	}
	return out
}

// checkScopes ensures every name in the initializer refers to the same
// thing at each use.
func (x *inliner) checkScopes() error {
	pkgScope := x.Pkg.Types.Scope()
	for _, u := range x.uses {
		inner := pkgScope.Innermost(u.Pos())
		if inner == nil {
			continue
		}
		for _, id := range analysis.ScopeRefs(x.Info, x.init) {
			want := x.Info.Uses[id]
			if want == nil || (want.Pos() >= x.init.Pos() && want.Pos() < x.init.End()) {
				continue // declared inside the initializer (function literal)
			}
			if _, got := inner.LookupParent(id.Name, u.Pos()); got != want {
				where := "is not visible there"
				if got != nil {
					where = fmt.Sprintf("refers to the %s declared at %s instead", analysis.KindOf(got), site.DeclaredAt(x.WS, got))
				}
				return refactor.Refusef("at the use of %q at %s, %q (used by its initializer %s) %s",
					x.obj.Name(), x.WS.Position(u.Pos()), id.Name, x.Quote(x.init), where)
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// The replacement text

// replacement returns the node to replace at use and the text to put
// there: the initializer, converted to the variable's type if needed and
// parenthesized if the context requires it; or, where a pointer alias is
// dereferenced, what it points to.
func (x *inliner) replacement(use *ast.Ident) (ast.Node, string, bool, error) {
	path := analysis.PathTo(x.File, use)
	parent := path[1]
	if e := x.aliased(); e != nil {
		if n, text, ok := x.dereference(path, e); ok {
			return n, text, false, nil
		}
	}
	text := x.Text(x.init)
	if err := x.checkConstantContext(use, parent); err != nil {
		return nil, "", false, err
	}
	if x.needsConversion(use, parent) {
		t := x.typeName()
		if strings.HasPrefix(t, "*") || strings.HasPrefix(t, "<-") || strings.HasPrefix(t, "func") || strings.HasPrefix(t, "chan") {
			t = "(" + t + ")"
		}
		if _, missing := analysis.TypeString(x.obj.Type(), x.File, x.Pkg.Types, analysis.ImportMap(x.Pkg.Types)); missing != "" {
			return nil, "", false, refactor.Refusef("the inlined expression needs a conversion to %s, but this file does not import %q", x.typeName(), missing)
		}
		return use, t + "(" + strings.TrimSpace(x.Text(ast.Unparen(x.init))) + ")", true, nil
	}
	if analysis.NeedsParens(ast.Unparen(x.init), parent, use) || analysis.InControlHeader(x.init, path) {
		return use, "(" + text + ")", false, nil
	}
	return use, text, false, nil
}

// aliased returns what a pointer alias points to: e, when the initializer
// is &e for an addressable e (not &T{...}, which makes a new value), or
// nil.
func (x *inliner) aliased() ast.Expr {
	u, ok := ast.Unparen(x.init).(*ast.UnaryExpr)
	if !ok || u.Op != token.AND || !x.Info.Types[u.X].Addressable() {
		return nil
	}
	return u.X
}

// dereference returns, for a use of a pointer alias that goes through the
// pointer (*p, p.f, p[i]), the node to replace and the expression it
// points to, written for that place.
func (x *inliner) dereference(path []ast.Node, target ast.Expr) (ast.Node, string, bool) {
	text := x.Text(target)
	n, parent := path[0].(ast.Expr), path[1]
	switch p := parent.(type) {
	case *ast.StarExpr:
		if paren, ok := path[2].(*ast.ParenExpr); ok && !analysis.NeedsParens(target, path[3], paren) {
			return paren, text, true // (*p).f becomes e.f
		}
		n, parent = p, path[2]
	case *ast.SelectorExpr:
		if p.X != n {
			return nil, "", false
		}
	case *ast.IndexExpr:
		if p.X != n {
			return nil, "", false
		}
	default:
		return nil, "", false
	}
	if analysis.NeedsParens(target, parent, n) {
		text = "(" + text + ")"
	}
	return n, text, true
}

// reads returns the locals the initializer's value depends on: for a
// pointer alias, those its address depends on.
func (x *inliner) reads() []*types.Var {
	e := x.aliased()
	if e == nil {
		return x.fa.LocalsRead(x.init)
	}
	ops, _ := x.fa.AddressOperands(e)
	var vs []*types.Var
	for _, op := range ops {
		vs = append(vs, x.fa.LocalsRead(op)...)
	}
	return vs
}

func (x *inliner) typeName() string {
	s, _ := analysis.TypeString(x.obj.Type(), x.File, x.Pkg.Types, analysis.ImportMap(x.Pkg.Types))
	return s
}

// needsConversion reports whether the initializer, put at use, could have a
// different type than the variable had there.
func (x *inliner) needsConversion(use *ast.Ident, parent ast.Node) bool {
	want := x.obj.Type()
	own := x.ownType()
	if types.Identical(own, want) {
		return false
	}
	if ctx := x.contextType(use, parent); ctx != nil && types.Identical(ctx, want) {
		return false // the context converts it to the variable's type anyway
	}
	if isUntyped(own) && types.Identical(types.Default(own), want) {
		// An untyped value takes its default type, the variable's type,
		// except where it combines with other untyped operands or is
		// shifted, which can change the arithmetic (7 / 2.0).
		switch p := parent.(type) {
		case *ast.BinaryExpr:
			other := p.X
			if other == ast.Expr(use) {
				other = p.Y
			}
			return x.maybeUntyped(other) || p.Op == token.SHL || p.Op == token.SHR
		case *ast.UnaryExpr, *ast.ParenExpr, *ast.StarExpr:
			return true
		}
		return false
	}
	return true
}

// ownType is the type the initializer has on its own, before any context
// converts it (untyped constants and comparisons stay untyped).
func (x *inliner) ownType() types.Type {
	return analysis.OwnType(x.WS.Fset, x.Pkg.Types, x.Info, x.init)
}

// contextType returns the type a value at use is converted to by its
// context (a parameter, an assignment target, a result, the other operand
// of a binary operation), or nil if unknown.
func (x *inliner) contextType(use *ast.Ident, parent ast.Node) types.Type {
	switch p := parent.(type) {
	case *ast.CallExpr:
		if tv, ok := x.Info.Types[p.Fun]; ok && tv.IsType() {
			return nil
		}
		sig, ok := under(x.Info.TypeOf(p.Fun)).(*types.Signature)
		if !ok {
			return nil
		}
		for i, a := range p.Args {
			if a != ast.Expr(use) {
				continue
			}
			params := sig.Params()
			if sig.Variadic() && i >= params.Len()-1 {
				if p.Ellipsis.IsValid() {
					return params.At(params.Len() - 1).Type()
				}
				return params.At(params.Len() - 1).Type().(*types.Slice).Elem()
			}
			if i < params.Len() {
				return params.At(i).Type()
			}
		}
	case *ast.AssignStmt:
		if p.Tok == token.DEFINE {
			return nil
		}
		if len(p.Lhs) == len(p.Rhs) {
			for i, r := range p.Rhs {
				if r == ast.Expr(use) {
					return x.Info.TypeOf(p.Lhs[i])
				}
			}
		}
	case *ast.ValueSpec:
		if p.Type != nil {
			return x.Info.TypeOf(p.Type)
		}
	case *ast.ReturnStmt:
		sig := x.enclosingSignature(use)
		if sig != nil && sig.Results().Len() == len(p.Results) {
			for i, r := range p.Results {
				if r == ast.Expr(use) {
					return sig.Results().At(i).Type()
				}
			}
		}
	case *ast.BinaryExpr:
		if p.Op == token.SHL || p.Op == token.SHR {
			return nil
		}
		other := p.X
		if other == ast.Expr(use) {
			other = p.Y
		}
		if t := x.Info.TypeOf(other); t != nil && !x.maybeUntyped(other) {
			return t
		}
	case *ast.IndexExpr:
		// A map index converts its key to the key type. (A slice, array or
		// string index does not: an untyped value there becomes int, which
		// can change the arithmetic, e.g. 1 << n no longer wraps at 8 bits.)
		if p.Index == ast.Expr(use) {
			if m, ok := under(x.Info.TypeOf(p.X)).(*types.Map); ok {
				return m.Key()
			}
		}
	case *ast.SendStmt:
		if p.Value == ast.Expr(use) {
			if ch, ok := under(x.Info.TypeOf(p.Chan)).(*types.Chan); ok {
				return ch.Elem()
			}
		}
	case *ast.KeyValueExpr, *ast.CompositeLit:
		// Element types of composite literals: the literal converts it.
		return x.compositeElemType(use)
	}
	return nil
}

func (x *inliner) compositeElemType(use *ast.Ident) types.Type {
	path := analysis.PathTo(x.File, use)
	for i := 1; i < len(path) && i <= 2; i++ {
		cl, ok := path[i].(*ast.CompositeLit)
		if !ok {
			continue
		}
		isKey := false
		if kv, ok := path[1].(*ast.KeyValueExpr); ok && kv.Key == ast.Expr(use) {
			isKey = true
		}
		switch t := under(x.Info.TypeOf(cl)).(type) {
		case *types.Slice:
			if !isKey {
				return t.Elem()
			}
		case *types.Array:
			if !isKey {
				return t.Elem()
			}
		case *types.Map:
			if isKey {
				return t.Key()
			}
			return t.Elem()
		case *types.Struct:
			if kv, ok := path[1].(*ast.KeyValueExpr); ok && !isKey {
				if id, ok := kv.Key.(*ast.Ident); ok {
					if f, ok := x.Info.Uses[id].(*types.Var); ok {
						return f.Type()
					}
				}
			}
		}
	}
	return nil
}

func (x *inliner) enclosingSignature(n ast.Node) *types.Signature {
	for _, p := range analysis.PathTo(x.File, n) {
		switch f := p.(type) {
		case *ast.FuncLit:
			s, _ := x.Info.TypeOf(f).(*types.Signature)
			return s
		case *ast.FuncDecl:
			if o, ok := x.Info.Defs[f.Name].(*types.Func); ok {
				return o.Signature()
			}
			return nil
		}
	}
	return nil
}

// checkConstantContext refuses inlining a constant into places where Go
// treats a constant differently from a variable: a conversion that must
// fit (int8(x) with x = 200 wraps at run time but is a compile error for a
// constant) or a division by a constant zero.
func (x *inliner) checkConstantContext(use *ast.Ident, parent ast.Node) error {
	tv := x.Info.Types[x.init]
	if tv.Value == nil {
		return nil
	}
	if _, isConst := x.obj.(*types.Const); isConst {
		return nil // already a constant: the code compiled with it
	}
	switch p := parent.(type) {
	case *ast.BinaryExpr:
		if (p.Op == token.QUO || p.Op == token.REM) && p.Y == ast.Expr(use) && isZero(tv.Value) {
			return refactor.Refusef("%q is zero and divides at %s; dividing by the constant 0 is a compile error, unlike dividing by a variable that is 0",
				x.obj.Name(), x.WS.Position(p.Pos()))
		}
	case *ast.CallExpr:
		if ftv, ok := x.Info.Types[p.Fun]; ok && ftv.IsType() {
			text := x.Text(p.Fun) + "(" + x.Text(x.init) + ")"
			e, err := parser.ParseExpr(text)
			if err != nil {
				return nil
			}
			info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
			if err := types.CheckExpr(token.NewFileSet(), x.Pkg.Types, p.Pos(), e, info); err != nil {
				return refactor.Refusef("inlining %q into the conversion at %s would make it a constant conversion, which Go rejects: %v",
					x.obj.Name(), x.WS.Position(p.Pos()), err)
			}
		}
	}
	return nil
}

func isZero(v constant.Value) bool {
	switch v.Kind() {
	case constant.Int, constant.Float, constant.Complex:
		return constant.Sign(v) == 0
	}
	return false
}

// ---------------------------------------------------------------------------
// Removing the declaration

func (x *inliner) deletion() edit.TextEdit {
	if x.owner != nil {
		// "if x := f(); cond" -> "if cond": remove through the semicolon.
		start := x.Off(x.declStmt.Pos())
		end := x.Off(x.declStmt.End())
		for end < len(x.Src) && x.Src[end] != ';' {
			end++
		}
		end++
		for end < len(x.Src) && (x.Src[end] == ' ' || x.Src[end] == '\t') {
			end++
		}
		return edit.TextEdit{Start: start, End: end}
	}
	var n ast.Node = x.declStmt
	if x.spec != nil {
		n = x.spec
	}
	start, end := x.Off(n.Pos()), x.Off(n.End())
	lineStart := start
	for lineStart > 0 && (x.Src[lineStart-1] == ' ' || x.Src[lineStart-1] == '\t') {
		lineStart--
	}
	atLineStart := lineStart == 0 || x.Src[lineStart-1] == '\n'
	// Take a trailing comment on the same line with it.
	rest := end
	for rest < len(x.Src) && (x.Src[rest] == ' ' || x.Src[rest] == '\t') {
		rest++
	}
	if strings.HasPrefix(string(x.Src[rest:]), "//") {
		for rest < len(x.Src) && x.Src[rest] != '\n' {
			rest++
		}
	}
	switch {
	case rest < len(x.Src) && x.Src[rest] == ';':
		end = rest + 1
	case atLineStart && (rest == len(x.Src) || x.Src[rest] == '\n'):
		start, end = lineStart, min(rest+1, len(x.Src))
	}
	return edit.TextEdit{Start: start, End: end}
}

// commentAbove returns the position of a comment ending on the line just
// above the declaration, or "".
func (x *inliner) commentAbove() string {
	var n ast.Node = x.declStmt
	if x.spec != nil {
		n = x.spec
	}
	if x.owner != nil {
		return ""
	}
	line := x.TF.Line(n.Pos())
	for _, cg := range x.File.Comments {
		if x.TF.Line(cg.End()) != line-1 {
			continue
		}
		start := x.Off(x.TF.LineStart(x.TF.Line(cg.Pos())))
		if strings.TrimSpace(string(x.Src[start:x.Off(cg.Pos())])) == "" { // a comment on its own line(s)
			return x.WS.Position(cg.Pos())
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Helpers

// maybeUntyped reports whether e may have been untyped before its context
// converted it. go/types records the converted type for untyped constants
// (the 2.0 in n / 2.0 is recorded as int), so any constant counts.
func (x *inliner) maybeUntyped(e ast.Expr) bool {
	tv := x.Info.Types[e]
	return tv.Value != nil || isUntyped(tv.Type)
}

func isUntyped(t types.Type) bool {
	b, ok := t.(*types.Basic)
	return ok && b.Info()&types.IsUntyped != 0
}

func under(t types.Type) types.Type {
	if t == nil {
		return nil
	}
	return t.Underlying()
}
