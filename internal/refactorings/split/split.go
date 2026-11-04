// Package split implements split: a variable assigned again into two
// variables, a loop doing two independent things into two loops, and a
// function computing in two phases into two, joined by a struct.
package split

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strconv"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings/extractfunc"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings/slidestmt"
	"github.com/tychofreeman/go-refactor-mcp/internal/site"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// Split is the "split" refactoring.
type Split struct{ refactor.Named[args] }

// New returns the split refactoring.
func New() *Split { return &Split{Named: refactor.Called[args]("split")} }

func (*Split) Description() string {
	return `Split code that does two things into two, without changing what it does.
kind variable: a variable given a new value that does not depend on its old one becomes a new variable from there: in n := len(plants); fmt.Println(n); n = 3 * len(plants); fmt.Println(n), new_name waterings at the assignment gives waterings := 3 * len(plants), and the uses after it waterings.
kind loop: a loop doing two independent things becomes two: for _, b := range beds { water += b.Water; plants += len(b.Plants) } at the second statement becomes for _, b := range beds { water += b.Water } then for _, b := range beds { plants += len(b.Plants) }.
kind phase: what the statements before line compute for the rest goes into a new struct type, and the rest becomes the function new_name of it: litres := b.Rows * 2; total := litres * days; return total * rate at the second line, with type Usage and new_name price, gives usage := Usage{litres: litres}; return price(usage, days, rate).
Identify a variable by file, line (the assignment) and name; a loop by file, line (the for) and at (the second loop's first statement); a phase by file and line (its first statement).
Refuses, saying why: an assignment that reads the old value or is in a block inside the variable's; a variable whose address is taken or that a closure may use later; a name that would hide another; goto; loop parts that leave the loop, depend on each other or both have side effects; a loop over a map, channel or function, or whose header the body changes; a second phase that changes what the first computes (split it first).`
}

type args struct {
	Kind    string `json:"kind" desc:"variable, loop or phase."`
	File    string `json:"file" desc:"Path of the file, relative to the module root."`
	Line    int    `json:"line" desc:"variable: the line of the assignment from which the variable is a new one; loop: the line of the for statement; phase: the line of the second phase's first statement."`
	Name    string `json:"name,omitempty" desc:"variable: the variable's name."`
	NewName string `json:"new_name,omitempty" desc:"variable: the new variable's name; phase: the second phase's new function."`
	At      int    `json:"at,omitempty" desc:"loop: the line of the first statement of the body that goes to the second loop."`
	Type    string `json:"type,omitempty" desc:"phase: the new struct type of what the first phase computes."`
}

// Plan checks preconditions and computes the edits.
func (*Split) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	st, err := site.At(ws, path)
	if err != nil {
		return nil, err
	}
	var plan *refactor.Plan
	switch a.Kind {
	case "variable":
		plan, err = splitVariable(st, a)
	case "loop":
		plan, err = splitLoop(st, a)
	default:
		return nil, refactor.Refusef("kind is variable, loop or phase, not %q", a.Kind)
	}
	if err != nil {
		return nil, err
	}
	plan.Edits[0].Path = path
	return plan, nil
}

// Expand makes kind phase two steps: extract_function makes the second
// phase a function, then the parameters it takes from what the first
// phase computed go into a struct (dataStep).
func (*Split) Expand(ws *workspace.Workspace, raw json.RawMessage) ([]refactor.Step, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.Kind != "phase" {
		return nil, nil
	}
	if !token.IsIdentifier(a.NewName) || !token.IsIdentifier(a.Type) {
		return nil, refactor.Refusef("kind phase takes new_name, the second phase's function, and type, the struct of what the first phase computes")
	}
	st, err := site.At(ws, ws.Abs(a.File))
	if err != nil {
		return nil, err
	}
	p, err := phaseAt(st, a.Line)
	if err != nil {
		return nil, err
	}
	extract, err := json.Marshal(map[string]any{"file": a.File, "start_line": a.Line, "end_line": st.TF.Line(p.body.List[len(p.body.List)-1].End()), "name": a.NewName})
	if err != nil {
		return nil, err
	}
	// The second step finds the function by name: its lines are the ones
	// the first step rewrote, which a batch cannot address.
	data, err := json.Marshal(map[string]any{"kind": a.Kind, "file": a.File, "new_name": a.NewName, "type": a.Type})
	if err != nil {
		return nil, err
	}
	return []refactor.Step{{Refactoring: extractfunc.New(), Args: extract}, {Refactoring: dataStep, Args: data}}, nil
}

// dataStep is the second step of kind phase. Its arguments are args
// without the line: the function it changes is found by name.
var dataStep = refactor.Inner{Tool: "split", Schema: refactor.Schema[dataArgs](), Do: planData}

// dataArgs are dataStep's arguments.
type dataArgs struct {
	Kind    string `json:"kind" desc:"phase."`
	File    string `json:"file" desc:"Path of the file, relative to the module root."`
	NewName string `json:"new_name" desc:"The second phase's function."`
	Type    string `json:"type" desc:"The new struct type of what the first phase computes."`
}

// phase is where a function's second phase starts: at statement i of
// the body of decl.
type phase struct {
	decl *ast.FuncDecl
	body *ast.BlockStmt
	i    int
}

// phaseAt finds the second phase that starts on line.
func phaseAt(st *site.Site, line int) (*phase, error) {
	list, i := slidestmt.StmtAt(st.TF, st.File, line)
	if i < 0 {
		return nil, refactor.Refusef("no statement starts on line %d", line)
	}
	path := analysis.PathTo(st.File, list[i])
	var decl *ast.FuncDecl
	if len(path) > 2 {
		decl, _ = path[2].(*ast.FuncDecl)
	}
	if decl == nil || decl.Body != path[1] {
		return nil, refactor.Refusef("line %d is not a statement of the function's body; the second phase starts at one", line)
	}
	if i == 0 {
		return nil, refactor.Refusef("line %d is the function's first statement, so there would be no first phase", line)
	}
	return &phase{decl: decl, body: decl.Body, i: i}, nil
}

// planData plans dataStep, after extract_function made the second phase
// the function a.NewName: its parameters that its one call passes locals
// of the caller (what the first phase computed) become the fields of
// a.Type, a new struct, whose value is declared just before the call and
// passed in their place; the body reads the fields.
func planData(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	st, err := site.At(ws, path)
	if err != nil {
		return nil, err
	}
	if st.Pkg.Types.Scope().Lookup(a.Type) != nil || types.Universe.Lookup(a.Type) != nil {
		return nil, refactor.Refusef("%s is already declared; give type another name", a.Type)
	}
	var callee, caller *ast.FuncDecl
	var call *ast.CallExpr
	var stmt ast.Stmt
	for _, d := range st.File.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		if fd.Name.Name == a.NewName && fd.Recv == nil {
			callee = fd
		}
		for _, s := range fd.Body.List {
			ast.Inspect(s, func(n ast.Node) bool {
				if c, ok := n.(*ast.CallExpr); ok {
					if id, ok := c.Fun.(*ast.Ident); ok && id.Name == a.NewName {
						caller, call, stmt = fd, c, s
					}
				}
				return true
			})
		}
	}
	if callee == nil || call == nil {
		return nil, refactor.Refusef("extract_function did not make a call of %s to split the phases at", a.NewName)
	}
	params := callee.Type.Params.List
	var objs []*types.Var
	for _, field := range params {
		for _, n := range field.Names {
			v, _ := st.Info.Defs[n].(*types.Var)
			objs = append(objs, v)
		}
	}
	scope := st.Info.Scopes[caller.Type]
	bundled := make([]bool, len(objs))
	var fields []*types.Var
	for i, arg := range call.Args {
		id, ok := arg.(*ast.Ident)
		if !ok {
			continue
		}
		if v, ok := st.Info.Uses[id].(*types.Var); ok && v.Parent() == scope && v.Pos() > caller.Body.Pos() {
			bundled[i] = true
			fields = append(fields, objs[i])
		}
	}
	if len(fields) == 0 {
		return nil, refactor.Refusef("the second phase's statements use nothing the ones before compute, so the phases share no data; extract_function them instead")
	}
	f := analysis.AnalyzeFunc(st.Info, callee, ws.Effects())
	for _, v := range fields {
		if len(f.Modified[v]) > 0 {
			return nil, refactor.Refusef("the second phase assigns %s, which the first computes; split %s with kind variable where the second phase assigns it, then split the phase", v.Name(), v.Name())
		}
	}
	named := map[string]bool{}
	for _, fd := range []*ast.FuncDecl{caller, callee} {
		ast.Inspect(fd, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				named[id.Name] = true
			}
			return true
		})
	}
	lower, name := strings.ToLower(a.Type[:1])+a.Type[1:], ""
	for _, alt := range []string{lower, lower + "Data", "data"} {
		if alt != a.Type && !named[alt] {
			name = alt
			break
		}
	}
	if name == "" {
		return nil, refactor.Refusef("the function already uses the names %s, %sData and data; rename one of them first", lower, lower)
	}
	var lits, decls, sig, passed []string
	for i, v := range objs {
		typ, err := typeIn(st, v.Type())
		if err != nil {
			return nil, err
		}
		if !bundled[i] {
			sig = append(sig, v.Name()+" "+typ)
			passed = append(passed, st.Text(call.Args[i]))
			continue
		}
		if len(lits) == 0 {
			sig = append(sig, name+" "+a.Type)
			passed = append(passed, name)
		}
		lits = append(lits, v.Name()+": "+st.Text(call.Args[i]))
		decls = append(decls, "\t"+v.Name()+" "+typ)
	}
	src := st.Src
	at := lineStart(src, st.Off(stmt.Pos()))
	indent := gocode.IndentOf(src, at)
	edits := []edit.TextEdit{
		{Start: at, End: at, NewText: indent + name + " := " + a.Type + "{" + strings.Join(lits, ", ") + "}\n"},
		{Start: st.Off(call.Lparen) + 1, End: st.Off(call.Rparen), NewText: strings.Join(passed, ", ")},
		{Start: st.Off(caller.End()), End: st.Off(caller.End()), NewText: fmt.Sprintf("\n\n// %s holds what %s computes first, for the rest of it.\ntype %s struct {\n%s\n}", a.Type, caller.Name.Name, a.Type, strings.Join(decls, "\n"))},
		{Start: st.Off(callee.Type.Params.Opening) + 1, End: st.Off(callee.Type.Params.Closing), NewText: strings.Join(sig, ", ")},
	}
	ast.Inspect(callee.Body, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if v, ok := st.Info.Uses[id].(*types.Var); ok && slices.Contains(fields, v) {
				edits = append(edits, edit.TextEdit{Start: st.Off(id.Pos()), End: st.Off(id.End()), NewText: name + "." + id.Name})
			}
		}
		return true
	})
	return &refactor.Plan{
		Summary: fmt.Sprintf("declared %s, the struct of what %s computes first (%s), and passed it to %s", a.Type, caller.Name.Name, strings.Join(fieldNames(fields), ", "), a.NewName),
		Edits:   []edit.FileEdit{{Path: path, Edits: edits}},
	}, nil
}

// fieldNames returns the names of vs.
func fieldNames(vs []*types.Var) []string {
	var names []string
	for _, v := range vs {
		names = append(names, v.Name())
	}
	return names
}

// splitVariable plans kind variable: the assignment on a.Line gives
// a.Name a value that does not depend on its old one, and a.NewName, a
// new variable, takes its place from there.
func splitVariable(st *site.Site, a args) (*refactor.Plan, error) {
	if a.Name == "" || a.NewName == "" {
		return nil, refactor.Refusef("kind variable takes name, the variable, and new_name, the name of the new one")
	}
	if !token.IsIdentifier(a.NewName) || a.NewName == "_" || a.NewName == a.Name {
		return nil, refactor.Refusef("%q is not a name the new variable can have", a.NewName)
	}
	list, i := slidestmt.StmtAt(st.TF, st.File, a.Line)
	if i < 0 {
		return nil, refactor.Refusef("no statement starts on line %d", a.Line)
	}
	as, ok := list[i].(*ast.AssignStmt)
	if !ok || as.Tok != token.ASSIGN {
		return nil, refactor.Refusef("line %d is not an assignment %s = ...; split takes the assignment from which %s holds a new value", a.Line, a.Name, a.Name)
	}
	var lhs *ast.Ident
	for _, l := range as.Lhs {
		if id, ok := l.(*ast.Ident); ok && id.Name == a.Name {
			lhs = id
		}
	}
	if lhs == nil {
		return nil, refactor.Refusef("line %d does not assign %s", a.Line, a.Name)
	}
	enclosing := analysis.PathTo(st.File, as)
	fn := analysis.OutermostFunc(enclosing)
	f := analysis.AnalyzeFunc(st.Info, fn, st.WS.Effects())
	v, _ := st.Info.Uses[lhs].(*types.Var)
	block := scopeOf(st.Info, enclosing)
	switch {
	case v == nil || !f.IsLocal(v):
		return nil, refactor.Refusef("%s is not a local variable", a.Name)
	case v.Parent() != block:
		return nil, refactor.Refusef("line %d is in a block inside the one that declares %s, so the value it gives can reach code after that block; split takes an assignment in the block that declares the variable", a.Line, a.Name)
	case f.Addressed[v]:
		return nil, refactor.Refusef("the address of %s is taken, so code holding it would see the old variable", a.Name)
	case f.Owns(v) != analysis.ThisCode:
		return nil, refactor.Refusef("a closure that may run later uses %s, so it would see the old variable", a.Name)
	case hasGoto(fn):
		return nil, refactor.Refusef("the function uses goto, which could run the code before line %d again after it", a.Line)
	case block.Lookup(a.NewName) != nil:
		return nil, refactor.Refusef("%s is already declared in this block", a.NewName)
	}
	for _, e := range append(append([]ast.Expr{}, as.Rhs...), as.Lhs...) {
		if e != lhs && refersTo(st.Info, e, v) {
			return nil, refactor.Refusef("the assignment on line %d reads %s, so the new value depends on the old one", a.Line, a.Name)
		}
	}
	writes := plainWrites(fn)
	var readBefore, readAfter bool
	var uses []*ast.Ident
	var hidden *ast.Ident
	for _, s := range list {
		ast.Inspect(s, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			switch {
			case !ok:
			case id.Name == a.NewName && id.Pos() >= as.Pos():
				if hidden == nil {
					hidden = id
				}
			case st.Info.Uses[id] != v || id == lhs:
			case id.Pos() < as.Pos():
				readBefore = readBefore || !writes[id]
			default:
				readAfter = readAfter || !writes[id]
				uses = append(uses, id)
			}
			return true
		})
	}
	switch {
	case hidden != nil:
		return nil, refactor.Refusef("line %d already uses %s, which the new variable would hide there", st.TF.Line(hidden.Pos()), a.NewName)
	case !readBefore:
		return nil, refactor.Refusef("nothing reads %s before line %d, so its earlier value is not used; remove the earlier assignment rather than split", a.Name, a.Line)
	case !readAfter:
		return nil, refactor.Refusef("nothing reads %s after line %d, so the new variable would not be used; remove the assignment rather than split", a.Name, a.Line)
	}
	edits, err := declare(st, as, lhs, v, a.NewName)
	if err != nil {
		return nil, err
	}
	for _, id := range uses {
		edits = append(edits, edit.TextEdit{Start: st.Off(id.Pos()), End: st.Off(id.End()), NewText: a.NewName})
	}
	return &refactor.Plan{
		Summary: fmt.Sprintf("split %s at line %d: from there it is %s (%d use%s)", a.Name, a.Line, a.NewName, len(uses), textutil.Plural(len(uses))),
		Edits:   []edit.FileEdit{{Edits: edits}},
	}, nil
}

// declare returns the edits that make as declare name in place of
// assigning lhs, the variable v: name := e when e has v's type, var name
// T = e when it does not (an untyped constant, a concrete type for an
// interface), and var name T before an assignment of several values.
func declare(st *site.Site, as *ast.AssignStmt, lhs *ast.Ident, v *types.Var, name string) ([]edit.TextEdit, error) {
	rename := edit.TextEdit{Start: st.Off(lhs.Pos()), End: st.Off(lhs.End()), NewText: name}
	if len(as.Lhs) == 1 && len(as.Rhs) == 1 {
		if declaredAs(st.WS.Fset, st.Pkg.Types, st.Info, as.Rhs[0], v.Type()) {
			tok := st.Off(as.TokPos)
			return []edit.TextEdit{rename, {Start: tok, End: tok + 1, NewText: ":="}}, nil
		}
	}
	typ, err := typeIn(st, v.Type())
	if err != nil {
		return nil, err
	}
	if len(as.Lhs) == 1 {
		rename.NewText = "var " + name + " " + typ
		return []edit.TextEdit{rename}, nil
	}
	start := st.Off(as.Pos())
	indent := gocode.IndentOf(st.Src, lineStart(st.Src, start))
	return []edit.TextEdit{{Start: start, End: start, NewText: "var " + name + " " + typ + "\n" + indent}, rename}, nil
}

// splitLoop plans kind loop: the statements of the loop on a.Line from
// line a.At on go to a second loop over the same values, after it.
func splitLoop(st *site.Site, a args) (*refactor.Plan, error) {
	list, i := slidestmt.StmtAt(st.TF, st.File, a.Line)
	if i < 0 {
		return nil, refactor.Refusef("no statement starts on line %d", a.Line)
	}
	loop := list[i]
	var body *ast.BlockStmt
	switch l := loop.(type) {
	case *ast.ForStmt:
		body = l.Body
	case *ast.RangeStmt:
		body = l.Body
	case *ast.LabeledStmt:
		return nil, refactor.Refusef("the loop on line %d is labeled; split takes a loop without a label", a.Line)
	default:
		return nil, refactor.Refusef("line %d does not start a for loop", a.Line)
	}
	k := -1
	var lines []string
	for j, s := range body.List {
		l := st.TF.Line(s.Pos())
		if l == a.At {
			k = j
		}
		lines = append(lines, strconv.Itoa(l))
	}
	switch {
	case k < 0:
		return nil, refactor.Refusef("no statement of the loop's body starts on line %d; at is one of lines %s", a.At, strings.Join(lines, ", "))
	case k == 0:
		return nil, refactor.Refusef("line %d is the first statement of the loop, so nothing would stay in the first loop; at is the line of a later one", a.At)
	}
	effects := st.WS.Effects()
	f := analysis.AnalyzeFunc(st.Info, analysis.OutermostFunc(analysis.PathTo(st.File, loop)), effects)
	for _, s := range body.List {
		if how, _ := slidestmt.Leaves(s); how != "" {
			return nil, refactor.Refusef("line %d %s, so after the split the rest of the body would run when it did not, or the reverse", st.TF.Line(s.Pos()), how)
		}
	}
	if err := checkHeader(st, f, effects, loop, body); err != nil {
		return nil, err
	}
	for _, x := range body.List[:k] {
		for _, y := range body.List[k:] {
			if err := slidestmt.Independent(f, st.TF, x, y, effects); err != nil {
				return nil, refactor.Refusef("the second loop's statements would run after every iteration of the first's, so they must not depend on each other: %v", err)
			}
		}
	}
	src := st.Src
	cut, _, err := slidestmt.Span(st.TF, st.File, src, body.List[k])
	if err != nil {
		return nil, err
	}
	start := st.Off(loop.Pos())
	indent := gocode.IndentOf(src, lineStart(src, start))
	lb, rb := st.Off(body.Lbrace), st.Off(body.Rbrace)
	first := strings.TrimRight(string(src[lb+1:cut]), " \t\n") + "\n"
	text := header(st, loop, body.List[:k]) + "{" + first + indent + "}\n" +
		indent + header(st, loop, body.List[k:]) + "{\n" + string(src[cut:rb+1])
	return &refactor.Plan{
		Summary: fmt.Sprintf("split the loop on line %d in two: the statements from line %d on go to a second loop after it", a.Line, a.At),
		Edits:   []edit.FileEdit{{Edits: []edit.TextEdit{{Start: start, End: rb + 1, NewText: text}}}},
	}, nil
}

// checkHeader refuses a loop whose header would not do the same when
// evaluated again for a second loop after the first: one over a map, a
// channel or a function, one that assigns variables declared outside it,
// or one whose expressions the body could change or that have effects.
func checkHeader(st *site.Site, f *analysis.Func, effects *analysis.Effects, loop ast.Stmt, body *ast.BlockStmt) error {
	var evaluated []ast.Expr
	switch l := loop.(type) {
	case *ast.RangeStmt:
		switch st.Info.TypeOf(l.X).Underlying().(type) {
		case *types.Map:
			return refactor.Refusef("the loop ranges over a map, whose order can differ from one loop to the next")
		case *types.Chan:
			return refactor.Refusef("the loop ranges over a channel, which the first loop would drain")
		case *types.Signature:
			return refactor.Refusef("the loop ranges over a function, which would run twice")
		}
		if l.Tok == token.ASSIGN {
			return refactor.Refusef("the loop assigns variables declared outside it, which the second loop would assign again")
		}
		evaluated = append(evaluated, l.X)
	case *ast.ForStmt:
		declared := map[types.Object]bool{}
		switch init := l.Init.(type) {
		case nil:
		case *ast.AssignStmt:
			for _, e := range init.Lhs {
				if id, ok := e.(*ast.Ident); ok && st.Info.Defs[id] != nil {
					declared[st.Info.Defs[id]] = true
				}
			}
			evaluated = append(evaluated, init.Rhs...)
		default:
			return refactor.Refusef("the loop's init statement %s does not declare its variables", st.Text(l.Init))
		}
		if l.Post != nil {
			var written []ast.Expr
			switch post := l.Post.(type) {
			case *ast.IncDecStmt:
				written = []ast.Expr{post.X}
			case *ast.AssignStmt:
				written = post.Lhs
				evaluated = append(evaluated, post.Rhs...)
			}
			for _, w := range written {
				if id, ok := ast.Unparen(w).(*ast.Ident); !ok || !declared[st.Info.Uses[id]] {
					return refactor.Refusef("%s changes a variable the loop does not declare, so the second loop would start where the first ended", st.Text(l.Post))
				}
			}
		}
		for o := range declared {
			v, _ := o.(*types.Var)
			for _, n := range f.Modified[v] {
				if n.Pos() >= body.Pos() && n.End() <= body.End() {
					return refactor.Refusef("line %d changes %s, which the loop's header uses", st.TF.Line(n.Pos()), v.Name())
				}
			}
		}
		if l.Cond != nil {
			evaluated = append(evaluated, l.Cond)
		}
	}
	past := make([]ast.Node, len(body.List))
	for i, s := range body.List {
		past[i] = s
	}
	for _, e := range evaluated {
		m := analysis.Move{Effects: effects, Func: f, Expr: e, Past: past, Times: analysis.Several}
		if obs := m.Check(); obs != nil {
			return refactor.Refusef("the second loop would evaluate %s again, after the first loop's body ran: %s", st.Text(e), obs.Why)
		}
	}
	return nil
}

// header returns the text of loop's header, from for to its body's brace,
// for a loop whose body is part: a range variable part does not use
// becomes _ or is left out.
func header(st *site.Site, loop ast.Stmt, part []ast.Stmt) string {
	r, ok := loop.(*ast.RangeStmt)
	if !ok {
		return string(st.Src[st.Off(loop.Pos()):st.Off(loop.(*ast.ForStmt).Body.Lbrace)])
	}
	key, val := usedIn(st.Info, r.Key, part), usedIn(st.Info, r.Value, part)
	x := st.Text(r.X)
	switch {
	case val != "":
		if key == "" {
			key = "_"
		}
		return "for " + key + ", " + val + " := range " + x + " "
	case key != "":
		return "for " + key + " := range " + x + " "
	}
	return "for range " + x + " "
}

// usedIn returns the name of the variable e declares if part uses it,
// or "".
func usedIn(info *types.Info, e ast.Expr, part []ast.Stmt) string {
	id, ok := e.(*ast.Ident)
	if !ok || id.Name == "_" {
		return ""
	}
	o := info.Defs[id]
	for _, s := range part {
		if refersTo(info, s, o) {
			return id.Name
		}
	}
	return ""
}

// refersTo reports whether an identifier in n refers to o.
func refersTo(info *types.Info, n ast.Node, o types.Object) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && info.Uses[id] == o {
			found = true
		}
		return !found
	})
	return found
}

// plainWrites returns the identifiers in n that an assignment assigns
// as a whole (x = ..., x := ...), which do not read them.
func plainWrites(n ast.Node) map[*ast.Ident]bool {
	writes := map[*ast.Ident]bool{}
	ast.Inspect(n, func(n ast.Node) bool {
		if as, ok := n.(*ast.AssignStmt); ok && (as.Tok == token.ASSIGN || as.Tok == token.DEFINE) {
			for _, l := range as.Lhs {
				if id, ok := l.(*ast.Ident); ok {
					writes[id] = true
				}
			}
		}
		return true
	})
	return writes
}

// hasGoto reports whether n has a goto statement.
func hasGoto(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		if b, ok := n.(*ast.BranchStmt); ok && b.Tok == token.GOTO {
			found = true
		}
		return !found
	})
	return found
}

// declaredAs reports whether name := e declares a variable of type t: e
// has type t, or is a constant whose type, checked on its own, defaults
// to t (an untyped 2 is an int there, though it is a float64 in rate = 2).
func declaredAs(fset *token.FileSet, pkg *types.Package, info *types.Info, e ast.Expr, t types.Type) bool {
	tv := info.Types[e]
	if tv.Value == nil {
		return tv.Type != nil && types.Identical(tv.Type, t)
	}
	alone := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
	if err := types.CheckExpr(fset, pkg, e.Pos(), e, alone); err != nil {
		return false
	}
	return types.Identical(types.Default(alone.Types[e].Type), t)
}

// typeIn spells t as st's file must, refusing a type from a package the
// file does not import.
func typeIn(st *site.Site, t types.Type) (string, error) {
	imports := map[string]*types.Package{}
	for path, p := range st.Pkg.Imports {
		imports[path] = p.Types
	}
	s, missing := analysis.TypeString(t, st.File, st.Pkg.Types, imports)
	if missing != "" {
		return "", refactor.Refusef("the variable's type needs package %s, which the file does not import", missing)
	}
	return s, nil
}

// lineStart returns the offset of the start of the line off is on.
func lineStart(src []byte, off int) int {
	for off > 0 && src[off-1] != '\n' {
		off--
	}
	return off
}

// scopeOf returns the scope of path[1], the block whose statement is
// path[0]: a function's body has its signature's scope.
func scopeOf(info *types.Info, path []ast.Node) *types.Scope {
	if len(path) > 2 {
		switch fn := path[2].(type) {
		case *ast.FuncDecl:
			if fn.Body == path[1] {
				return info.Scopes[fn.Type]
			}
		case *ast.FuncLit:
			if fn.Body == path[1] {
				return info.Scopes[fn.Type]
			}
		}
	}
	return info.Scopes[path[1]]
}
