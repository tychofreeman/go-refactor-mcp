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
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/pattern"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/site"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// IntroduceParameter is the "introduce_parameter" refactoring.
type IntroduceParameter struct{}

// NewIntroduceParameter returns the introduce_parameter refactoring.
func NewIntroduceParameter() *IntroduceParameter { return &IntroduceParameter{} }

func (*IntroduceParameter) Name() string { return "introduce_parameter" }

func (*IntroduceParameter) Description() string {
	return `Replace an expression in a function's body with a new parameter, and pass the expression at every call, written in the call's own terms: introducing u.Name as name, func greet(u User) string { return "hi " + u.Name } becomes func greet(u User, name string) string { return "hi " + name }, and greet(a) becomes greet(a, a.Name). Then remove_parameter u once the body no longer uses it. The receiver works too: m.decl in a method of m becomes mv.decl at the call mv.check().
In a method of an interface, every method of the interface's types gets it, each for the same expression of its own parameters: p.Name in Fern.Water makes Water(p Plant) Water(p Plant, name string) in Species and every species, and s.Water(p) s.Water(p, p.Name).
Identify the expression by file and its source text, with function (its first use in that body) or the line it starts on (column if the text is on the line twice). The parameter goes last.
The expression is then evaluated once, at the call, so it must give the same value and do the same there: one that could panic or reads memory other code can change must be what the body evaluates first, on every call. Refusals say which rule a case breaks.`
}

func (*IntroduceParameter) InputSchema() json.RawMessage {
	return refactor.Schema[introArgs]()
}

type introArgs struct {
	File       string `json:"file" desc:"Path of the file, relative to the workspace root."`
	Function   string `json:"function,omitempty" desc:"Instead of line: the function (Name or Type.Method) whose body uses the expression; its first use."`
	Line       int    `json:"line,omitempty" desc:"1-based line on which the expression starts."`
	Expression string `json:"expression" desc:"Source text of the expression, e.g. \"p.x\"."`
	Parameter  string `json:"parameter" desc:"Name of the new parameter."`
	Column     int    `json:"column,omitempty" desc:"Optional 1-based column where the expression starts, only needed if the text occurs more than once on the line."`
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
	opened, err := site.At(ws, path)
	if err != nil {
		return nil, err
	}
	pkg, file, src := opened.Pkg, opened.File, opened.Src
	want, err := locate.ExprTokens(a.Expression)
	if err != nil {
		return nil, err
	}
	expr, err := findIntroduced(ws, file, src, want, a)
	if err != nil {
		return nil, err
	}
	fd, ok := analysis.OutermostFunc(analysis.PathTo(file, expr)).(*ast.FuncDecl)
	if !ok {
		return nil, refactor.Refusef("%s is not in the body of a declared function", a.Expression)
	}
	declPos := ws.Fset.Position(fd.Name.Pos())
	t, err := findFamily(ws, locate.IdentQuery{File: declPos.Filename, Line: declPos.Line, Column: declPos.Column, Name: fd.Name.Name})
	if err != nil {
		return nil, err
	}
	if pkg != t.Pkg { // work in the package variant the target was found in
		pkg = t.Pkg
		for _, f := range pkg.Syntax {
			if f.Pos() <= t.decl.Pos() && t.decl.Pos() < f.End() {
				file = f
			}
		}
		if expr, err = findIntroduced(ws, file, src, want, a); err != nil {
			return nil, err
		}
	}
	if t.members != nil {
		return t.introduceFamily(a, file, src, expr)
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

	o := t.Off(expr.Pos())
	fes := []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{
		t.paramEdit(at, a.Parameter, typSrc),
		{Start: o, End: t.Off(expr.End()), NewText: a.Parameter},
	}}}
	srcs := map[string][]byte{path: src}
	adders := map[string]*imports.Adder{}
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
		value, err := t.substitute(pkg.TypesInfo, src, expr, c, srcs[cpath], adders)
		if err != nil {
			return nil, err
		}
		fes = append(fes, edit.FileEdit{Path: cpath, Edits: []edit.TextEdit{t.argEdit(c.expr, at, value)}})
	}
	for p, ad := range adders {
		fes = append(fes, edit.FileEdit{Path: p, Edits: ad.Edits()})
	}
	return t.newPlan(fmt.Sprintf("introduced parameter %q for %s in %s", a.Parameter+" "+typSrc, a.Expression, t.obj.Name()), fes), nil
}

// findIntroduced finds the expression a names: on a.Line, or else the
// first in the body of a.Function.
func findIntroduced(ws *workspace.Workspace, file *ast.File, src []byte, want []string, a introArgs) (ast.Expr, error) {
	if a.Function == "" || a.Line != 0 {
		return locate.FindExpr(ws, file, src, want, a.Line, a.Column, false)
	}
	fd := locate.Func(file, a.Function)
	if fd == nil || fd.Body == nil {
		return nil, refactor.Refusef("no function %s with a body in %s; give Name, or Type.Method for a method", a.Function, a.File)
	}
	tf := ws.Fset.File(file.Pos())
	var found ast.Expr
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if e, ok := n.(ast.Expr); ok && found == nil && locate.TokensMatch(tf, src, e, want) {
			found = e
		}
		return found == nil
	})
	if found == nil {
		return nil, refactor.Refusef("%s does not use %s", a.Function, a.Expression)
	}
	return found, nil
}

// introduceFamily introduces the parameter in every method of t's family
// (an interface method and the methods that implement it): in each body,
// the expression written with that method's own parameters becomes the
// parameter, and every call of any of them passes expr, of t's body.
func (t *target) introduceFamily(a introArgs, file *ast.File, src []byte, expr ast.Expr) (*refactor.Plan, error) {
	info := t.Pkg.TypesInfo
	params := t.paramObjects(info)
	for _, id := range analysis.ScopeRefs(info, expr) {
		if i, ok := params[info.Uses[id]]; ok && i < 0 {
			return nil, refactor.Refusef("%s uses the receiver %s, and each method of %s has its own; introduce a parameter for an expression of the parameters", a.Expression, id.Name, t.groupName())
		}
	}
	at, err := t.named().insertIndex("")
	if err != nil {
		return nil, err
	}
	typ := info.TypeOf(expr)
	srcs := map[string][]byte{t.declFile(): src}
	read := func(path string) ([]byte, error) {
		if srcs[path] == nil {
			b, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			srcs[path] = b
		}
		return srcs[path], nil
	}
	var fes []edit.FileEdit
	adders := map[string]*imports.Adder{}
	typSrc := ""
	for _, m := range t.group() {
		mfile := m.File
		ts, missing := analysis.TypeString(typ, mfile, m.Pkg.Types, analysis.ImportMap(m.Pkg.Types))
		if missing != "" {
			return nil, refactor.Refusef("the type of %s, %s, cannot be written in %s without importing %s", a.Expression, typ, t.WS.Rel(m.declFile()), missing)
		}
		if m == t {
			typSrc = ts
		}
		if err := m.checkNewName(a.Parameter); err != nil {
			return nil, err
		}
		if err := m.checkInterfaces(func(ps []string) []string { return slices.Insert(ps, at, typeKey(typ)) }); err != nil {
			return nil, err
		}
		edits := []edit.TextEdit{m.paramEdit(at, a.Parameter, ts)}
		if m.decl != nil {
			for _, x := range m.occurrences(t, expr) {
				if err := m.checkIntroduced(m.Pkg.TypesInfo, mfile, x); err != nil {
					return nil, refactor.Refusef("in %s.%s: %v", t.typeName(receiverBase(m.obj.Signature().Recv().Type())), m.obj.Name(), err)
				}
				edits = append(edits, edit.TextEdit{Start: m.Off(x.Pos()), End: m.Off(x.End()), NewText: a.Parameter})
			}
		}
		fes = append(fes, edit.FileEdit{Path: m.declFile(), Edits: edits})
		for _, c := range m.calls {
			cpath := t.WS.Fset.Position(c.expr.Pos()).Filename
			csrc, err := read(cpath)
			if err != nil {
				return nil, err
			}
			if err := t.checkCall(info, expr, c, csrc); err != nil {
				return nil, err
			}
			value, err := t.substitute(info, src, expr, c, csrc, adders)
			if err != nil {
				return nil, err
			}
			fes = append(fes, edit.FileEdit{Path: cpath, Edits: []edit.TextEdit{m.argEdit(c.expr, at, value)}})
		}
	}
	for p, ad := range adders {
		fes = append(fes, edit.FileEdit{Path: p, Edits: ad.Edits()})
	}
	return t.newPlan(fmt.Sprintf("introduced parameter %q for %s in %s", a.Parameter+" "+typSrc, a.Expression, t.groupName()), fes), nil
}

// occurrences returns the expressions of m's body that are expr, of t's
// body, with each of t's parameters in it m's parameter at the same place.
func (m *target) occurrences(t *target, expr ast.Expr) []ast.Expr {
	tparams := t.paramObjects(t.Pkg.TypesInfo)
	holes := map[types.Object]int{}
	for o, i := range tparams {
		if i >= 0 {
			holes[o] = 1
		}
	}
	p := &pattern.Matcher{Pkg: t.Pkg, Holes: holes}
	minfo := m.Pkg.TypesInfo
	mparams := m.paramObjects(minfo)
	var out []ast.Expr
	ast.Inspect(m.decl.Body, func(n ast.Node) bool {
		x, ok := n.(ast.Expr)
		if !ok {
			return true
		}
		at := p.At(minfo)
		if !at.Match(expr, x) {
			return true
		}
		for k, b := range at.Bound {
			id, ok := ast.Unparen(b).(*ast.Ident)
			if !ok {
				return true
			}
			if j, ok := mparams[minfo.Uses[id]]; !ok || j != tparams[k.Obj] {
				return true
			}
		}
		out = append(out, x)
		return false
	})
	return out
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
		if !ok || !fa.IsLocal(v) || expr.Pos() <= v.Pos() && v.Pos() < expr.End() {
			continue // declared in expr itself, such as a function literal's parameter: it goes with expr
		}
		if _, ok := isParam[v]; !ok {
			return refactor.Refusef("%s uses %s, declared in the body of %s, which the calls cannot see", q, v.Name(), t.obj.Name())
		}
		if mods := fa.Modified[v]; len(mods) > 0 {
			return refactor.Refusef("%s changes %s (at %s), so %s could have another value there than at the call", t.obj.Name(), v.Name(), t.WS.Position(mods[0].Pos()), q)
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
	where := t.WS.Position(c.expr.Pos())
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
	if sig := t.obj.Signature(); sig.Variadic() && !c.expr.Ellipsis.IsValid() {
		// The call passes the variadic arguments one by one, if any.
		for _, id := range analysis.ScopeRefs(info, expr) {
			if i, ok := isParam[info.Uses[id]]; ok && i == sig.Params().Len()-1 {
				return refactor.Refusef("%s uses the variadic parameter %s, which the call at %s passes as separate arguments", types.ExprString(expr), id.Name, where)
			}
		}
	}
	scope := c.pkg.Types.Scope().Innermost(c.expr.Pos())
	for _, id := range analysis.ScopeRefs(info, expr) {
		o := info.Uses[id]
		if _, ok := isParam[o]; ok || o == nil || expr.Pos() <= o.Pos() && o.Pos() < expr.End() {
			continue // a parameter, or declared in expr itself (it goes with expr)
		}
		var there types.Object
		if scope != nil {
			_, there = scope.LookupParent(id.Name, c.expr.Pos())
		}
		if t.qualified(o, c) && there == nil {
			continue // written pkg.Name at the call
		}
		if !sameObject(t.WS.Fset, o, there) {
			return refactor.Refusef("%s means something else at the call at %s (or is not visible there)", id.Name, where)
		}
	}
	return nil
}

// qualified reports whether o, used in the introduced expression, is
// written qualified at the call c: an exported package-level name of the
// function's package, and c in another package.
func (t *target) qualified(o types.Object, c call) bool {
	home := t.obj.Pkg()
	return home != nil && o.Exported() && o.Parent() == home.Scope() && c.pkg.Types != nil && c.pkg.Types.Path() != home.Path()
}

// checkArgEffects refuses a call whose receiver or an argument has side
// effects: the new argument repeats some of them, and its evaluation is
// not ordered with theirs.
func (t *target) checkArgEffects(c call, csrc []byte) error {
	acts := func(e ast.Expr) bool { return t.WS.Effects().Of(funcAround(c), e).Effect != "" }
	if x := c.recv(); x != nil && acts(x) {
		return refactor.Refusef("the receiver %s at %s has side effects; assign it to a variable first",
			csrc[t.Off(x.Pos()):t.Off(x.End())], t.WS.Position(x.Pos()))
	}
	for i, arg := range c.expr.Args {
		if acts(arg) {
			p := t.params[min(i, len(t.params)-1)]
			return refactor.Refusef("the argument %s for %s at %s has side effects; assign it to a variable first",
				csrc[t.Off(arg.Pos()):t.Off(arg.End())], p.name, t.WS.Position(arg.Pos()))
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
func (t *target) substitute(info *types.Info, src []byte, expr ast.Expr, c call, csrc []byte, adders map[string]*imports.Adder) (string, error) {
	params := t.paramObjects(info)
	type repl struct {
		start, end int
		text       string
	}
	var rs []repl
	for _, id := range analysis.ScopeRefs(info, expr) {
		if o := info.Uses[id]; o != nil && t.qualified(o, c) {
			q, err := t.importName(c, o.Pkg(), adders)
			if err != nil {
				return "", err
			}
			rs = append(rs, repl{t.Off(id.Pos()), t.Off(id.Pos()), q + "."})
			continue
		}
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
		text := string(csrc[t.Off(arg.Pos()):t.Off(arg.End())])
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
		rs = append(rs, repl{t.Off(id.Pos()), t.Off(id.End()), text})
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].start > rs[j].start })
	s := string(src[t.Off(expr.Pos()):t.Off(expr.End())])
	base := t.Off(expr.Pos())
	for _, r := range rs {
		s = s[:r.start-base] + r.text + s[r.end-base:]
	}
	return strings.TrimSpace(s), nil
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
