package switchmethod

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/site"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// A mover moves a value's case out of every switch prepared with a
// lookup into methods of a new type, and adds the value to the lookup,
// which then runs them.
type mover struct {
	site.Site // the lookup's file
	iface     *types.Named
	table     *types.Var        // the lookup
	decl      *ast.GenDecl      // its declaration
	lit       *ast.CompositeLit // its literal
	value     constant.Value
	key       string // the value as written
	typ       string // the new type
	moves     []move
	sites     map[string]*site.Site // by path
}

// A move is a prepared switch's code for the value, which becomes a
// method of the new type.
type move struct {
	s         *switcher
	call      *ast.CallExpr   // the guard's call of the interface's method
	ifs       *ast.IfStmt     // the guard
	returning bool            // the guard returns what the call does
	clause    *ast.CaseClause // the value's case, or the default when it has none
	rest      []ast.Stmt      // with neither: the code after the switch, which ends the function
}

// deletion deletes the move's case, or, when it is the switch's only
// clause, the switch: the statement after a returning guard, or the
// guard's else that holds it. A switch whose tag calls or receives
// stays, since deleting it would drop that.
func (mv move) deletion() edit.TextEdit {
	s := mv.s
	if len(s.block.List) > 1 || !plain(s.subject) {
		return s.deleteClause(mv.clause)
	}
	if mv.returning {
		return edit.TextEdit{Start: s.Off(s.TF.LineStart(s.TF.Line(s.stmt.Pos()))), End: s.Off(s.TF.LineStart(s.TF.Line(s.stmt.End()) + 1))}
	}
	return edit.TextEdit{Start: s.Off(mv.ifs.Body.End()), End: s.Off(mv.ifs.Else.End())}
}

// planMove moves a's value into a's type: each prepared switch's case
// for it becomes the type's method, and the lookup holds the type for
// the value, so the lookup runs for it what its cases ran.
func planMove(ws *workspace.Workspace, a prepareArgs) (*refactor.Plan, error) {
	for _, n := range []string{a.Interface, a.Lookup, a.Type} {
		if !token.IsIdentifier(n) {
			return nil, refactor.Refusef("move takes interface and lookup, what the switches were prepared with, and type, the new type, each a Go identifier; %q is not one", n)
		}
	}
	m := &mover{key: a.Move, typ: a.Type, sites: map[string]*site.Site{}}
	st, err := m.site(ws, ws.Abs(a.File))
	if err != nil {
		return nil, err
	}
	if err := m.find(st, a); err != nil {
		return nil, err
	}
	if err := m.collect(); err != nil {
		return nil, err
	}
	if err := m.cover(); err != nil {
		return nil, err
	}
	return m.plan()
}

// find finds the interface, the lookup and its literal, and the value,
// and checks that the type's name is free and the lookup lacks the value.
func (m *mover) find(st *site.Site, a prepareArgs) error {
	scope := st.Pkg.Types.Scope()
	tn, _ := scope.Lookup(a.Interface).(*types.TypeName)
	if tn != nil {
		m.iface, _ = tn.Type().(*types.Named)
	}
	if m.iface == nil || !types.IsInterface(m.iface) {
		return refactor.Refusef("%s is not an interface declared in the package of %s", a.Interface, a.File)
	}
	m.table, _ = scope.Lookup(a.Lookup).(*types.Var)
	if m.table == nil {
		return refactor.Refusef("%s is not a variable of the package of %s; prepare the switches with lookup %s first", a.Lookup, a.File, a.Lookup)
	}
	if o := scope.Lookup(a.Type); o != nil {
		return refactor.Refusef("%s is declared at %s; move makes a new type, so give another name", a.Type, st.WS.Position(o.Pos()))
	}
	if types.Universe.Lookup(a.Type) != nil {
		return refactor.Refusef("%s is predeclared; give another name for the type", a.Type)
	}
	tv, err := types.Eval(st.WS.Fset, st.Pkg.Types, token.NoPos, a.Move)
	if err != nil || tv.Value == nil {
		return refactor.Refusef("move is a value as written in the cases (\"fern\", or a constant's name), and %s is not a constant", a.Move)
	}
	m.value = tv.Value
	ts, err := m.site(st.WS, st.WS.Fset.Position(m.table.Pos()).Filename)
	if err != nil {
		return err
	}
	m.Site = *ts
	for _, d := range m.File.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, sp := range gd.Specs {
			vs, ok := sp.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, n := range vs.Names {
				if m.Info.Defs[n] == m.table && i < len(vs.Values) {
					m.decl = gd
					m.lit, _ = ast.Unparen(vs.Values[i]).(*ast.CompositeLit)
				}
			}
		}
	}
	if m.lit == nil {
		return refactor.Refusef("%s is not declared with a map literal, which move adds the value to", a.Lookup)
	}
	for _, e := range m.lit.Elts {
		if kv, ok := e.(*ast.KeyValueExpr); ok && m.is(m.Info, kv.Key) {
			return refactor.Refusef("%s already holds %s", a.Lookup, m.Text(kv.Key))
		}
	}
	return nil
}

// is reports whether e is the constant value being moved.
func (m *mover) is(info *types.Info, e ast.Expr) bool {
	v := info.Types[e].Value
	return v != nil && v.Kind() == m.value.Kind() && constant.Compare(v, token.EQL, m.value)
}

// site returns the site of the file at path, read once.
func (m *mover) site(ws *workspace.Workspace, path string) (*site.Site, error) {
	if s, ok := m.sites[path]; ok {
		return s, nil
	}
	s, err := site.At(ws, path)
	if err != nil {
		return nil, err
	}
	m.sites[path] = s
	return s, nil
}

// collect finds the switches prepared with the lookup, in every file of
// its package, and the code of each for the value.
func (m *mover) collect() error {
	for _, f := range m.Pkg.Syntax {
		st, err := m.site(m.WS, m.WS.Fset.Position(f.Pos()).Filename)
		if err != nil {
			return err
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				b, ok := n.(*ast.BlockStmt)
				if !ok || err != nil {
					return err == nil
				}
				for i, s := range b.List {
					if ifs, ok := s.(*ast.IfStmt); ok && err == nil {
						if mv, ok := m.guarded(st, fd, b, i, ifs); ok {
							if err = m.code(&mv, b, i); err == nil {
								m.moves = append(m.moves, mv)
							}
						}
					}
				}
				return err == nil
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// cover checks that the type will implement the interface: one guard
// calls each of its methods.
func (m *mover) cover() error {
	it := m.iface.Underlying().(*types.Interface)
	by := map[string][]move{}
	for _, mv := range m.moves {
		by[mv.s.method] = append(by[mv.s.method], mv)
	}
	name := m.iface.Obj().Name()
	for i := range it.NumMethods() {
		f := it.Method(i).Name()
		switch ms := by[f]; len(ms) {
		case 0:
			return refactor.Refusef("%s's method %s has no switch prepared with %s, so %s would not implement %s; prepare that switch first (prepare_switch_for_types with lookup %s)", name, f, m.table.Name(), m.typ, name, m.table.Name())
		case 1:
		default:
			return refactor.Refusef("the switches of %s and %s both call %s's method %s; give one its own method first (prepare it again with another method)", locate.FuncName(ms[0].s.fd), locate.FuncName(ms[1].s.fd), name, f)
		}
	}
	return nil
}

// guarded returns the move of a guard that prepare_switch_for_types
// wrote with the lookup, if k, ok := lookup[tag]; ok { return k.M(args) }
// before a switch on tag, or with the switch in its else.
func (m *mover) guarded(st *site.Site, fd *ast.FuncDecl, b *ast.BlockStmt, i int, ifs *ast.IfStmt) (move, bool) {
	as, ok := ifs.Init.(*ast.AssignStmt)
	if !ok || as.Tok != token.DEFINE || len(as.Lhs) != 2 || len(as.Rhs) != 1 || len(ifs.Body.List) != 1 {
		return move{}, false
	}
	ix, ok := as.Rhs[0].(*ast.IndexExpr)
	if !ok {
		return move{}, false
	}
	if id, ok := ast.Unparen(ix.X).(*ast.Ident); !ok || st.Info.Uses[id] != m.table {
		return move{}, false
	}
	var mv move
	switch s := ifs.Body.List[0].(type) {
	case *ast.ReturnStmt:
		if len(s.Results) == 1 {
			mv.call, _ = s.Results[0].(*ast.CallExpr)
		}
		mv.returning = true
	case *ast.ExprStmt:
		mv.call, _ = s.X.(*ast.CallExpr)
	}
	if mv.call == nil {
		return move{}, false
	}
	sel, ok := mv.call.Fun.(*ast.SelectorExpr)
	k, _ := as.Lhs[0].(*ast.Ident)
	if !ok || k == nil {
		return move{}, false
	}
	if x, ok := sel.X.(*ast.Ident); !ok || st.Info.Uses[x] != st.Info.Defs[k] {
		return move{}, false
	}
	var sw *ast.SwitchStmt
	if mv.returning && ifs.Else == nil && i+1 < len(b.List) {
		sw, _ = b.List[i+1].(*ast.SwitchStmt)
	} else if eb, ok := ifs.Else.(*ast.BlockStmt); !mv.returning && ok && len(eb.List) == 1 {
		sw, _ = eb.List[0].(*ast.SwitchStmt)
	}
	if sw == nil || sw.Tag == nil || st.Text(sw.Tag) != st.Text(ix.Index) {
		return move{}, false
	}
	mv.s = &switcher{Site: *st, fd: fd, stmt: sw, block: sw.Body, subject: sw.Tag, method: sel.Sel.Name}
	mv.ifs = ifs
	return mv, true
}

// code finds the move's code for the value: its case, the default, or
// the code after a switch whose cases return; and checks that it can be
// a method's body that takes only the guard's arguments.
func (m *mover) code(mv *move, b *ast.BlockStmt, i int) error {
	s := mv.s
	var deflt *ast.CaseClause
	for _, st := range s.block.List {
		cc := st.(*ast.CaseClause)
		if cc.List == nil {
			deflt = cc
		}
		for _, e := range cc.List {
			if !m.is(s.Info, e) {
				continue
			}
			if len(cc.List) > 1 {
				return refactor.Refusef("the case %s of %s at %s has other values; split it into a case for each first (prepare_switch_for_types splits them)", s.Text(e), locate.FuncName(s.fd), s.at(cc))
			}
			mv.clause = cc
		}
	}
	if mv.clause == nil {
		mv.clause = deflt
	}
	if mv.clause == nil && mv.returning {
		if b != s.fd.Body {
			return refactor.Refusef("the switch of %s at %s has no case for %s and no default, and the code after it does not end the function; add a case for %s first", locate.FuncName(s.fd), s.at(s.stmt), m.key, m.key)
		}
		mv.rest = b.List[i+2:]
	}
	cc := mv.clause
	if cc == nil {
		cc = &ast.CaseClause{Body: mv.rest}
		if len(mv.rest) > 0 {
			cc.Case, cc.Colon = mv.rest[0].Pos(), mv.rest[0].Pos()
		}
	} else if r, err := s.returns(cc); err != nil {
		return err
	} else if r != mv.returning {
		return refactor.Refusef("the case %s of %s at %s returns where its guard does not, or the other way round; every case must return, or none", s.caseName(cc), locate.FuncName(s.fd), s.at(cc))
	}
	if err := s.checkLeaves(cc, false); err != nil {
		return err
	}
	args := map[*types.Var]bool{}
	for _, a := range mv.call.Args {
		id, _ := a.(*ast.Ident)
		var v *types.Var
		if id != nil {
			v, _ = s.Info.Uses[id].(*types.Var)
		}
		if v == nil {
			return refactor.Refusef("the guard of %s at %s passes %s, which is not a variable; prepare the switch again", locate.FuncName(s.fd), s.at(a), s.Text(a))
		}
		args[v] = true
		s.params = append(s.params, v)
	}
	used, err := s.freeVars(kase{clause: cc})
	if err != nil {
		return err
	}
	for _, v := range used {
		if !args[v] {
			return refactor.Refusef("the code of %s for %s uses %s, which its guard does not pass to %s; prepare the switch again", locate.FuncName(s.fd), m.key, v.Name(), s.method)
		}
	}
	return s.checkWrites(kase{clause: cc})
}

// plan deletes each case, declares the type and its methods after the
// lookup, and adds the value to the lookup.
func (m *mover) plan() (*refactor.Plan, error) {
	adder := imports.New(m.WS, m.Pkg, m.File, m.Src)
	q := func(p *types.Package) string {
		n, _ := adder.Name(p)
		return n
	}
	iname := m.iface.Obj().Name()
	text := fmt.Sprintf("\n\n// %s is the %s of %s.\ntype %s struct{}", m.typ, iname, m.key, m.typ)
	var fes []edit.FileEdit
	var names, notes []string
	for i := range len(m.moves) {
		mv := m.moves[i]
		f := m.method(mv.s.method)
		body, err := m.body(mv, adder)
		if err != nil {
			return nil, err
		}
		text += fmt.Sprintf("\n\nfunc (%s) %s {\n%s}", m.typ, m.signature(f, mv, q), body)
		fn := locate.FuncName(mv.s.fd)
		names = append(names, fn)
		switch {
		case mv.clause != nil && mv.clause.List != nil:
			fes = append(fes, edit.FileEdit{Path: m.WS.Fset.Position(mv.s.fd.Pos()).Filename, Edits: []edit.TextEdit{mv.deletion()}})
		case mv.clause != nil:
			notes = append(notes, fmt.Sprintf("%s has no case for %s: %s.%s is its default", fn, m.key, m.typ, f.Name()))
		case mv.rest != nil:
			notes = append(notes, fmt.Sprintf("%s has no case for %s: %s.%s is the code after its switch", fn, m.key, m.typ, f.Name()))
		default:
			notes = append(notes, fmt.Sprintf("%s has no case for %s: %s.%s does nothing, as its switch did", fn, m.key, m.typ, f.Name()))
		}
	}
	at := m.Off(m.decl.End())
	edits := []edit.TextEdit{{Start: at, End: at, NewText: text}, m.entry()}
	fes = append(fes, edit.FileEdit{Path: m.WS.Fset.Position(m.File.Pos()).Filename, Edits: append(edits, adder.Edits()...)})
	return &refactor.Plan{
		Summary: fmt.Sprintf("moved the code of %s for %s into methods of a new type %s, and added %s: %s{} to %s, which runs them now", strings.Join(names, ", "), m.key, m.typ, m.key, m.typ, m.table.Name()),
		Edits:   edit.Merge(fes),
		Notes:   notes,
	}, nil
}

// signature is a method's: the guard's arguments as its parameters, with
// the types of the interface's method, and its results.
func (m *mover) signature(f *types.Func, mv move, q types.Qualifier) string {
	sig := f.Signature()
	var ps, rs []string
	for i, a := range mv.call.Args {
		t := types.TypeString(sig.Params().At(i).Type(), q)
		if sig.Variadic() && i == sig.Params().Len()-1 {
			t = "..." + types.TypeString(sig.Params().At(i).Type().(*types.Slice).Elem(), q)
		}
		ps = append(ps, mv.s.Text(a)+" "+t)
	}
	for i := range sig.Results().Len() {
		rs = append(rs, types.TypeString(sig.Results().At(i).Type(), q))
	}
	return withResults(rs, f.Name()+"("+strings.Join(ps, ", ")+")")
}

// body is a method's body, indented by a tab: the case's or the
// default's, or the code after the switch. The packages it names are
// imported into the lookup's file, by the names it uses.
func (m *mover) body(mv move, adder *imports.Adder) (string, error) {
	s := mv.s
	var stmts []ast.Stmt
	var text string
	switch {
	case mv.clause != nil:
		stmts, text = mv.clause.Body, s.body(mv.clause, "\t")
	case len(mv.rest) > 0:
		stmts = mv.rest
		from := s.Off(s.TF.LineStart(s.TF.Line(stmts[0].Pos())))
		raw := strings.TrimRight(string(s.Src[from:s.Off(stmts[len(stmts)-1].End())]), " \t\n")
		text = textutil.Reindent(raw, leading(raw), "\t") + "\n"
	}
	var err error
	for _, st := range stmts {
		ast.Inspect(st, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok || err != nil {
				return err == nil
			}
			if pn, ok := s.Info.Uses[id].(*types.PkgName); ok {
				name, e := adder.Name(pn.Imported())
				if e != nil {
					err = refactor.Refusef("the code of %s for %s uses package %s, which %s cannot import: %v", locate.FuncName(s.fd), m.key, pn.Imported().Path(), m.WS.Rel(m.TF.Name()), e)
				} else if name != id.Name {
					err = refactor.Refusef("the code of %s for %s names package %s as %s, which %s names %s; rename the import first", locate.FuncName(s.fd), m.key, pn.Imported().Path(), id.Name, m.WS.Rel(m.TF.Name()), name)
				}
			}
			return true
		})
	}
	return text, err
}

// entry adds the value, with the new type's zero value, to the lookup's
// literal.
func (m *mover) entry() edit.TextEdit {
	kv := m.key + ": " + m.typ + "{}"
	l := m.lit
	if len(l.Elts) == 0 {
		return edit.TextEdit{Start: m.Off(l.Lbrace) + 1, End: m.Off(l.Rbrace), NewText: kv}
	}
	last := l.Elts[len(l.Elts)-1]
	if m.TF.Line(last.End()) == m.TF.Line(l.Rbrace) {
		at := m.Off(last.End())
		return edit.TextEdit{Start: at, End: at, NewText: ", " + kv}
	}
	at := m.Off(m.TF.LineStart(m.TF.Line(l.Rbrace)))
	return edit.TextEdit{Start: at, End: at, NewText: textutil.IndentAt(m.Src, m.Off(last.Pos())) + kv + ",\n"}
}

// method is the interface's method of that name.
func (m *mover) method(name string) *types.Func {
	it := m.iface.Underlying().(*types.Interface)
	for i := range it.NumMethods() {
		if it.Method(i).Name() == name {
			return it.Method(i)
		}
	}
	return nil
}

// deleteClause deletes a case clause's lines, up to the next clause or
// the switch's closing brace.
func (s *switcher) deleteClause(cc *ast.CaseClause) edit.TextEdit {
	end := s.block.Rbrace
	for i, st := range s.block.List {
		if st == cc && i+1 < len(s.block.List) {
			end = s.block.List[i+1].Pos()
		}
	}
	return edit.TextEdit{Start: s.Off(s.TF.LineStart(s.TF.Line(cc.Pos()))), End: s.Off(s.TF.LineStart(s.TF.Line(end)))}
}

// plain reports whether evaluating e calls nothing and receives nothing.
func plain(e ast.Expr) bool {
	ok := true
	ast.Inspect(e, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			ok = false
		case *ast.UnaryExpr:
			ok = ok && n.Op != token.ARROW
		}
		return ok
	})
	return ok
}
