// Package switchmethod implements type_switch_to_method, which turns a
// type switch into a method of each type it switches on.
package switchmethod

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"sort"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/site"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// SwitchToMethod is the "type_switch_to_method" refactoring.
type SwitchToMethod struct{ refactor.Named[args] }

// New returns the type_switch_to_method refactoring.
func New() *SwitchToMethod {
	return &SwitchToMethod{Named: refactor.Called[args]("type_switch_to_method")}
}

func (*SwitchToMethod) Description() string {
	return `Turn a type switch into a method of each type it switches on: each case's body becomes that type's method, the interface gets the method, and the switch becomes one call. With method care,
  func Care(p Plant) string { switch k := p.Species.(type) { case Fern: return p.Name; case Cactus: return k.Note(p.Name) }; return p.Name }
becomes
  func Care(p Plant) string { if p.Species != nil { return p.Species.care(p) }; return p.Name }
  func (Fern) care(p Plant) string { return p.Name }
  func (k Cactus) care(p Plant) string { return k.Note(p.Name) }
and Species gets care(p Plant) string. The variables a case uses become parameters, the switch's variable (k) the receiver; case nil (or default) stays behind the nil check. Every case must return, or none.
Identify the switch by file and line, or by function when it is that function's only type switch; method is the new method's name.
Refuses, saying why: a switch with an init statement or a label, or in a function literal; a type of the module that implements the interface without a case, a case of several types, of a pointer type or of a type of another package, a case that assigns a variable it uses or leaves the case (break, continue, goto, defer), a method name a type or the interface already has.`
}

type args struct {
	File     string `json:"file" desc:"Path of the file with the switch, relative to the workspace root."`
	Line     int    `json:"line,omitempty" desc:"1-based line on which the switch statement starts."`
	Function string `json:"function,omitempty" desc:"Instead of line: the function (Name or Type.Method) whose only type switch it is."`
	Method   string `json:"method" desc:"Name of the new method."`
}

// A kase is a case of the switch for one type of the package, which gets
// the method.
type kase struct {
	clause *ast.CaseClause
	typ    types.Type   // T or *T
	named  *types.Named // T
	recv   *types.Var   // the switch's variable in this case, or nil
}

type switcher struct {
	site.Site
	fd      *ast.FuncDecl
	stmt    ast.Stmt            // the switch
	block   *ast.BlockStmt      // its body
	ts      *ast.TypeSwitchStmt // the switch, when it is a type switch
	subject ast.Expr
	iface   *types.Named
	method  string

	cases          []kase
	nilCase, deflt *ast.CaseClause
	params         []*types.Var // the variables the cases use, in order of declaration
}

// Plan checks preconditions and computes the edits.
func (*SwitchToMethod) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if !token.IsIdentifier(a.Method) {
		return nil, refactor.Refusef("method %q is not a valid Go identifier", a.Method)
	}
	s, err := find(ws, a, true)
	if err != nil {
		return nil, err
	}
	for _, check := range []func() error{s.checkSwitch, s.collectCases, s.checkCoverage, s.checkName, s.checkBodies} {
		if err := check(); err != nil {
			return nil, err
		}
	}
	return s.plan()
}

// find locates the switch a addresses: a type switch, or with values, a
// switch with a tag.
func find(ws *workspace.Workspace, a args, typeSwitch bool) (*switcher, error) {
	what := "switch"
	if typeSwitch {
		what = "type switch"
	}
	if a.Line == 0 && a.Function == "" {
		return nil, refactor.Refusef("give line (where the switch starts) or function (whose only %s it is)", what)
	}
	path := ws.Abs(a.File)
	pkg, file := ws.FileSyntax(path)
	if file == nil {
		return nil, refactor.Refusef("file %s is not part of the loaded packages %v", ws.Rel(path), ws.Patterns)
	}
	tf := ws.Fset.File(file.Pos())
	var found []ast.Stmt
	var fds []*ast.FuncDecl
	for _, d := range file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil || a.Function != "" && locate.FuncName(fd) != a.Function {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			var st ast.Stmt
			switch n := n.(type) {
			case *ast.TypeSwitchStmt:
				if typeSwitch {
					st = n
				}
			case *ast.SwitchStmt:
				if !typeSwitch {
					st = n
				}
			}
			if st != nil && (a.Line == 0 || tf.Line(st.Pos()) == a.Line) {
				found = append(found, st)
				fds = append(fds, fd)
			}
			return true
		})
	}
	switch {
	case len(found) == 0 && a.Line > 0:
		return nil, refactor.Refusef("no %s starts on line %d of %s", what, a.Line, ws.Rel(path))
	case len(found) == 0:
		return nil, refactor.Refusef("%s has no function %s with a %s", ws.Rel(path), a.Function, what)
	case len(found) > 1:
		var lines []string
		for _, st := range found {
			lines = append(lines, fmt.Sprint(tf.Line(st.Pos())))
		}
		return nil, refactor.Refusef("%s has %d %ses, on lines %s; give line", a.Function, len(found), what, strings.Join(lines, ", "))
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := &switcher{Site: site.Site{TF: ws.Fset.File(file.Pos()), WS: ws, Pkg: pkg, Info: pkg.TypesInfo, File: file, Src: src}, fd: fds[0], stmt: found[0], method: a.Method}
	switch st := found[0].(type) {
	case *ast.TypeSwitchStmt:
		s.ts, s.block = st, st.Body
	case *ast.SwitchStmt:
		s.block = st.Body
	}
	return s, nil
}

func (s *switcher) at(n ast.Node) string { return s.WS.Position(n.Pos()) }

func (s *switcher) typeName(t types.Type) string {
	return types.TypeString(t, types.RelativeTo(s.Pkg.Types))
}

// checkSwitch checks the switch itself: its subject, an interface type of
// the package, and where it is.
func (s *switcher) checkSwitch() error {
	ts := s.ts
	if err := s.checkPlace(ts.Init); err != nil {
		return err
	}
	switch as := ts.Assign.(type) {
	case *ast.AssignStmt:
		s.subject = as.Rhs[0].(*ast.TypeAssertExpr).X
	case *ast.ExprStmt:
		s.subject = as.X.(*ast.TypeAssertExpr).X
	}
	t := s.Info.TypeOf(s.subject)
	named, _ := types.Unalias(t).(*types.Named)
	if named == nil || !types.IsInterface(named) || named.Obj().Pkg() != s.Pkg.Types {
		return refactor.Refusef("%s is of type %s, not an interface type declared in package %s", s.Text(s.subject), s.typeName(t), s.Pkg.Types.Name())
	}
	if named.TypeParams().Len() > 0 {
		return refactor.Refusef("the interface %s has type parameters; not supported yet", named.Obj().Name())
	}
	s.iface = named
	return s.checkStable()
}

// checkPlace refuses a switch with an init statement (init), with a
// label, or in a function literal or a generic function.
func (s *switcher) checkPlace(init ast.Stmt) error {
	switch {
	case init != nil:
		return refactor.Refusef("the switch at %s has an init statement; move it before the switch first", s.at(s.stmt))
	case s.fd.Type.TypeParams != nil:
		return refactor.Refusef("%s has type parameters, which a method cannot use", locate.FuncName(s.fd))
	}
	for _, n := range analysis.PathTo(s.fd.Body, s.stmt) {
		switch n := n.(type) {
		case *ast.FuncLit:
			return refactor.Refusef("the switch at %s is in a function literal; not supported yet", s.at(s.stmt))
		case *ast.LabeledStmt:
			if n.Stmt == s.stmt {
				return refactor.Refusef("the switch at %s has a label; remove it first", s.at(s.stmt))
			}
		}
	}
	return nil
}

// checkStable refuses a subject that the new code evaluates again, when
// it could have another value then.
func (s *switcher) checkStable() error {
	if culprit, ok := s.WS.Effects().Of(analysis.AnalyzeFunc(s.Info, s.fd, s.WS.Effects()), s.subject).Stable(false); !ok {
		return refactor.Refusef("%s is evaluated again by the call, and %s could change it; assign it to a variable first (extract_variable)", s.Text(s.subject), s.Text(culprit))
	}
	return nil
}

// collectCases sorts the clauses into the cases that become methods and
// the ones that stay: case nil and default.
func (s *switcher) collectCases() error {
	for _, st := range s.ts.Body.List {
		cc := st.(*ast.CaseClause)
		switch {
		case cc.List == nil:
			s.deflt = cc
			continue
		case len(cc.List) > 1:
			return refactor.Refusef("the case at %s lists several types; give each its own case first", s.at(cc))
		case s.Info.Types[cc.List[0]].IsNil():
			s.nilCase = cc
			continue
		}
		t := s.Info.TypeOf(cc.List[0])
		base := t
		if p, ok := t.(*types.Pointer); ok {
			base = p.Elem()
		}
		named, ok := types.Unalias(base).(*types.Named)
		if !ok || named.Obj().Pkg() != s.Pkg.Types || types.IsInterface(named) || named.TypeParams().Len() > 0 {
			return refactor.Refusef("the case %s at %s is not a type of package %s that can have methods", s.typeName(t), s.at(cc), s.Pkg.Types.Name())
		}
		if _, ok := named.Underlying().(*types.Pointer); ok {
			return refactor.Refusef("the case %s at %s is a pointer type, which cannot have methods", s.typeName(t), s.at(cc))
		}
		recv, _ := s.Info.Implicits[cc].(*types.Var)
		s.cases = append(s.cases, kase{cc, t, named, recv})
	}
	if len(s.cases) == 0 {
		return refactor.Refusef("the switch at %s has no case for a type", s.at(s.ts))
	}
	seen := map[*types.TypeName]types.Type{}
	for _, c := range s.cases {
		if other, ok := seen[c.named.Obj()]; ok {
			return refactor.Refusef("the switch has cases for both %s and %s, which cannot both have the method", s.typeName(other), s.typeName(c.typ))
		}
		seen[c.named.Obj()] = c.typ
	}
	for _, cc := range []*ast.CaseClause{s.nilCase, s.deflt} {
		if cc == nil {
			continue
		}
		if v := s.Info.Implicits[cc]; v != nil && s.uses(cc, v) {
			return refactor.Refusef("the case at %s uses %s, which it would no longer have; write nil or %s in its place first", s.at(cc), v.Name(), s.Text(s.subject))
		}
	}
	return nil
}

// uses reports whether the body of cc refers to obj.
func (s *switcher) uses(cc *ast.CaseClause, obj types.Object) bool {
	found := false
	for _, st := range cc.Body {
		ast.Inspect(st, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && s.Info.Uses[id] == obj {
				found = true
			}
			return !found
		})
	}
	return found
}

// checkCoverage refuses when some value of the interface would reach a
// different case after the change: a type of the module that implements
// the interface must have a case, so that it gets the method, and a
// pointer to a type that has a case must not be made, since it would run
// that case where now it runs the default.
func (s *switcher) checkCoverage() error {
	has := map[string]bool{}
	for _, c := range s.cases {
		has[typeKey(c.typ)] = true
	}
	it := s.iface.Underlying().(*types.Interface)
	for _, n := range s.namedTypes() {
		ptr := types.NewPointer(n)
		switch {
		case has[typeKey(n)]:
		case has[typeKey(ptr)] && types.Implements(n, it):
			return refactor.Refusef("type %s implements %s, but the case is *%s; a method on *%s would make %s stop implementing it", n.Obj().Name(), s.iface.Obj().Name(), n.Obj().Name(), n.Obj().Name(), n.Obj().Name())
		case has[typeKey(ptr)]:
		case types.Implements(n, it) || types.Implements(ptr, it):
			return refactor.Refusef("type %s implements %s but has no case; add one (add_case), so that it gets the method too", n.Obj().Name(), s.iface.Obj().Name())
		}
	}
	for _, c := range s.cases {
		if c.typ != types.Type(c.named) {
			continue
		}
		if at := s.pointerMade(c.named); at != nil {
			return refactor.Refusef("a *%s is made at %s; with a method on %s, a *%s in %s would run the case %s, which it does not reach now",
				c.named.Obj().Name(), s.at(at), c.named.Obj().Name(), c.named.Obj().Name(), s.Text(s.subject), c.named.Obj().Name())
		}
	}
	return nil
}

// namedTypes lists the named types declared in the workspace, once each,
// in order of declaration.
func (s *switcher) namedTypes() []*types.Named {
	var out []*types.Named
	seen := map[string]bool{}
	for _, p := range s.WS.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, o := range p.TypesInfo.Defs {
			tn, ok := o.(*types.TypeName)
			if !ok || tn.IsAlias() || seen[analysis.ObjectKey(s.WS.Fset, tn)] {
				continue
			}
			if n, ok := tn.Type().(*types.Named); ok && !types.IsInterface(n) {
				seen[analysis.ObjectKey(s.WS.Fset, tn)] = true
				out = append(out, n)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return analysis.ObjectKey(s.WS.Fset, out[i].Obj()) < analysis.ObjectKey(s.WS.Fset, out[j].Obj())
	})
	return out
}

// pointerMade returns an expression of the workspace that makes a *T:
// &v or &T{...} of a T, or new(T); or nil.
func (s *switcher) pointerMade(named *types.Named) ast.Node {
	key := typeKey(named)
	for _, p := range s.WS.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		var found ast.Node
		for _, f := range p.Syntax {
			ast.Inspect(f, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.UnaryExpr:
					if n.Op == token.AND && typeKey(p.TypesInfo.TypeOf(n.X)) == key {
						found = n
					}
				case *ast.CallExpr:
					if id, ok := n.Fun.(*ast.Ident); ok && len(n.Args) == 1 {
						if _, builtin := p.TypesInfo.Uses[id].(*types.Builtin); builtin && id.Name == "new" && typeKey(p.TypesInfo.TypeOf(n.Args[0])) == key {
							found = n
						}
					}
				}
				return found == nil
			})
			if found != nil {
				return found
			}
		}
	}
	return nil
}

// typeKey identifies a type across package variants, by its spelling.
func typeKey(t types.Type) string {
	if t == nil {
		return ""
	}
	return types.TypeString(t, nil)
}

// checkName refuses a method name that the interface, a case's type, or
// a type embedding one already has, or that code looks for at run time.
func (s *switcher) checkName() error {
	it := s.iface.Underlying().(*types.Interface)
	for i := 0; i < it.NumMethods(); i++ {
		if it.Method(i).Name() == s.method {
			return refactor.Refusef("interface %s already has a method %s", s.iface.Obj().Name(), s.method)
		}
	}
	for _, c := range s.cases {
		if o, _, _ := types.LookupFieldOrMethod(types.NewPointer(c.named), true, s.Pkg.Types, s.method); o != nil {
			return refactor.Refusef("%s already has a %s %s", c.named.Obj().Name(), memberKind(o), s.method)
		}
		if token.IsExported(s.method) {
			if iface := analysis.MethodInterface(s.WS.Pkgs, c.named, s.method, true); iface != "" {
				return refactor.Refusef("a method %s could make %s implement %s, which code checks for at run time, and change behavior with no compile error; choose another name", s.method, c.named.Obj().Name(), iface)
			}
		}
		for _, n := range s.namedTypes() {
			if typeKey(n) == typeKey(c.named) || !embeds(n, c.named, map[string]bool{}) {
				continue
			}
			if o, _, _ := types.LookupFieldOrMethod(types.NewPointer(n), true, s.Pkg.Types, s.method); o != nil {
				return refactor.Refusef("%s embeds %s and already has a %s %s, which the new method could hide or make ambiguous", n.Obj().Name(), c.named.Obj().Name(), memberKind(o), s.method)
			}
		}
	}
	return nil
}

// embeds reports whether the struct underlying t embeds named, directly or
// through other embedded fields.
func embeds(t types.Type, named *types.Named, seen map[string]bool) bool {
	if seen[typeKey(t)] {
		return false
	}
	seen[typeKey(t)] = true
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		return false
	}
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		if !f.Embedded() {
			continue
		}
		ft := f.Type()
		if p, ok := ft.(*types.Pointer); ok {
			ft = p.Elem()
		}
		if typeKey(ft) == typeKey(named) || embeds(ft, named, seen) {
			return true
		}
	}
	return false
}

// memberKind says what a field or method found by selection is.
func memberKind(o types.Object) string {
	if _, ok := o.(*types.Func); ok {
		return "method"
	}
	return "field"
}

// returns reports whether the case's body returns: every path ends in a
// return. It refuses a body that returns on some paths only.
func (s *switcher) returns(cc *ast.CaseClause) (bool, error) {
	has := false
	for _, st := range cc.Body {
		ast.Inspect(st, func(n ast.Node) bool {
			switch n.(type) {
			case *ast.FuncLit:
				return false
			case *ast.ReturnStmt:
				has = true
			}
			return !has
		})
	}
	if has && !analysis.TerminatingList(s.Info, cc.Body, nil) {
		return false, refactor.Refusef("the case at %s returns on some paths only; every path of a case must return, or none", s.at(cc))
	}
	return has, nil
}

func (s *switcher) caseName(cc *ast.CaseClause) string {
	if cc.List == nil {
		return "default"
	}
	return s.Text(cc.List[0])
}

// checkBodies checks what the cases do: they all return, or none does;
// what they leave by is a return; and the variables they use, which
// become parameters, they only read.
func (s *switcher) checkBodies() error {
	var first *ast.CaseClause
	firstReturns := false
	for i, c := range s.cases {
		r, err := s.returns(c.clause)
		if err != nil {
			return err
		}
		if i == 0 {
			first, firstReturns = c.clause, r
		} else if r != firstReturns {
			ret, not := first, c.clause
			if !firstReturns {
				ret, not = c.clause, first
			}
			return refactor.Refusef("every case must return, or none: the case %s at %s returns and the case %s at %s does not",
				s.caseName(ret), s.at(ret), s.caseName(not), s.at(not))
		}
	}
	for _, cc := range []*ast.CaseClause{s.nilCase, s.deflt} {
		if cc != nil {
			if err := s.checkLeaves(cc, true); err != nil {
				return err
			}
		}
	}
	vars := map[*types.Var]bool{}
	for _, c := range s.cases {
		if err := s.checkLeaves(c.clause, false); err != nil {
			return err
		}
		used, err := s.freeVars(c)
		if err != nil {
			return err
		}
		for _, v := range used {
			vars[v] = true
		}
	}
	names := map[string]*types.Var{}
	for v := range vars {
		if other, ok := names[v.Name()]; ok {
			return refactor.Refusef("the cases use two variables named %s (declared at %s and %s); rename one first", v.Name(), s.WS.Position(other.Pos()), s.WS.Position(v.Pos()))
		}
		names[v.Name()] = v
		s.params = append(s.params, v)
	}
	sort.Slice(s.params, func(i, j int) bool { return s.params[i].Pos() < s.params[j].Pos() })
	for _, c := range s.cases {
		if err := s.checkWrites(c); err != nil {
			return err
		}
	}
	return nil
}

// checkLeaves refuses what would leave a case's body other than by
// return: break, continue, goto and fallthrough out of it, defer and
// recover, which belong to the function, and a bare return. For a case
// that stays (nil, default), only a break of the switch.
func (s *switcher) checkLeaves(cc *ast.CaseClause, stays bool) error {
	var err error
	var walk func(n ast.Node, loop, breakable bool)
	walk = func(n ast.Node, loop, breakable bool) {
		ast.Inspect(n, func(n ast.Node) bool {
			if err != nil || n == nil {
				return false
			}
			switch n := n.(type) {
			case *ast.FuncLit:
				return false
			case *ast.ForStmt, *ast.RangeStmt:
				for _, c := range children(n) {
					walk(c, true, true)
				}
				return false
			case *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
				for _, c := range children(n) {
					walk(c, loop, true)
				}
				return false
			case *ast.BranchStmt:
				switch {
				case n.Tok == token.BREAK && n.Label == nil && !breakable,
					!stays && n.Tok == token.CONTINUE && n.Label == nil && !loop,
					!stays && n.Label != nil && !labelIn(cc, n.Label.Name),
					!stays && n.Tok == token.FALLTHROUGH:
					err = refactor.Refusef("the %s at %s leaves the case", n.Tok, s.at(n))
				}
			case *ast.DeferStmt:
				if !stays {
					err = refactor.Refusef("the defer at %s would run when the method returns, not the function", s.at(n))
				}
			case *ast.CallExpr:
				if id, ok := ast.Unparen(n.Fun).(*ast.Ident); ok && !stays && id.Name == "recover" {
					if _, b := s.Info.Uses[id].(*types.Builtin); b {
						err = refactor.Refusef("the recover at %s would recover in the method, not the function", s.at(n))
					}
				}
			case *ast.ReturnStmt:
				if !stays && len(n.Results) == 0 && s.fd.Type.Results != nil && len(s.fd.Type.Results.List) > 0 {
					err = refactor.Refusef("the bare return at %s returns named results; name them first", s.at(n))
				}
			}
			return true
		})
	}
	for _, st := range cc.Body {
		walk(st, false, false)
	}
	return err
}

// children returns the direct children of n.
func children(n ast.Node) []ast.Node {
	var out []ast.Node
	ast.Inspect(n, func(c ast.Node) bool {
		if c == n {
			return true
		}
		if c != nil {
			out = append(out, c)
		}
		return false
	})
	return out
}

// labelIn reports whether the case's body declares the label name.
func labelIn(cc *ast.CaseClause, name string) bool {
	found := false
	for _, st := range cc.Body {
		ast.Inspect(st, func(n ast.Node) bool {
			if l, ok := n.(*ast.LabeledStmt); ok && l.Label.Name == name {
				found = true
			}
			return !found
		})
	}
	return found
}

// freeVars returns the variables of the function that the case's body
// uses and does not declare, but its switch variable: they become
// parameters. It refuses local constants and types, which a method
// cannot see.
func (s *switcher) freeVars(c kase) ([]*types.Var, error) {
	var out []*types.Var
	seen := map[*types.Var]bool{}
	var err error
	inside := func(p token.Pos) bool { return c.clause.Pos() <= p && p < c.clause.End() }
	local := func(o types.Object) bool {
		return o.Parent() != nil && o.Parent() != s.Pkg.Types.Scope() && s.fd.Pos() <= o.Pos() && o.Pos() < s.fd.End() && !inside(o.Pos())
	}
	for _, st := range c.clause.Body {
		ast.Inspect(st, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok || err != nil {
				return err == nil
			}
			switch o := s.Info.Uses[id].(type) {
			case *types.Var:
				if o != c.recv && !o.IsField() && local(o) && !seen[o] {
					seen[o] = true
					out = append(out, o)
				}
			case *types.Const, *types.TypeName:
				if o.Pkg() != nil && local(o) {
					err = refactor.Refusef("the case %s uses %s, declared in %s at %s; move it to package level first", s.caseName(c.clause), o.Name(), locate.FuncName(s.fd), s.WS.Position(o.Pos()))
				}
			}
			return true
		})
	}
	return out, err
}

// checkWrites refuses a case that assigns a parameter, or a part of one
// held by value, or takes its address: in a method it would change a copy.
func (s *switcher) checkWrites(c kase) error {
	param := map[*types.Var]bool{}
	for _, v := range s.params {
		param[v] = true
	}
	var err error
	written := func(e ast.Expr, what string) {
		if v := s.root(e); v != nil && param[v] && err == nil {
			err = refactor.Refusef("the case %s %s %s at %s; in a method it would change a copy", s.caseName(c.clause), what, v.Name(), s.at(e))
		}
	}
	for _, st := range c.clause.Body {
		ast.Inspect(st, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.AssignStmt:
				if n.Tok != token.DEFINE {
					for _, l := range n.Lhs {
						written(l, "assigns")
					}
				}
			case *ast.IncDecStmt:
				written(n.X, "assigns")
			case *ast.RangeStmt:
				if n.Tok == token.ASSIGN {
					written(n.Key, "assigns")
					written(n.Value, "assigns")
				}
			case *ast.UnaryExpr:
				if n.Op == token.AND {
					written(n.X, "takes the address of")
				}
			case *ast.SelectorExpr:
				if sel := s.Info.Selections[n]; sel != nil && sel.Kind() == types.MethodVal && analysis.PtrRecv(sel) && !analysis.IsPointer(s.Info.TypeOf(n.X)) {
					written(n.X, "calls a pointer method on")
				}
			}
			return err == nil
		})
	}
	return err
}

// root returns the variable whose own storage e is: v for v, v.f and v[i]
// of a struct or array held by value; nil past a pointer, slice or map.
func (s *switcher) root(e ast.Expr) *types.Var {
	for e != nil {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
		case *ast.Ident:
			v, _ := s.Info.Uses[x].(*types.Var)
			return v
		case *ast.SelectorExpr:
			if sel := s.Info.Selections[x]; sel == nil || sel.Kind() != types.FieldVal || sel.Indirect() || analysis.IsPointer(s.Info.TypeOf(x.X)) {
				return nil
			}
			e = x.X
		case *ast.IndexExpr:
			if !analysis.IsArray(s.Info.TypeOf(x.X)) {
				return nil
			}
			e = x.X
		default:
			return nil
		}
	}
	return nil
}

// plan writes the methods after the function, adds the method to the
// interface, and puts the call in place of the switch.
func (s *switcher) plan() (*refactor.Plan, error) {
	path := s.WS.Fset.Position(s.fd.Pos()).Filename
	adder := imports.New(s.WS, s.Pkg, s.File, s.Src)
	qual := func(a *imports.Adder) types.Qualifier {
		return func(p *types.Package) string {
			n, _ := a.Name(p)
			return n
		}
	}
	sig := s.signature(qual(adder))

	returning, _ := s.returns(s.cases[0].clause)
	var methods []string
	var typeNames []string
	for _, c := range s.cases {
		recv := ""
		if c.recv != nil && s.uses(c.clause, c.recv) {
			recv = c.recv.Name() + " "
		}
		methods = append(methods, fmt.Sprintf("func (%s%s) %s {\n%s}", recv, s.typeName(c.typ), sig, s.body(c.clause, "\t")))
		typeNames = append(typeNames, s.typeName(c.typ))
	}

	subject := s.Text(s.subject)
	switch ast.Unparen(s.subject).(type) {
	case *ast.Ident, *ast.SelectorExpr, *ast.CallExpr, *ast.IndexExpr:
	default:
		subject = "(" + subject + ")"
	}
	call := subject + "." + s.method + "(" + s.args() + ")"
	if returning {
		call = "return " + call
	}
	stays := s.nilCase
	var notes []string
	if stays == nil {
		stays = s.deflt
		if s.deflt != nil {
			notes = append(notes, fmt.Sprintf("the default case now runs only for nil: every type of the module that implements %s has a case", s.iface.Obj().Name()))
		}
	} else if s.deflt != nil {
		notes = append(notes, fmt.Sprintf("the default case was dropped: every type of the module that implements %s has a case", s.iface.Obj().Name()))
	}
	ind := s.indent()
	var repl string
	switch {
	case stays == nil:
		repl = fmt.Sprintf("if %s != nil {\n%s\t%s\n%s}", s.Text(s.subject), ind, call, ind)
	case returning && analysis.TerminatingList(s.Info, stays.Body, nil):
		repl = fmt.Sprintf("if %s == nil {\n%s%s}\n%s%s", s.Text(s.subject), s.body(stays, ind+"\t"), ind, ind, call)
	default:
		repl = fmt.Sprintf("if %s == nil {\n%s%s} else {\n%s\t%s\n%s}", s.Text(s.subject), s.body(stays, ind+"\t"), ind, ind, call, ind)
	}

	at := textutil.LineEnd(s.Src, s.Off(s.fd.End()))
	edits := []edit.TextEdit{
		{Start: s.Off(s.ts.Pos()), End: s.Off(s.ts.End()), NewText: repl},
		{Start: at, End: at, NewText: "\n\n" + strings.Join(methods, "\n\n")},
	}
	ifaceEdits, ifacePath, err := s.interfaceEdit(path, adder)
	if err != nil {
		return nil, err
	}
	fes := []edit.FileEdit{{Path: path, Edits: append(edits, adder.Edits()...)}} // after interfaceEdit, which may add to adder
	if ifacePath == path {
		fes[0].Edits = append(fes[0].Edits, ifaceEdits...)
	} else {
		fes = append(fes, edit.FileEdit{Path: ifacePath, Edits: ifaceEdits})
	}
	plan := &refactor.Plan{
		Summary: fmt.Sprintf("turned the type switch of %s into method %s of %s and of interface %s", locate.FuncName(s.fd), s.method, strings.Join(typeNames, ", "), s.iface.Obj().Name()),
		Edits:   edit.Merge(fes),
		Notes:   notes,
		Choices: []string{fmt.Sprintf("the methods go after %s; move_declaration moves them", locate.FuncName(s.fd))},
	}
	if analysis.PublicAPI(s.iface.Obj(), s.WS.Pkgs) {
		plan.Notes = append(plan.Notes, refactor.APINote(s.iface.Obj().Name(), "adding a method to it"))
	}
	return plan, nil
}

// signature is the new method's: its name, the variables the cases use
// as parameters, and the function's results.
func (s *switcher) signature(q types.Qualifier) string {
	var ps, rs []string
	for _, v := range s.params {
		ps = append(ps, v.Name()+" "+types.TypeString(v.Type(), q))
	}
	results := s.Info.Defs[s.fd.Name].(*types.Func).Signature().Results()
	for i := range results.Len() {
		rs = append(rs, types.TypeString(results.At(i).Type(), q))
	}
	sig := s.method + "(" + strings.Join(ps, ", ") + ")"
	switch len(rs) {
	case 0:
		return sig
	case 1:
		return sig + " " + rs[0]
	}
	return sig + " (" + strings.Join(rs, ", ") + ")"
}

// args are the arguments of a call of the new method.
func (s *switcher) args() string {
	var names []string
	for _, v := range s.params {
		names = append(names, v.Name())
	}
	return strings.Join(names, ", ")
}

// indent is the white space before the switch on its line.
func (s *switcher) indent() string {
	tf := s.WS.Fset.File(s.stmt.Pos())
	return leading(string(s.Src[s.Off(tf.LineStart(tf.Line(s.stmt.Pos()))):s.Off(s.stmt.Pos())]) + "x")
}

// body returns the source of a case's body, as whole lines indented by
// indent: from the line after the colon (or the first statement, when it
// is on the colon's line) to the line before the next case or the closing
// brace (or the last statement's end, when that is on their line).
func (s *switcher) body(cc *ast.CaseClause, indent string) string {
	if len(cc.Body) == 0 {
		return ""
	}
	tf := s.WS.Fset.File(cc.Pos())
	end := s.block.Rbrace
	for i, st := range s.block.List {
		if st == cc && i+1 < len(s.block.List) {
			end = s.block.List[i+1].Pos()
		}
	}
	to := max(s.Off(tf.LineStart(tf.Line(end))), s.Off(cc.Body[len(cc.Body)-1].End()))
	from := s.Off(tf.LineStart(tf.Line(cc.Colon) + 1))
	lead := ""
	if tf.Line(cc.Body[0].Pos()) == tf.Line(cc.Colon) {
		from, lead = s.Off(cc.Body[0].Pos()), indent
	}
	text := lead + strings.TrimRight(string(s.Src[from:to]), " \t\n")
	return textutil.Reindent(text, leading(text), indent) + "\n"
}

// leading returns the white space that starts the first line of text
// that is not blank.
func leading(text string) string {
	for _, l := range strings.Split(text, "\n") {
		if t := strings.TrimLeft(l, " \t"); t != "" {
			return l[:len(l)-len(t)]
		}
	}
	return ""
}

// interfaceEdit adds the method to the interface's declaration, in its
// file: the edits, and the file's path.
func (s *switcher) interfaceEdit(path string, adder *imports.Adder) ([]edit.TextEdit, string, error) {
	ipath := s.WS.Fset.Position(s.iface.Obj().Pos()).Filename
	ipkg, ifile := s.WS.FileSyntax(ipath)
	if ifile == nil {
		return nil, "", refactor.Refusef("the declaration of %s was not found", s.iface.Obj().Name())
	}
	var it *ast.InterfaceType
	ast.Inspect(ifile, func(n ast.Node) bool {
		if ts, ok := n.(*ast.TypeSpec); ok && ts.Name.Name == s.iface.Obj().Name() && s.WS.Fset.Position(ts.Name.Pos()) == s.WS.Fset.Position(s.iface.Obj().Pos()) {
			it, _ = ts.Type.(*ast.InterfaceType)
		}
		return it == nil
	})
	if it == nil {
		return nil, "", refactor.Refusef("%s is not declared as an interface literal", s.iface.Obj().Name())
	}
	a := adder
	if ipath != path {
		src, err := os.ReadFile(ipath)
		if err != nil {
			return nil, "", err
		}
		a = imports.New(s.WS, ipkg, ifile, src)
	}
	q := func(p *types.Package) string {
		n, _ := a.Name(p)
		return n
	}
	sig := s.signature(q)
	tf := s.WS.Fset.File(it.Pos())
	off := func(p token.Pos) int { return s.WS.Fset.Position(p).Offset }
	var e edit.TextEdit
	switch {
	case tf.Line(it.Methods.Opening) != tf.Line(it.Methods.Closing):
		at := off(tf.LineStart(tf.Line(it.Methods.Closing)))
		e = edit.TextEdit{Start: at, End: at, NewText: "\t" + sig + "\n"}
	case len(it.Methods.List) > 0:
		at := off(it.Methods.List[len(it.Methods.List)-1].End())
		e = edit.TextEdit{Start: at, End: at, NewText: "; " + sig}
	default:
		at := off(it.Methods.Closing)
		e = edit.TextEdit{Start: at, End: at, NewText: " " + sig + " "}
	}
	es := []edit.TextEdit{e}
	if ipath != path {
		es = append(es, a.Edits()...)
	}
	return es, ipath, nil
}
