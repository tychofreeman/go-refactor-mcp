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
	"os"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// InlineVariable is the "inline_variable" refactoring.
type InlineVariable struct{}

// New returns the inline_variable refactoring.
func New() *InlineVariable { return &InlineVariable{} }

func (*InlineVariable) Name() string { return "inline_variable" }

func (*InlineVariable) Description() string {
	return `Replace every use of a local variable (or local constant) with the expression it is initialized with, and delete its declaration. The inverse of extract_variable.
Use it to remove a variable whose name adds nothing, such as one that is set and then used once immediately, or to undo an unhelpful extraction.
Identify the variable by file, line and name, at its declaration or at any use.
Works for "x := expr", "var x [T] = expr" and "const x [T] = expr", also in an if or switch header. If the variable's type differs from the expression's own type (an explicit type, an untyped constant, an interface), the expression is converted so the type is kept.
Refuses if the variable is ever assigned or its address is taken; if inlining would evaluate the expression a different number of times or in a different order (an initializer with function calls, or that reads data other code can change, needs exactly one use, evaluated exactly once in the same block with no calls in between); if a variable the expression reads is modified before a use; or if a name in the expression would mean something else at a use.`
}

func (*InlineVariable) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":   {"type": "string",  "description": "Path of the file, relative to the module root."},
    "line":   {"type": "integer", "description": "1-based line of the variable's declaration or of any use."},
    "name":   {"type": "string",  "description": "The variable's name."},
    "column": {"type": "integer", "description": "Optional 1-based column, only needed if the name occurs more than once on the line with different meanings."}
  },
  "required": ["file", "line", "name"],
  "additionalProperties": false
}`)
}

type args struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column,omitempty"`
	Name   string `json:"name"`
}

type inliner struct {
	ws   *workspace.Workspace
	pkg  *packages.Package
	info *types.Info
	file *ast.File
	tf   *token.File
	src  []byte
	fa   *analysis.Func

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
	pkg, id, err := locate.Ident(ws, locate.IdentQuery{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name})
	if err != nil {
		return nil, err
	}
	obj := pkg.TypesInfo.ObjectOf(id)
	if obj == nil {
		return nil, refactor.Refusef("%q at %s:%d is not a variable or constant that can be inlined", a.Name, a.File, a.Line)
	}
	path := ws.Fset.Position(id.Pos()).Filename
	_, file := ws.FileSyntax(path)
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	x := &inliner{ws: ws, pkg: pkg, info: pkg.TypesInfo, file: file, tf: ws.Fset.File(file.Pos()), src: src, obj: obj}
	return x.plan()
}

func (x *inliner) plan() (*refactor.Plan, error) {
	if err := x.findDeclaration(); err != nil {
		return nil, err
	}
	if err := x.checkVariable(); err != nil {
		return nil, err
	}
	if err := x.checkEvaluation(); err != nil {
		return nil, err
	}
	if err := x.checkReadsUnchanged(); err != nil {
		return nil, err
	}
	if err := x.checkScopes(); err != nil {
		return nil, err
	}

	var edits []edit.TextEdit
	converted := false
	for _, u := range x.uses {
		text, conv, err := x.replacement(u)
		if err != nil {
			return nil, err
		}
		converted = converted || conv
		edits = append(edits, edit.TextEdit{Start: x.off(u.Pos()), End: x.off(u.End()), NewText: text})
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
			kind, x.obj.Name(), x.quote(x.init), len(x.uses), plural(len(x.uses))),
		Edits: []edit.FileEdit{{Path: x.tf.Name(), Edits: edits}},
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
		return refactor.Refusef("%q is a %s; only local variables and constants can be inlined", name, kindOf(x.obj))
	}
	if x.obj.Pkg() == nil || x.obj.Parent() == x.obj.Pkg().Scope() {
		return refactor.Refusef("%q is declared at package level; only variables and constants declared inside a function can be inlined", name)
	}
	if x.obj.Parent() == nil {
		return refactor.Refusef("%q has no declaring scope (it may be a type switch variable) and cannot be inlined", name)
	}
	var declID *ast.Ident
	ast.Inspect(x.file, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Pos() == x.obj.Pos() {
			declID = id
		}
		return declID == nil
	})
	if declID == nil {
		return refactor.Refusef("the declaration of %q is not in this file", name)
	}
	path := analysis.PathTo(x.file, declID)
	if _, ok := path[1].(*ast.Field); ok {
		return refactor.Refusef("%q is a parameter or result name; only local variables with an initializer can be inlined", name)
	}
	fn := analysis.OutermostFunc(path)
	if fn == nil {
		return refactor.Refusef("%q is not declared inside a function", name)
	}
	x.fa = analysis.AnalyzeFunc(x.info, fn)

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

	tv := x.info.Types[x.init]
	if _, ok := tv.Type.(*types.Tuple); ok {
		return refactor.Refusef("the initializer of %q returns several values", name)
	}

	for id, o := range x.info.Uses {
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
			v.Name(), x.ws.Position(mods[0].Pos()))
	}
	if x.fa.Addressed[v] {
		return refactor.Refusef("%q must stay a variable: its address is taken, it is sliced, or a pointer-receiver method is called on it", v.Name())
	}
	return nil
}

// ---------------------------------------------------------------------------
// Evaluation count and order

// checkEvaluation ensures the initializer is evaluated as often and in the
// same order relative to other effects after inlining.
func (x *inliner) checkEvaluation() error {
	culprit, ok := x.fa.Stable(x.init, true)
	if ok {
		return nil // same value and no effects however often and wherever it is evaluated
	}
	if len(x.uses) > 1 {
		return refactor.Refusef("%q is used %d times, and its initializer %s could give a different value or do something each time it is evaluated (because of %s); inlining requires a single use",
			x.obj.Name(), len(x.uses), x.quote(x.init), x.quote(culprit))
	}
	use := x.uses[0]
	stmt, err := x.evaluatedOnce(use)
	if err != nil {
		return err
	}
	if _, ok := x.fa.Stable(x.init, false); ok {
		return nil // no effects and nothing else can change it: only the count mattered
	}
	if x.owner == nil {
		if bad := x.firstEffectBetween(stmt); bad != nil {
			return refactor.Refusef("%s (at %s) runs between the declaration of %q and its use; inlining would move %s after it, which could change behavior",
				x.quote(bad), x.ws.Position(bad.Pos()), x.obj.Name(), x.quote(x.init))
		}
	}
	if first := analysis.FirstEffectBefore(x.info, stmt, use.Pos(), x.declStmt); first != nil {
		return refactor.Refusef("inlining would evaluate %s after %s (at %s), which comes first in the statement that uses %q; that could change behavior",
			x.quote(x.init), x.quote(first), x.ws.Position(first.Pos()), x.obj.Name())
	}
	return nil
}

// evaluatedOnce checks that use is evaluated exactly once each time the
// declaration is, at the top level of a statement in the same block (or in
// the if/switch header that declares the variable), and returns that
// statement.
func (x *inliner) evaluatedOnce(use *ast.Ident) (ast.Stmt, error) {
	path := analysis.PathTo(x.file, use)
	for i := 1; i < len(path); i++ {
		n, child := path[i], path[i-1]
		if _, isBody := n.(*ast.BlockStmt); isBody && i+1 < len(path) {
			if _, ok := path[i+1].(*ast.FuncLit); ok {
				n, child = path[i+1], n // report the function literal, not its body
			}
		}
		if why := conditionalOrRepeated(n, child); why != "" {
			return nil, refactor.Refusef("the use of %q at %s is %s, so the initializer %s would no longer run exactly once, right after the declaration",
				x.obj.Name(), x.ws.Position(use.Pos()), why, x.quote(x.init))
		}
		if x.owner != nil && n == x.owner {
			switch o := x.owner.(type) {
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
			return nil, refactor.Refusef("the use of %q at %s is not in the condition of the statement that declares it, so the initializer %s would no longer run exactly once",
				x.obj.Name(), x.ws.Position(use.Pos()), x.quote(x.init))
		}
		if x.owner == nil && i+1 < len(path) && path[i+1] == x.block {
			if s, ok := n.(ast.Stmt); ok {
				return s, nil
			}
		}
	}
	return nil, refactor.Refusef("the use of %q at %s is not directly in the statement after its declaration, so the initializer %s would no longer run exactly once there",
		x.obj.Name(), x.ws.Position(use.Pos()), x.quote(x.init))
}

// conditionalOrRepeated says why code at child is not evaluated exactly
// once each time n is, or "".
func conditionalOrRepeated(n, child ast.Node) string {
	switch n := n.(type) {
	case *ast.BinaryExpr:
		if (n.Op == token.LAND || n.Op == token.LOR) && child == n.Y {
			return fmt.Sprintf("the right operand of %s, which is only evaluated sometimes", n.Op)
		}
	case *ast.FuncLit:
		return "inside a function literal, which may run later, several times, or not at all"
	case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause:
		return "inside a nested block (an if, loop or case body), which may run zero or several times"
	case *ast.ForStmt:
		if child == n.Cond {
			return "in a loop condition, which is evaluated before every iteration"
		}
		if child == n.Post {
			return "in a loop's post statement, which runs after every iteration"
		}
	case *ast.IfStmt:
		if child == n.Else {
			return "in an else-if, which is only evaluated when the preceding conditions are false"
		}
	}
	return ""
}

// firstEffectBetween returns the first statement part between the
// declaration and stmt (in the same block) that has side effects, writes
// anything but an unshared local, or can transfer control.
func (x *inliner) firstEffectBetween(stmt ast.Stmt) ast.Node {
	var list []ast.Stmt
	switch b := x.block.(type) {
	case *ast.BlockStmt:
		list = b.List
	case *ast.CaseClause:
		list = b.Body
	case *ast.CommClause:
		list = b.Body
	}
	var bad ast.Node
	for _, s := range list {
		if s.Pos() <= x.declStmt.Pos() || s.Pos() >= stmt.Pos() {
			continue
		}
		ast.Inspect(s, func(n ast.Node) bool {
			if bad != nil || n == nil {
				return false
			}
			switch n := n.(type) {
			case *ast.FuncLit:
				return false
			case *ast.SendStmt, *ast.GoStmt, *ast.DeferStmt, *ast.ReturnStmt, *ast.BranchStmt, *ast.LabeledStmt, *ast.SelectStmt:
				bad = n
			case *ast.AssignStmt:
				for _, l := range n.Lhs {
					if id, ok := l.(*ast.Ident); ok && n.Tok == token.DEFINE && x.info.Defs[id] != nil {
						continue
					}
					if v := x.fa.RootVar(l); v == nil || x.fa.Escapes(v) {
						bad = n
					}
				}
			case *ast.IncDecStmt:
				if v := x.fa.RootVar(n.X); v == nil || x.fa.Escapes(v) {
					bad = n
				}
			default:
				if analysis.HasEffect(x.info, n) {
					bad = n
				}
			}
			return bad == nil
		})
		if bad != nil {
			return bad
		}
	}
	return nil
}

// checkReadsUnchanged refuses if a local the initializer reads is modified
// after the declaration but before a use, or anywhere in a loop or closure
// that contains a use but not the declaration (where the use is evaluated
// again later).
func (x *inliner) checkReadsUnchanged() error {
	last := x.uses[len(x.uses)-1].Pos()
	for _, v := range x.fa.LocalsRead(x.init) {
		for _, m := range x.fa.Modified[v] {
			if m.Pos() <= x.declStmt.End() {
				continue
			}
			if x.writtenAt(m) < last {
				return refactor.Refusef("%q, which the initializer of %q reads, is modified at %s before a use; the inlined expression would see the new value",
					v.Name(), x.obj.Name(), x.ws.Position(m.Pos()))
			}
			for _, u := range x.uses {
				if loop := x.repeatingAncestor(u); loop != nil && loop.Pos() <= m.Pos() && m.Pos() < loop.End() {
					return refactor.Refusef("%q, which the initializer of %q reads, is modified at %s inside the loop or function literal that uses it at %s; the inlined expression could see the new value",
						v.Name(), x.obj.Name(), x.ws.Position(m.Pos()), x.ws.Position(u.Pos()))
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
	path := analysis.PathTo(x.file, m)
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
	for _, n := range analysis.PathTo(x.file, u) {
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
	pkgScope := x.pkg.Types.Scope()
	for _, u := range x.uses {
		inner := pkgScope.Innermost(u.Pos())
		if inner == nil {
			continue
		}
		for _, id := range analysis.ScopeRefs(x.info, x.init) {
			want := x.info.Uses[id]
			if want == nil || (want.Pos() >= x.init.Pos() && want.Pos() < x.init.End()) {
				continue // declared inside the initializer (function literal)
			}
			if _, got := inner.LookupParent(id.Name, u.Pos()); got != want {
				where := "is not visible there"
				if got != nil {
					where = fmt.Sprintf("refers to the %s declared at %s instead", kindOf(got), declaredAt(x.ws, got))
				}
				return refactor.Refusef("at the use of %q at %s, %q (used by its initializer %s) %s",
					x.obj.Name(), x.ws.Position(u.Pos()), id.Name, x.quote(x.init), where)
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// The replacement text

// replacement returns the text to put in place of use: the initializer,
// converted to the variable's type if needed and parenthesized if the
// context requires it.
func (x *inliner) replacement(use *ast.Ident) (string, bool, error) {
	path := analysis.PathTo(x.file, use)
	parent := path[1]
	text := string(x.text(x.init))
	if err := x.checkConstantContext(use, parent); err != nil {
		return "", false, err
	}
	if x.needsConversion(use, parent) {
		t := x.typeName()
		if strings.HasPrefix(t, "*") || strings.HasPrefix(t, "<-") || strings.HasPrefix(t, "func") || strings.HasPrefix(t, "chan") {
			t = "(" + t + ")"
		}
		if _, missing := analysis.TypeString(x.obj.Type(), x.file, x.pkg.Types, analysis.ImportMap(x.pkg.Types)); missing != "" {
			return "", false, refactor.Refusef("the inlined expression needs a conversion to %s, but this file does not import %q", x.typeName(), missing)
		}
		return t + "(" + strings.TrimSpace(string(x.text(ast.Unparen(x.init)))) + ")", true, nil
	}
	if needsParens(ast.Unparen(x.init), parent, use) || x.inControlHeader(path) {
		return "(" + text + ")", false, nil
	}
	return text, false, nil
}

func (x *inliner) typeName() string {
	s, _ := analysis.TypeString(x.obj.Type(), x.file, x.pkg.Types, analysis.ImportMap(x.pkg.Types))
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
	tv := x.info.Types[x.init]
	b, basic := tv.Type.Underlying().(*types.Basic)
	if !basic || b.Info()&types.IsUntyped != 0 {
		return tv.Type // only basic types can come from untyped expressions
	}
	hasFuncLit := false
	ast.Inspect(x.init, func(n ast.Node) bool {
		_, ok := n.(*ast.FuncLit)
		hasFuncLit = hasFuncLit || ok
		return !hasFuncLit
	})
	if hasFuncLit {
		return tv.Type
	}
	e := ast.Unparen(x.init)
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
	if err := types.CheckExpr(x.ws.Fset, x.pkg.Types, e.Pos(), e, info); err != nil {
		return tv.Type
	}
	return info.Types[e].Type
}

// contextType returns the type a value at use is converted to by its
// context (a parameter, an assignment target, a result, the other operand
// of a binary operation), or nil if unknown.
func (x *inliner) contextType(use *ast.Ident, parent ast.Node) types.Type {
	switch p := parent.(type) {
	case *ast.CallExpr:
		if tv, ok := x.info.Types[p.Fun]; ok && tv.IsType() {
			return nil
		}
		sig, ok := under(x.info.TypeOf(p.Fun)).(*types.Signature)
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
					return x.info.TypeOf(p.Lhs[i])
				}
			}
		}
	case *ast.ValueSpec:
		if p.Type != nil {
			return x.info.TypeOf(p.Type)
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
		if t := x.info.TypeOf(other); t != nil && !x.maybeUntyped(other) {
			return t
		}
	case *ast.SendStmt:
		if p.Value == ast.Expr(use) {
			if ch, ok := under(x.info.TypeOf(p.Chan)).(*types.Chan); ok {
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
	path := analysis.PathTo(x.file, use)
	for i := 1; i < len(path) && i <= 2; i++ {
		cl, ok := path[i].(*ast.CompositeLit)
		if !ok {
			continue
		}
		isKey := false
		if kv, ok := path[1].(*ast.KeyValueExpr); ok && kv.Key == ast.Expr(use) {
			isKey = true
		}
		switch t := under(x.info.TypeOf(cl)).(type) {
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
					if f, ok := x.info.Uses[id].(*types.Var); ok {
						return f.Type()
					}
				}
			}
		}
	}
	return nil
}

func (x *inliner) enclosingSignature(n ast.Node) *types.Signature {
	for _, p := range analysis.PathTo(x.file, n) {
		switch f := p.(type) {
		case *ast.FuncLit:
			s, _ := x.info.TypeOf(f).(*types.Signature)
			return s
		case *ast.FuncDecl:
			if o, ok := x.info.Defs[f.Name].(*types.Func); ok {
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
	tv := x.info.Types[x.init]
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
				x.obj.Name(), x.ws.Position(p.Pos()))
		}
	case *ast.CallExpr:
		if ftv, ok := x.info.Types[p.Fun]; ok && ftv.IsType() {
			text := string(x.text(p.Fun)) + "(" + string(x.text(x.init)) + ")"
			e, err := parser.ParseExpr(text)
			if err != nil {
				return nil
			}
			info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
			if err := types.CheckExpr(token.NewFileSet(), x.pkg.Types, p.Pos(), e, info); err != nil {
				return refactor.Refusef("inlining %q into the conversion at %s would make it a constant conversion, which Go rejects: %v",
					x.obj.Name(), x.ws.Position(p.Pos()), err)
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

// needsParens reports whether init, written in place of use, must be
// parenthesized to keep its grouping.
func needsParens(init ast.Expr, parent ast.Node, use ast.Expr) bool {
	switch init.(type) {
	case *ast.Ident, *ast.BasicLit, *ast.CompositeLit, *ast.CallExpr, *ast.SelectorExpr, *ast.IndexExpr,
		*ast.IndexListExpr, *ast.SliceExpr, *ast.TypeAssertExpr, *ast.ParenExpr, *ast.FuncLit:
		return false // primary expressions bind tightest
	}
	switch p := parent.(type) {
	case *ast.BinaryExpr:
		b, ok := init.(*ast.BinaryExpr)
		if !ok {
			return false // unary operators bind tighter than any binary one
		}
		pp, ip := p.Op.Precedence(), b.Op.Precedence()
		return ip < pp || (ip == pp && p.Y == use)
	case *ast.UnaryExpr, *ast.StarExpr:
		return true
	case *ast.SelectorExpr:
		return p.X == use
	case *ast.IndexExpr:
		return p.X == use
	case *ast.IndexListExpr:
		return p.X == use
	case *ast.SliceExpr:
		return p.X == use
	case *ast.TypeAssertExpr:
		return p.X == use
	case *ast.CallExpr:
		return p.Fun == use
	}
	return false
}

// inControlHeader reports whether a composite literal initializer would
// land in an if/for/switch header, where Go requires parentheses around it.
func (x *inliner) inControlHeader(path []ast.Node) bool {
	if _, ok := ast.Unparen(x.init).(*ast.CompositeLit); !ok {
		return false
	}
	for i := 1; i < len(path); i++ {
		switch path[i].(type) {
		case *ast.ParenExpr, *ast.CallExpr, *ast.IndexExpr, *ast.CompositeLit, *ast.FuncLit, *ast.BlockStmt:
			return false // bracketed: no ambiguity
		case *ast.IfStmt, *ast.ForStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.RangeStmt:
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Removing the declaration

func (x *inliner) deletion() edit.TextEdit {
	if x.owner != nil {
		// "if x := f(); cond" -> "if cond": remove through the semicolon.
		start := x.off(x.declStmt.Pos())
		end := x.off(x.declStmt.End())
		for end < len(x.src) && x.src[end] != ';' {
			end++
		}
		end++
		for end < len(x.src) && (x.src[end] == ' ' || x.src[end] == '\t') {
			end++
		}
		return edit.TextEdit{Start: start, End: end}
	}
	var n ast.Node = x.declStmt
	if x.spec != nil {
		n = x.spec
	}
	start, end := x.off(n.Pos()), x.off(n.End())
	lineStart := start
	for lineStart > 0 && (x.src[lineStart-1] == ' ' || x.src[lineStart-1] == '\t') {
		lineStart--
	}
	atLineStart := lineStart == 0 || x.src[lineStart-1] == '\n'
	// Take a trailing comment on the same line with it.
	rest := end
	for rest < len(x.src) && (x.src[rest] == ' ' || x.src[rest] == '\t') {
		rest++
	}
	if strings.HasPrefix(string(x.src[rest:]), "//") {
		for rest < len(x.src) && x.src[rest] != '\n' {
			rest++
		}
	}
	switch {
	case rest < len(x.src) && x.src[rest] == ';':
		end = rest + 1
	case atLineStart && (rest == len(x.src) || x.src[rest] == '\n'):
		start, end = lineStart, min(rest+1, len(x.src))
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
	line := x.tf.Line(n.Pos())
	for _, cg := range x.file.Comments {
		if x.tf.Line(cg.End()) != line-1 {
			continue
		}
		start := x.off(x.tf.LineStart(x.tf.Line(cg.Pos())))
		if strings.TrimSpace(string(x.src[start:x.off(cg.Pos())])) == "" { // a comment on its own line(s)
			return x.ws.Position(cg.Pos())
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Helpers

func (x *inliner) off(p token.Pos) int { return x.tf.Offset(p) }

func (x *inliner) text(n ast.Node) []byte { return x.src[x.off(n.Pos()):x.off(n.End())] }

func (x *inliner) quote(n ast.Node) string {
	s := strings.Join(strings.Fields(string(x.text(n))), " ")
	if len(s) > 60 {
		s = s[:57] + "..."
	}
	return "`" + s + "`"
}

// maybeUntyped reports whether e may have been untyped before its context
// converted it. go/types records the converted type for untyped constants
// (the 2.0 in n / 2.0 is recorded as int), so any constant counts.
func (x *inliner) maybeUntyped(e ast.Expr) bool {
	tv := x.info.Types[e]
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
		return "predeclared nil"
	case *types.Label:
		return "label"
	}
	return "symbol"
}

func declaredAt(ws *workspace.Workspace, o types.Object) string {
	if o.Pkg() == nil || !o.Pos().IsValid() {
		return "universe scope (predeclared)"
	}
	return ws.Position(o.Pos())
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
