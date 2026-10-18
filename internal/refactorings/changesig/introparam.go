package changesig

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// IntroduceParameter is the "introduce_parameter" refactoring.
type IntroduceParameter struct{}

// NewIntroduceParameter returns the introduce_parameter refactoring.
func NewIntroduceParameter() *IntroduceParameter { return &IntroduceParameter{} }

func (*IntroduceParameter) Name() string { return "introduce_parameter" }

func (*IntroduceParameter) Description() string {
	return `Replace an expression in a function's body with a new parameter, and pass that expression at every call, written in terms of the call's arguments.
Use it to make a function depend on a value instead of on where the value comes from: introduce p.x as x, then remove_parameter p once the body no longer uses it.
Identify the expression by file, the line where it starts, and its exact source text (whitespace may differ); add column only if that text occurs more than once on the line. Give the new parameter's name; it goes at the end (or just before a variadic parameter), with the expression's type.
It may use the receiver: m.decl in a method of m becomes mv.decl at the call mv.check().
The expression is then evaluated once, at the call, instead of where the body evaluated it, so it must have the same value there and do the same: it may only read parameters (and the receiver) the body never changes or takes the address of, and names every call can see with the same meaning, and it must have no side effects. If it could panic (a read through a pointer, indexing, division) or reads memory other code can change (through a pointer, a slice or map, a package variable), it must be evaluated at entry: on every call (not inside an if, case or loop), with nothing before it that has side effects, writes anything but a local, or could panic otherwise than by a nil dereference.
Refuses if the expression uses a variable declared in the body, is assigned to or has its address taken, if an argument or receiver of some call has side effects (assign it to a variable first), for a method called through an embedded field, or for the same reasons as add_parameter (function values, interface methods, interfaces a method would stop or start implementing, a name that clashes).`
}

func (*IntroduceParameter) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":       {"type": "string",  "description": "Path of the file, relative to the workspace root."},
    "line":       {"type": "integer", "description": "1-based line on which the expression starts."},
    "expression": {"type": "string",  "description": "Source text of the expression, e.g. \"p.x\"."},
    "parameter":  {"type": "string",  "description": "Name of the new parameter."},
    "column":     {"type": "integer", "description": "Optional 1-based column where the expression starts, only needed if the text occurs more than once on the line."}
  },
  "required": ["file", "line", "expression", "parameter"],
  "additionalProperties": false
}`)
}

type introArgs struct {
	File       string `json:"file"`
	Line       int    `json:"line"`
	Column     int    `json:"column,omitempty"`
	Expression string `json:"expression"`
	Parameter  string `json:"parameter"`
}

// Plan checks preconditions and computes the edits.
func (*IntroduceParameter) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a introArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if !token.IsIdentifier(a.Parameter) {
		return nil, refactor.Refusef("parameter %q is not a valid Go identifier", a.Parameter)
	}
	path := ws.Abs(a.File)
	pkg, file := ws.FileSyntax(path)
	if file == nil {
		return nil, refactor.Refusef("file %s is not part of the module's packages", a.File)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	want, err := locate.ExprTokens(a.Expression)
	if err != nil {
		return nil, err
	}
	expr, err := locate.FindExpr(ws, file, src, want, a.Line, a.Column, false)
	if err != nil {
		return nil, err
	}
	fd, ok := analysis.OutermostFunc(analysis.PathTo(file, expr)).(*ast.FuncDecl)
	if !ok {
		return nil, refactor.Refusef("%s is not in the body of a declared function", a.Expression)
	}
	declPos := ws.Fset.Position(fd.Name.Pos())
	t, err := findTarget(ws, locate.IdentQuery{File: declPos.Filename, Line: declPos.Line, Column: declPos.Column, Name: fd.Name.Name})
	if err != nil {
		return nil, err
	}
	if pkg != t.pkg { // work in the package variant the target was found in
		pkg = t.pkg
		for _, f := range pkg.Syntax {
			if f.Pos() <= t.decl.Pos() && t.decl.Pos() < f.End() {
				file = f
			}
		}
		if expr, err = locate.FindExpr(ws, file, src, want, a.Line, a.Column, false); err != nil {
			return nil, err
		}
	}
	if err := t.checkIntroduced(pkg.TypesInfo, file, expr); err != nil {
		return nil, err
	}
	at, err := t.insertIndex("")
	if err != nil {
		return nil, err
	}
	if err := t.checkNewName(a.Parameter); err != nil {
		return nil, err
	}
	typ := pkg.TypesInfo.TypeOf(expr)
	typSrc, missing := analysis.TypeString(typ, file, pkg.Types, analysis.ImportMap(pkg.Types))
	if missing != "" {
		return nil, refactor.Refusef("the type of %s, %s, cannot be written in %s without importing %s", a.Expression, typ, a.File, missing)
	}
	if err := t.checkInterfaces(func(ps []string) []string { return slices.Insert(ps, at, typeKey(typ)) }); err != nil {
		return nil, err
	}

	o := t.off(expr.Pos())
	fes := []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{
		t.paramEdit(at, a.Parameter, typSrc),
		{Start: o, End: t.off(expr.End()), NewText: a.Parameter},
	}}}
	srcs := map[string][]byte{path: src}
	for _, c := range t.calls {
		cpath := ws.Fset.Position(c.expr.Pos()).Filename
		if srcs[cpath] == nil {
			if srcs[cpath], err = os.ReadFile(cpath); err != nil {
				return nil, err
			}
		}
		if err := t.checkCall(pkg.TypesInfo, expr, c, srcs[cpath]); err != nil {
			return nil, err
		}
		value := t.substitute(pkg.TypesInfo, src, expr, c, srcs[cpath])
		fes = append(fes, edit.FileEdit{Path: cpath, Edits: []edit.TextEdit{t.argEdit(c.expr, at, value)}})
	}
	return t.newPlan(fmt.Sprintf("introduced parameter %q for %s in %s", a.Parameter+" "+typSrc, a.Expression, t.obj.Name()), fes), nil
}

// checkIntroduced checks that evaluating expr once at the call, instead of
// where (and as often as) the body did, changes nothing: it reads only
// parameters (and the receiver) the body never changes, and names the
// calls can see, and its evaluation passes checkEvaluation.
func (t *target) checkIntroduced(info *types.Info, file *ast.File, expr ast.Expr) error {
	q := types.ExprString(expr)
	tv, ok := info.Types[expr]
	switch {
	case !ok || tv.IsType() || tv.IsVoid() || tv.IsBuiltin() || tv.IsNil():
		return refactor.Refusef("%s is not a value that a parameter can hold", q)
	case isTuple(tv.Type):
		return refactor.Refusef("%s returns multiple values, which a parameter cannot hold", q)
	}
	if why := analysis.UsedAsVariable(info, analysis.PathTo(file, expr)); why != "" {
		return refactor.Refusef("%s %s; a parameter would be a copy, so the change would no longer reach the original", q, why)
	}
	if err := t.checkReads(info, expr, q); err != nil {
		return err
	}
	return t.checkEvaluation(info, file, expr, q)
}

func (t *target) checkReads(info *types.Info, expr ast.Expr, q string) error {
	fa := analysis.AnalyzeFunc(info, t.decl)
	isParam := t.paramObjects(info)
	for _, id := range analysis.ScopeRefs(info, expr) {
		v, ok := info.Uses[id].(*types.Var)
		if !ok || !fa.IsLocal(v) {
			continue
		}
		if _, ok := isParam[v]; !ok {
			return refactor.Refusef("%s uses %s, declared in the body of %s, which the calls cannot see", q, v.Name(), t.obj.Name())
		}
		if mods := fa.Modified[v]; len(mods) > 0 {
			return refactor.Refusef("%s changes %s (at %s), so %s could have another value there than at the call", t.obj.Name(), v.Name(), t.ws.Position(mods[0].Pos()), q)
		}
		if fa.Addressed[v] {
			return refactor.Refusef("%s takes the address of %s, so it could change, and %s could have another value there than at the call", t.obj.Name(), v.Name(), q)
		}
	}
	return nil
}

// checkCall checks that the call c can pass the value of expr: no argument
// has side effects (the new argument repeats some of them, and its
// evaluation is not ordered with their calls), and every other name in
// expr means the same thing at the call.
func (t *target) checkCall(info *types.Info, expr ast.Expr, c call, csrc []byte) error {
	where := t.ws.Position(c.expr.Pos())
	if err := checkNoSpread(c, where); err != nil {
		return err
	}
	if sel := c.recvSelection(); sel != nil && len(sel.Index()) > 1 {
		return refactor.Refusef("the call at %s calls %s promoted through an embedded field, which is not supported yet", where, t.obj.Name())
	}
	if err := t.checkArgEffects(c, csrc); err != nil {
		return err
	}
	isParam := t.paramObjects(info)
	scope := c.pkg.Types.Scope().Innermost(c.expr.Pos())
	for _, id := range analysis.ScopeRefs(info, expr) {
		o := info.Uses[id]
		if _, ok := isParam[o]; ok || o == nil {
			continue
		}
		var there types.Object
		if scope != nil {
			_, there = scope.LookupParent(id.Name, c.expr.Pos())
		}
		if !sameObject(t.ws.Fset, o, there) {
			return refactor.Refusef("%s means something else at the call at %s (or is not visible there)", id.Name, where)
		}
	}
	return nil
}

// checkArgEffects refuses a call whose arguments or receiver have side
// effects.
func (t *target) checkArgEffects(c call, csrc []byte) error {
	if x := c.recv(); x != nil && firstRisk(c.pkg.TypesInfo, x, func(r risk) bool { return r.effect }) != nil {
		return refactor.Refusef("the receiver %s at %s has side effects; assign it to a variable first",
			csrc[t.off(x.Pos()):t.off(x.End())], t.ws.Position(x.Pos()))
	}
	for i, arg := range c.expr.Args {
		if firstRisk(c.pkg.TypesInfo, arg, func(r risk) bool { return r.effect }) != nil {
			p := t.params[min(i, len(t.params)-1)]
			return refactor.Refusef("the argument %s for %s at %s has side effects; assign it to a variable first",
				csrc[t.off(arg.Pos()):t.off(arg.End())], p.name, t.ws.Position(arg.Pos()))
		}
	}
	return nil
}

// recv returns the receiver expression of a method call, or nil.
func (c call) recv() ast.Expr {
	if sel := c.recvSelection(); sel != nil {
		return ast.Unparen(c.expr.Fun).(*ast.SelectorExpr).X
	}
	return nil
}

// recvSelection returns the selection of a method call, or nil.
func (c call) recvSelection() *types.Selection {
	if se, ok := ast.Unparen(c.expr.Fun).(*ast.SelectorExpr); ok {
		if sel := c.pkg.TypesInfo.Selections[se]; sel != nil && sel.Kind() == types.MethodVal {
			return sel
		}
	}
	return nil
}

// sameObject reports whether a and b are the same object, possibly seen
// through different package variants.
func sameObject(fset *token.FileSet, a, b types.Object) bool {
	if a == nil || b == nil {
		return a == b
	}
	if pa, ok := a.(*types.PkgName); ok {
		pb, ok := b.(*types.PkgName)
		return ok && pa.Imported().Path() == pb.Imported().Path()
	}
	if a.Parent() == types.Universe || b.Parent() == types.Universe {
		return a == b
	}
	return keyOf(fset, a) == keyOf(fset, b)
}

// paramObjects maps each parameter's object to its index in t.params,
// and the receiver's to -1.
func (t *target) paramObjects(info *types.Info) map[types.Object]int {
	m := map[types.Object]int{}
	if r := info.Defs[recvName(t.decl)]; r != nil {
		m[r] = -1
	}
	for i, p := range t.params {
		m[info.Defs[p.field.Names[p.index]]] = i
	}
	return m
}

// recvName returns the name of fd's receiver, or nil.
func recvName(fd *ast.FuncDecl) *ast.Ident {
	if fd.Recv == nil || len(fd.Recv.List[0].Names) == 0 {
		return nil
	}
	return fd.Recv.List[0].Names[0]
}

func isTuple(t types.Type) bool {
	_, ok := t.(*types.Tuple)
	return ok
}

// substitute writes expr (from the declaring file, whose source is src) as
// the call c passes it: each parameter it uses replaced by the argument
// for it, taken from csrc, the calling file's source.
func (t *target) substitute(info *types.Info, src []byte, expr ast.Expr, c call, csrc []byte) string {
	params := t.paramObjects(info)
	type repl struct {
		start, end int
		text       string
	}
	var rs []repl
	for _, id := range analysis.ScopeRefs(info, expr) {
		i, ok := params[info.Uses[id]]
		if !ok {
			continue
		}
		var arg ast.Expr
		if i < 0 {
			arg = c.recv()
		} else {
			arg = c.expr.Args[i]
		}
		text := string(csrc[t.off(arg.Pos()):t.off(arg.End())])
		if !simpleOperand(arg) {
			text = "(" + text + ")"
		}
		if i < 0 && !selected(expr, id) {
			// The call takes the receiver's address or copies what it
			// points to; outside a selector that must be written out.
			recvPtr, argPtr := analysis.IsPointer(t.obj.Signature().Recv().Type()), analysis.IsPointer(c.pkg.TypesInfo.TypeOf(arg))
			switch {
			case recvPtr && !argPtr:
				text = "(&" + text + ")"
			case !recvPtr && argPtr:
				text = "(*" + text + ")"
			}
		}
		rs = append(rs, repl{t.off(id.Pos()), t.off(id.End()), text})
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].start > rs[j].start })
	s := string(src[t.off(expr.Pos()):t.off(expr.End())])
	base := t.off(expr.Pos())
	for _, r := range rs {
		s = s[:r.start-base] + r.text + s[r.end-base:]
	}
	return strings.TrimSpace(s)
}

// selected reports whether id is the operand of a selector in expr (id.f
// or id.m), where Go takes the address or dereferences as needed.
func selected(expr ast.Expr, id *ast.Ident) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		if se, ok := n.(*ast.SelectorExpr); ok && ast.Unparen(se.X) == id {
			found = true
		}
		return !found
	})
	return found
}

// simpleOperand reports whether e can replace a name inside another
// expression without parentheses.
func simpleOperand(e ast.Expr) bool {
	switch e.(type) {
	case *ast.Ident, *ast.BasicLit, *ast.CompositeLit, *ast.SelectorExpr, *ast.IndexExpr, *ast.CallExpr, *ast.ParenExpr:
		return true
	}
	return false
}
