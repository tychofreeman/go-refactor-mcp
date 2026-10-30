// Package extractvar implements "extract variable": introduce a named
// variable (or constant) for an expression, declared just before the
// statement that contains it.
//
// The transformation moves the expression's evaluation from inside its
// statement to just before it. That is only behavior-preserving when the
// expression would have been evaluated exactly once at that point anyway,
// so most of this file is precondition checking:
//
//   - placement: the expression must be evaluated unconditionally and once
//     per execution of the statement (not in a loop condition, else-if,
//     case expression, select case, or the right operand of && / ||);
//   - evaluation order: it must not jump ahead of calls earlier in the same
//     statement, unless its value cannot be affected by them;
//   - variables: it must not be an assignment target or have its address
//     taken (the new variable would be a copy);
//   - scope: everything it refers to must be visible before the statement,
//     and the new name must not clash with, hide, or be hidden by others.
//
// Constant expressions become `const` declarations, which keeps untyped
// constants untyped, so they are exempt from the placement and order rules.
package extractvar

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/site"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// ExtractVariable is the "extract_variable" refactoring.
type ExtractVariable struct{ refactor.Named[args] }

// New returns the extract_variable refactoring.
func New() *ExtractVariable {
	return &ExtractVariable{Named: refactor.Called[args]("extract_variable")}
}

func (*ExtractVariable) Description() string {
	return `Name an expression: a declaration is inserted just before its statement and the name used in its place. Extracting price*qty as total, return price*qty + tax becomes total := price * qty; return total + tax. A constant expression becomes a const.
Identify the expression by file, the line it starts on and its source text (column if the text is on the line twice). all_occurrences replaces the same expression later in the block too, when it has no calls with side effects and nothing it reads changes in between. pointer declares an alias instead, entries := &s.Log, and writes *entries in its place: for a field that is assigned to or whose address is taken.
Refuses, saying why: an expression evaluated only sometimes or more than once (after && or ||, in a loop condition, an else-if or a case), one that would then run before other calls of its statement, one assigned to, one using variables its statement declares.`
}

type args struct {
	File           string `json:"file" desc:"Path of the file, relative to the module root."`
	Line           int    `json:"line" desc:"1-based line on which the expression starts."`
	Expression     string `json:"expression" desc:"Source text of the expression, e.g. \"len(items) * unitPrice\"."`
	Name           string `json:"name" desc:"Name for the new variable."`
	Column         int    `json:"column,omitempty" desc:"Optional 1-based column where the expression starts, only needed if the text occurs more than once on the line."`
	AllOccurrences bool   `json:"all_occurrences,omitempty" desc:"Also replace identical occurrences later in the same block."`
	Pointer        bool   `json:"pointer,omitempty" desc:"Declare a pointer to the expression (name := &e.f) and use *name in its place, so writes still reach the original."`
}

// extraction holds the state of one planning run.
type extraction struct {
	site.Site
	name    string
	pointer bool // the variable is a pointer to the expression

	fn ast.Node       // outermost enclosing function (FuncDecl or FuncLit)
	fa *analysis.Func // facts about fn's locals
}

// Plan checks preconditions and computes the edits.
func (*ExtractVariable) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	switch {
	case a.File == "":
		return nil, refactor.Refusef("file is required")
	case a.Line < 1:
		return nil, refactor.Refusef("line is required (1-based)")
	case strings.TrimSpace(a.Expression) == "":
		return nil, refactor.Refusef("expression is required: the source text of the expression to extract")
	}
	if err := checkName(a.Name); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	at, err := site.At(ws, path)
	if err != nil {
		return nil, err
	}
	x := &extraction{
		Site:    *at,
		name:    a.Name,
		pointer: a.Pointer,
	}

	want, err := locate.ExprTokens(a.Expression)
	if err != nil {
		return nil, err
	}
	expr, err := locate.FindExpr(ws, x.File, x.Src, want, a.Line, a.Column, a.AllOccurrences)
	if err != nil {
		return nil, err
	}
	return x.plan(expr, a.AllOccurrences)
}

func checkName(name string) error {
	switch {
	case name == "":
		return refactor.Refusef("name is required")
	case name == "_":
		return refactor.Refusef("the blank identifier cannot hold the extracted value; choose a name")
	case !token.IsIdentifier(name):
		return refactor.Refusef("%q is not a valid Go identifier", name)
	}
	return nil
}

func (x *extraction) plan(expr ast.Expr, all bool) (*refactor.Plan, error) {
	path := analysis.PathTo(x.File, expr)
	if path == nil {
		return nil, fmt.Errorf("internal error: expression not found in file syntax")
	}
	tv, err := x.validate(expr, path)
	if err != nil {
		return nil, err
	}
	isConst := tv.Value != nil

	x.fn = analysis.OutermostFunc(path)
	if x.fn == nil {
		return nil, refactor.Refusef("%s is not inside a function body; only expressions in functions can be extracted", x.Quote(expr))
	}
	x.fa = analysis.AnalyzeFunc(x.Info, x.fn)

	stmt, container, err := x.place(path)
	if err != nil {
		return nil, err
	}
	if times, why := analysis.Reach(path[:slices.Index(path, container)+1]); times != analysis.Once && !isConst {
		return nil, refactor.Refusef("cannot extract %s: it is %s, so computing it once before the statement could change behavior", x.Quote(expr), why)
	}
	scope := x.blockScope(container, path)
	if scope == nil {
		return nil, fmt.Errorf("internal error: no scope for the block at %s", x.WS.Position(container.Pos()))
	}
	insertPos := stmt.Pos()

	if err := x.checkReferences(expr, stmt, scope, insertPos); err != nil {
		return nil, err
	}
	if !isConst {
		if err := x.checkMove(expr, stmt); err != nil {
			return nil, err
		}
		if err := x.checkGoto(insertPos); err != nil {
			return nil, err
		}
	}
	if err := x.checkNewName(container, scope, insertPos); err != nil {
		return nil, err
	}

	decl, err := x.declaration(expr, tv, isConst)
	if err != nil {
		return nil, err
	}

	sites := []ast.Expr{expr}
	var notes []string
	if all {
		sites, notes, err = x.occurrences(expr, tv, container, insertPos)
		if err != nil {
			return nil, err
		}
	} else if others := x.laterCopies(expr, tv, container, insertPos); len(others) > 0 {
		notes = append(notes, fmt.Sprintf("the same expression also appears at %s; pass all_occurrences to replace those too",
			strings.Join(others, ", ")))
	}
	for _, s := range sites {
		if err := x.checkShadowAt(s, scope); err != nil {
			return nil, err
		}
	}

	// Insert the declaration at the start of the statement, followed by a
	// newline and the statement's indentation; gofmt tidies the rest.
	path0 := x.TF.Name()
	edits := []edit.TextEdit{{
		Start:   x.Off(insertPos),
		End:     x.Off(insertPos),
		NewText: decl + "\n" + x.indentOf(insertPos),
	}}
	for _, s := range sites {
		n, text := x.replacement(s)
		edits = append(edits, edit.TextEdit{Start: x.Off(n.Pos()), End: x.Off(n.End()), NewText: text})
	}

	kind := "variable"
	switch {
	case isConst:
		kind = "constant"
	case x.pointer:
		kind = "pointer"
	}
	return &refactor.Plan{
		Summary: fmt.Sprintf("extracted %s into %s %q (%d occurrence%s replaced)",
			x.Quote(expr), kind, x.name, len(sites), textutil.Plural(len(sites))),
		Edits: []edit.FileEdit{{Path: path0, Edits: edits}},
		Notes: notes,
	}, nil
}

// replacement returns the node to replace at an occurrence and what to
// write in its place.
func (x *extraction) replacement(site ast.Expr) (ast.Node, string) {
	w := x.widenParens(site) // "(a + b)" becomes "name", not "(name)"
	if !x.pointer {
		return w, x.name
	}
	if path := analysis.PathTo(x.File, w); len(path) > 1 {
		if u, ok := path[1].(*ast.UnaryExpr); ok && u.Op == token.AND {
			return u, x.name // &e.f becomes name
		}
		if analysis.NeedsParens(&ast.StarExpr{X: ast.NewIdent(x.name)}, path[1], w) {
			return w, "(*" + x.name + ")"
		}
	}
	return w, "*" + x.name
}

// move is the move of what the new variable holds (the expression, or
// with pointer its address) to where the variable is declared and, with
// times, to each occurrence it replaces.
func (x *extraction) move(expr ast.Expr, past []ast.Node, times analysis.Times) *analysis.Obstacle {
	moved := expr
	if x.pointer {
		moved = &ast.UnaryExpr{Op: token.AND, X: expr}
	}
	m := analysis.Move{Effects: x.WS.Effects(), Func: x.fa, Expr: moved, Past: past, Times: times, Dereferenced: x.pointer}
	return m.Check()
}

// ---------------------------------------------------------------------------
// Finding the expression

func (x *extraction) matches(e ast.Expr, want []string) bool {
	return locate.TokensMatch(x.TF, x.Src, e, want)
}

// ---------------------------------------------------------------------------
// What can be extracted

// validate checks that expr is a value that can be held in a variable, and
// that it is not used as a variable itself.
func (x *extraction) validate(expr ast.Expr, path []ast.Node) (types.TypeAndValue, error) {
	q := x.Quote(expr)
	tv, ok := x.Info.Types[expr]
	if !ok {
		if id, ok := expr.(*ast.Ident); ok {
			if _, isPkg := x.Info.Uses[id].(*types.PkgName); isPkg {
				return tv, refactor.Refusef("%s is a package name, not a value", q)
			}
		}
		return tv, refactor.Refusef("%s is not a value expression here (it may be a field name, label, or a name being declared)", q)
	}
	switch {
	case tv.IsType():
		return tv, refactor.Refusef("%s is a type, not a value", q)
	case tv.IsBuiltin():
		return tv, refactor.Refusef("%s is a builtin function, which can only be called, not stored in a variable", q)
	case tv.IsVoid():
		return tv, refactor.Refusef("%s does not return a value", q)
	case tv.IsNil():
		return tv, refactor.Refusef("%s is the untyped nil, which cannot initialize a variable without a type", q)
	}
	if _, ok := tv.Type.(*types.Tuple); ok {
		return tv, refactor.Refusef("%s returns multiple values; extract it with an explicit assignment instead", q)
	}
	if id := analysis.IdentOf(expr); id != nil {
		if _, ok := x.Info.Instances[id]; ok {
			if _, isFunc := tv.Type.(*types.Signature); isFunc {
				return tv, refactor.Refusef("%s is a generic function whose type arguments are inferred from the call; it cannot be stored without explicit type arguments", q)
			}
		}
	}
	if cl, ok := ast.Unparen(expr).(*ast.CompositeLit); ok && cl.Type == nil {
		return tv, refactor.Refusef("%s is a composite literal whose type is implied by the enclosing literal; it has no type of its own here", q)
	}
	if ta, ok := ast.Unparen(expr).(*ast.TypeAssertExpr); ok && ta.Type == nil {
		return tv, refactor.Refusef("%s is a type switch guard, not a value", q)
	}
	iota := types.Universe.Lookup("iota")
	usesIota := false
	ast.Inspect(expr, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && x.Info.Uses[id] == iota {
			usesIota = true
		}
		return !usesIota
	})
	if usesIota {
		return tv, refactor.Refusef("%s uses iota, whose value depends on its position in the const declaration", q)
	}
	if x.pointer {
		if !tv.Addressable() {
			return tv, refactor.Refusef("%s is not addressable (a map element, a call's result or a computed value), so there is no pointer to it; extract it without pointer", q)
		}
	} else if why := analysis.UsedAsVariable(x.Info, path); why != "" {
		hint := ""
		if tv.Addressable() {
			hint = "; pass pointer to declare a pointer to it instead"
		}
		return tv, refactor.Refusef("%s %s; a new variable would be a copy, so the change would no longer reach the original%s", q, why, hint)
	}
	if len(path) > 1 {
		switch p := path[1].(type) {
		case *ast.ExprStmt:
			return tv, refactor.Refusef("%s is a whole statement whose value is discarded; there is nothing to name", q)
		case *ast.DeferStmt:
			if p.Call == expr {
				return tv, refactor.Refusef("%s is the deferred call itself; extracting it would run it immediately", q)
			}
		case *ast.GoStmt:
			if p.Call == expr {
				return tv, refactor.Refusef("%s is the call started by go; extracting it would run it synchronously", q)
			}
		}
	}
	return tv, nil
}

// ---------------------------------------------------------------------------
// Where the declaration goes

// place finds the statement to insert the declaration before and the
// block (or case clause) that contains it.
func (x *extraction) place(path []ast.Node) (stmt ast.Stmt, container ast.Node, err error) {
	expr := path[0]
	i := 1
scan:
	for ; i < len(path); i++ {
		switch p := path[i].(type) {
		case *ast.CaseClause:
			// path[i+1] is the switch body; path[i+2] the switch statement.
			if i+2 >= len(path) {
				break scan
			}
			i += 2
			stmt, _ = path[i].(ast.Stmt)
			break scan
		case *ast.FuncLit, *ast.FuncDecl, *ast.File:
			return nil, nil, refactor.Refusef("%s is not inside a statement (it may be part of a function signature)", x.Quote(expr))
		case ast.Stmt:
			stmt = p
			break scan
		}
	}
	if stmt == nil {
		return nil, nil, refactor.Refusef("could not find the statement containing %s", x.Quote(expr))
	}

	for i+1 < len(path) {
		switch p := path[i+1].(type) {
		case *ast.BlockStmt:
			return stmt, p, nil
		case *ast.CaseClause:
			return stmt, p, nil
		case *ast.CommClause:
			if p.Comm != stmt {
				return stmt, p, nil
			}
			// path[i+1] is the CommClause, i+2 the select body, i+3 the select.
			if i+3 >= len(path) {
				return nil, nil, refactor.Refusef("cannot find the select statement containing %s", x.Quote(expr))
			}
			i += 3
			stmt = path[i].(ast.Stmt)
			continue
		case *ast.LabeledStmt:
			stmt = p
		case *ast.IfStmt:
			stmt = p
		case *ast.SwitchStmt:
			stmt = p // from Init: evaluated once, first
		case *ast.TypeSwitchStmt:
			stmt = p // from Init or the guard: evaluated once
		case *ast.ForStmt:
			stmt = p
		default:
			return nil, nil, refactor.Refusef("cannot find a place to declare the variable before the statement at %s", x.WS.Position(stmt.Pos()))
		}
		i++
	}
	return nil, nil, refactor.Refusef("cannot find a place to declare the variable before the statement at %s", x.WS.Position(stmt.Pos()))
}

// blockScope returns the scope that a declaration inserted into container
// joins. A function body shares its scope with the parameters, which
// go/types records on the FuncType.
func (x *extraction) blockScope(container ast.Node, path []ast.Node) *types.Scope {
	if s := x.Info.Scopes[container]; s != nil {
		return s
	}
	for _, n := range path {
		switch f := n.(type) {
		case *ast.FuncDecl:
			if f.Body == container {
				return x.Info.Scopes[f.Type]
			}
		case *ast.FuncLit:
			if f.Body == container {
				return x.Info.Scopes[f.Type]
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Behavior and scope checks

// checkReferences ensures every name the expression uses denotes the same
// thing at the insertion point, e.g. not a variable declared in an if
// statement's init.
func (x *extraction) checkReferences(expr ast.Expr, stmt ast.Stmt, scope *types.Scope, insertPos token.Pos) error {
	for _, id := range analysis.ScopeRefs(x.Info, expr) {
		obj := x.Info.Uses[id]
		if obj == nil || (obj.Pos() >= expr.Pos() && obj.Pos() < expr.End()) {
			continue // declared inside the expression (function literal)
		}
		if _, found := scope.LookupParent(id.Name, insertPos); found == obj {
			continue
		}
		if obj.Pos() >= stmt.Pos() && obj.Pos() < stmt.End() {
			return refactor.Refusef("%s uses %q, which is declared by the statement itself (at %s) and so does not exist before it",
				x.Quote(expr), id.Name, x.WS.Position(obj.Pos()))
		}
		return refactor.Refusef("%s uses %q (declared at %s), which is not visible where the new variable would be declared",
			x.Quote(expr), id.Name, x.WS.Position(obj.Pos()))
	}
	return nil
}

// checkMove refuses if hoisting the expression before the statement
// would change behavior: it would then be evaluated before the code of
// the statement that comes first (the init of an if, the other operands),
// which could change what it reads, or panic or act in another order.
func (x *extraction) checkMove(expr ast.Expr, stmt ast.Stmt) error {
	if o := x.move(expr, analysis.EvaluatedBefore(stmt, expr.Pos(), nil), analysis.Once); o != nil {
		return refactor.Refusef("extracting %s would evaluate it before %s (at %s), which comes first in the statement: %s; extract %s first, or choose another expression",
			x.Quote(expr), x.Quote(o.Past), x.WS.Position(o.Past.Pos()), o.Why, x.Quote(o.Past))
	}
	return nil
}

// checkGoto refuses when a goto jumps forward over the insertion point:
// Go forbids jumping over a variable declaration.
func (x *extraction) checkGoto(insertPos token.Pos) error {
	labels := map[string]token.Pos{}
	var gotos []*ast.BranchStmt
	ast.Inspect(x.fn, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.LabeledStmt:
			labels[n.Label.Name] = n.Pos()
		case *ast.BranchStmt:
			if n.Tok == token.GOTO && n.Label != nil {
				gotos = append(gotos, n)
			}
		}
		return true
	})
	for _, g := range gotos {
		if lp, ok := labels[g.Label.Name]; ok && g.Pos() < insertPos && lp > insertPos {
			return refactor.Refusef("the goto at %s jumps over the place where the variable would be declared, which Go does not allow", x.WS.Position(g.Pos()))
		}
	}
	return nil
}

// checkNewName refuses names that clash with or hide existing names.
func (x *extraction) checkNewName(container ast.Node, scope *types.Scope, insertPos token.Pos) error {
	if o := scope.Lookup(x.name); o != nil {
		return refactor.Refusef("%q is already declared in this block, at %s", x.name, site.DeclaredAt(x.WS, o))
	}
	// Later uses of an outer x.name in this block would bind to the new one.
	var hidden *ast.Ident
	ast.Inspect(container, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || hidden != nil || id.Name != x.name || id.Pos() < insertPos {
			return hidden == nil
		}
		if o := x.Info.Uses[id]; o != nil && o.Parent() != nil && !within(o.Parent(), scope) {
			hidden = id
		}
		return true
	})
	if hidden != nil {
		o := x.Info.Uses[hidden]
		return refactor.Refusef("a new %q would hide the %s %q declared at %s, which is used at %s; choose another name",
			x.name, kindOf(o), x.name, site.DeclaredAt(x.WS, o), x.WS.Position(hidden.Pos()))
	}
	return nil
}

// checkShadowAt refuses if, at a replacement site, the new name would refer
// to some other declaration nested between the block and the site.
func (x *extraction) checkShadowAt(at ast.Expr, scope *types.Scope) error {
	inner := x.Pkg.Types.Scope().Innermost(at.Pos())
	if inner == nil {
		return nil
	}
	if s, o := inner.LookupParent(x.name, at.Pos()); o != nil && s != scope && within(s, scope) {
		return refactor.Refusef("at %s, %q already refers to the %s declared at %s, so it cannot refer to the new variable there; choose another name",
			x.WS.Position(at.Pos()), x.name, kindOf(o), site.DeclaredAt(x.WS, o))
	}
	return nil
}

// within reports whether s is scope or nested inside it.
func within(s, scope *types.Scope) bool {
	for ; s != nil; s = s.Parent() {
		if s == scope {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// The declaration

func (x *extraction) declaration(expr ast.Expr, tv types.TypeAndValue, isConst bool) (string, error) {
	rhs := (x.Text(ast.Unparen(expr)))
	if isConst {
		return "const " + x.name + " = " + rhs, nil
	}
	if x.pointer {
		return x.name + " := &" + rhs, nil
	}
	// "v := e" gives v the type of e on its own. For untyped non-constant
	// expressions (comparisons, shifts of untyped constants) that is the
	// default type, which may differ from the type the context gave it.
	if x.needsExplicitType(expr, tv) {
		t, err := x.typeString(tv.Type)
		if err != nil {
			return "", err
		}
		return "var " + x.name + " " + t + " = " + rhs, nil
	}
	return x.name + " := " + rhs, nil
}

func (x *extraction) needsExplicitType(expr ast.Expr, tv types.TypeAndValue) bool {
	b, basic := tv.Type.Underlying().(*types.Basic)
	if !basic || b.Info()&types.IsUntyped != 0 {
		// Still untyped in context (e.g. an if condition): nothing imposed
		// a type, so the default type that := gives is accepted there.
		return false
	}
	hasFuncLit := false
	ast.Inspect(expr, func(n ast.Node) bool {
		_, ok := n.(*ast.FuncLit)
		hasFuncLit = hasFuncLit || ok
		return !hasFuncLit
	})
	if hasFuncLit {
		return false // CheckExpr would add scopes for it to the package's scope tree
	}
	e := ast.Unparen(expr)
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
	if err := types.CheckExpr(x.WS.Fset, x.Pkg.Types, e.Pos(), e, info); err != nil {
		return false // the post-change type check still guards correctness
	}
	alone := info.Types[e].Type
	b, ok := alone.(*types.Basic)
	return ok && b.Info()&types.IsUntyped != 0 && !types.Identical(types.Default(alone), tv.Type)
}

// typeString spells t as it must appear in this file.
func (x *extraction) typeString(t types.Type) (string, error) {
	str, missing := analysis.TypeString(t, x.File, x.Pkg.Types, analysis.ImportMap(x.Pkg.Types))
	if missing != "" {
		return "", refactor.Refusef("the new variable needs the explicit type %s, but this file does not import %q", str, missing)
	}
	return str, nil
}

// ---------------------------------------------------------------------------
// Repeated occurrences

// occurrences returns every occurrence of expr from the insertion point to
// the end of its block, after checking that one shared value is
// equivalent to recomputing it at each of them.
func (x *extraction) occurrences(expr ast.Expr, tv types.TypeAndValue, container ast.Node, insertPos token.Pos) ([]ast.Expr, []string, error) {
	for _, id := range x.dependsOn(expr) {
		v, ok := x.Info.Uses[id].(*types.Var)
		if !ok {
			continue
		}
		for _, w := range x.fa.Modified[v] {
			if w.Pos() >= insertPos {
				return nil, nil, refactor.Refusef("all_occurrences: %q is modified at %s, after the new variable would be declared, so later occurrences could have a different value; extract a single occurrence instead",
					id.Name, x.WS.Position(w.Pos()))
			}
		}
	}

	var sites []ast.Expr
	var skippedWrites []string
	ast.Inspect(container, func(n ast.Node) bool {
		e, ok := n.(ast.Expr)
		if !ok || e.Pos() < insertPos || !x.same(e, expr, tv) {
			return true
		}
		if why := analysis.UsedAsVariable(x.Info, analysis.PathTo(x.File, e)); why != "" && !x.pointer {
			skippedWrites = append(skippedWrites, x.WS.Position(e.Pos()))
			return false
		}
		sites = append(sites, e)
		return false
	})
	if o := x.shared(expr, container, insertPos, sites); o != nil {
		if o.Past != nil {
			return nil, nil, refactor.Refusef("all_occurrences requires an expression whose value cannot change between its occurrences, but %s %s between them, and %s %s; extract a single occurrence instead (omit all_occurrences)",
				x.Quote(o.Past), o.PastDoes, x.Quote(expr), o.Does)
		}
		return nil, nil, refactor.Refusef("all_occurrences requires an expression whose value cannot change between its occurrences, but %s could (it involves %s); extract a single occurrence instead (omit all_occurrences)",
			x.Quote(expr), x.Quote(o.In))
	}

	var notes []string
	if len(skippedWrites) > 0 {
		notes = append(notes, fmt.Sprintf("occurrences that are written to were left alone: %s", strings.Join(skippedWrites, ", ")))
	}
	outside := 0
	ast.Inspect(x.fn, func(n ast.Node) bool {
		e, ok := n.(ast.Expr)
		if !ok || !x.same(e, expr, tv) {
			return true
		}
		if e.Pos() < insertPos || e.Pos() >= container.End() {
			outside++
		}
		return false
	})
	if outside > 0 {
		notes = append(notes, fmt.Sprintf("%d identical occurrence%s elsewhere in the function %s not replaced, because %s outside the block where %q is visible",
			outside, textutil.Plural(outside), isAre(outside), itThey(outside), x.name))
	}
	return sites, notes, nil
}

// shared returns why one value of expr, computed at insertPos, would not
// stand for its evaluations at sites, in container: it does something
// each evaluation repeats, or the statements from insertPos through the
// last site change what it reads. With a site in a function literal,
// which may run at another time, it may read nothing that can change.
func (x *extraction) shared(expr ast.Expr, container ast.Node, insertPos token.Pos, sites []ast.Expr) *analysis.Obstacle {
	var between []ast.Node
	if len(sites) > 0 {
		nodes := make([]ast.Node, len(sites))
		for i, s := range sites {
			nodes[i] = s
		}
		between = analysis.Spanning(container, insertPos, sites[len(sites)-1].Pos(), nodes...)
	}
	moved := expr
	if x.pointer {
		moved = &ast.UnaryExpr{Op: token.AND, X: expr}
	}
	m := analysis.Move{Effects: x.WS.Effects(), Func: x.fa, Expr: moved, Past: between, Times: analysis.Shared, Dereferenced: x.pointer, Between: between != nil}
	return m.Check()
}

// dependsOn returns the names whose values the new variable's value
// depends on: those of the expression, or with pointer, those of the
// operands its address depends on.
func (x *extraction) dependsOn(expr ast.Expr) []*ast.Ident {
	if !x.pointer {
		return analysis.ScopeRefs(x.Info, expr)
	}
	ops, _ := x.fa.AddressOperands(expr)
	var ids []*ast.Ident
	for _, op := range ops {
		ids = append(ids, analysis.ScopeRefs(x.Info, op)...)
	}
	return ids
}

// laterCopies lists positions of other identical occurrences after the
// insertion point that all_occurrences could replace, for a helpful note.
func (x *extraction) laterCopies(expr ast.Expr, tv types.TypeAndValue, container ast.Node, insertPos token.Pos) []string {
	var out []string
	var sites []ast.Expr
	ast.Inspect(container, func(n ast.Node) bool {
		e, ok := n.(ast.Expr)
		if !ok || e.Pos() < insertPos || !x.same(e, expr, tv) {
			return true
		}
		sites = append(sites, e)
		if e != expr {
			out = append(out, x.WS.Position(e.Pos()))
		}
		return false
	})
	if len(out) == 0 || x.shared(expr, container, insertPos, sites) != nil {
		return nil
	}
	return out
}

// same reports whether e is the same expression as expr: identical tokens,
// identifiers denoting the same objects, and the same type.
func (x *extraction) same(e, expr ast.Expr, tv types.TypeAndValue) bool {
	if !x.matches(e, mustTokens([]byte(x.Text(expr)))) {
		return false
	}
	etv, ok := x.Info.Types[e]
	if !ok || !types.Identical(etv.Type, tv.Type) {
		return false
	}
	a, b := idents(e), idents(expr)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if x.Info.ObjectOf(a[i]) != x.Info.ObjectOf(b[i]) {
			return false
		}
	}
	return true
}

func mustTokens(src []byte) []string {
	t, _ := locate.Tokens(src)
	return t
}

func idents(e ast.Expr) []*ast.Ident {
	var out []*ast.Ident
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			out = append(out, id)
		}
		return true
	})
	return out
}

// ---------------------------------------------------------------------------
// Helpers

func (x *extraction) indentOf(p token.Pos) string {
	start := x.Off(x.TF.LineStart(x.TF.Line(p)))
	ws := x.Src[start:x.Off(p)]
	for i, c := range ws {
		if c != ' ' && c != '\t' {
			return string(ws[:i])
		}
	}
	return string(ws)
}

// widenParens returns the outermost parenthesized expression whose only
// content is e, or e itself.
func (x *extraction) widenParens(e ast.Expr) ast.Expr {
	path := analysis.PathTo(x.File, e)
	out := e
	for i := 1; i < len(path); i++ {
		pe, ok := path[i].(*ast.ParenExpr)
		if !ok {
			break
		}
		out = pe
	}
	return out
}

func kindOf(o types.Object) string {
	switch o := o.(type) {
	case *types.Var:
		if o.Pkg() != nil && o.Parent() == o.Pkg().Scope() {
			return "package-level variable"
		}
		return "variable"
	case *types.Const:
		return "constant"
	case *types.TypeName:
		return "type"
	case *types.Func:
		return "function"
	case *types.PkgName:
		return "import"
	case *types.Builtin:
		return "builtin"
	case *types.Nil:
		return "predeclared"
	}
	return "symbol"
}

func isAre(n int) string {
	if n == 1 {
		return "was"
	}
	return "were"
}

func itThey(n int) string {
	if n == 1 {
		return "it is"
	}
	return "they are"
}
