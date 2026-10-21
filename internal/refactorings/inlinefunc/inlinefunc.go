// Package inlinefunc implements "inline function": replace a call with the
// body of the function it calls, and delete the function when no call to it
// is left. It is the inverse of extract_function.
//
// The call can be a statement (f(a)), the right side of an assignment or
// declaration (x, err := f(a)), a return (return f(a)), or part of an
// expression when the body is a single return statement. Parameters are
// replaced by their arguments where that cannot change what the body sees,
// and otherwise declared at the top of the inlined code with the
// arguments' values, which keeps the evaluation order.
package inlinefunc

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// InlineFunction is the "inline_function" refactoring.
type InlineFunction struct{}

// New returns the inline_function refactoring.
func New() *InlineFunction { return &InlineFunction{} }

func (*InlineFunction) Name() string { return "inline_function" }

func (*InlineFunction) Description() string {
	return `Replace a call to a function or method with the function's body, and delete the function if this was its last use. The inverse of extract_function.
Use it to undo an extraction that did not help, or to remove a small helper whose name adds nothing.
Identify the call by file, line and the called function's name (pass column if the name appears more than once on the line).
The call can be a statement (f(a)), the whole right side of an assignment or declaration (x, err := f(a); var x = f(a)), a return (return f(a)), or part of an expression if the function's body is a single return statement. Parameters are replaced by the arguments where that is safe, and otherwise declared at the top of the inlined code (p := arg).
Refuses if the function is generic or variadic, comes from another package, is called through an interface or a function value, uses defer or recover, has named results, or returns early (except when the call is "return f(a)"); if a name the body uses would mean something else at the call; or if a name the inlined code declares clashes with one in the calling block (rename one first).
The function is deleted only if nothing else refers to it; an exported one with a note that the module's API changed. Pass keep_function to keep it anyway.
With all_calls, every call to the function in the module is inlined, one after another, and then the function is deleted (unless keep_function); give file and name (Name or Type.Method), or the line of any call. If one call cannot be inlined, none is.`
}

func (*InlineFunction) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":          {"type": "string",  "description": "Path of the file with the call, relative to the module root."},
    "line":          {"type": "integer", "description": "1-based line of the call. With all_calls, optional: the line of any call or of the declaration."},
    "name":          {"type": "string",  "description": "Name of the called function or method (for a method call x.m(), m)."},
    "column":        {"type": "integer", "description": "Optional 1-based column of the name, only needed if it appears more than once on the line."},
    "keep_function": {"type": "boolean", "description": "Keep the function even if no other call to it is left. Default false."},
    "all_calls":     {"type": "boolean", "description": "Inline every call to the function in the module, then delete it (unless keep_function). Without line, name is the function (Name or Type.Method) of file's package."}
  },
  "required": ["file", "name"],
  "additionalProperties": false
}`)
}

type args struct {
	File         string `json:"file"`
	Line         int    `json:"line"`
	Column       int    `json:"column,omitempty"`
	Name         string `json:"name"`
	KeepFunction bool   `json:"keep_function,omitempty"`
	AllCalls     bool   `json:"all_calls,omitempty"`
}

// shape is how the call is used, which decides what replaces it.
type shape int

const (
	stmtCall shape = iota // f(a) as a statement
	assign                // x, err := f(a); x = f(a); var x = f(a)
	terminal              // return f(a), or f(a) where the caller returns right after
	expr                  // f(a) inside an expression; the body is "return e"
)

type inliner struct {
	ws   *workspace.Workspace
	pkg  *packages.Package
	info *types.Info
	keep bool

	// The call and its file.
	file     *ast.File
	tf       *token.File
	src      []byte
	id       *ast.Ident // the function's name at the call
	call     *ast.CallExpr
	path     []ast.Node // from the call outwards
	callerFn ast.Node   // innermost FuncDecl or FuncLit around the call
	callerFA *analysis.Func

	// The called function and its file.
	fn       *types.Func
	sig      *types.Signature
	decl     *ast.FuncDecl
	dfile    *ast.File
	dtf      *token.File
	dsrc     []byte
	calleeFA *analysis.Func
	fnScope  *types.Scope // the function's scope: parameters and top-level body declarations
	returns  []*ast.ReturnStmt
	labels   map[string]bool
	hasGoto  bool

	// What replaces the call.
	shape      shape
	stmt       ast.Stmt     // the statement replaced (all shapes but expr) or containing the call
	block      ast.Node     // the block or clause holding stmt
	blockScope *types.Scope // its scope
	ret        *ast.ReturnStmt
	results    []ast.Expr // ret's results, for assign and stmtCall

	params  []*param
	merged  map[*types.Var]*ast.Ident // body locals that take the place of a result variable
	dropped map[int]bool              // result positions that need no assignment
	imp     *imports.Adder
	usedQ   map[string]bool // qualifiers the inlined text uses in the caller's file
	notes   []string

	// Edits that go with merging into an existing variable: in the body
	// (a dropped "var v T", ":=" becoming "=") and in the caller (the
	// variable's own "var o T", when the body's declaration replaces it).
	bodyEdits, callerEdits []edit.TextEdit
}

// Plan checks preconditions and computes the edits.
func (*InlineFunction) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	pkg, id, err := locate.Ident(ws, locate.IdentQuery{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name})
	if err != nil {
		return nil, err
	}
	path := ws.Fset.Position(id.Pos()).Filename
	_, file := ws.FileSyntax(path)
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	x := &inliner{ws: ws, pkg: pkg, info: pkg.TypesInfo, keep: a.KeepFunction, file: file, tf: ws.Fset.File(file.Pos()), src: src, id: id,
		merged: map[*types.Var]*ast.Ident{}, dropped: map[int]bool{}, usedQ: map[string]bool{}}
	x.imp = imports.New(ws, pkg, file, src)
	return x.plan()
}

// Expand returns, with all_calls, one step per call to the function: all
// but the last keep it, so the last deletes it unless keep_function.
func (r *InlineFunction) Expand(ws *workspace.Workspace, raw json.RawMessage) ([]refactor.Step, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if !a.AllCalls {
		return nil, nil
	}
	pkg, id, err := locate.Ident(ws, locate.IdentQuery{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name})
	if err != nil {
		return nil, err
	}
	fn, ok := pkg.TypesInfo.Defs[id].(*types.Func)
	if !ok {
		fn, ok = pkg.TypesInfo.Uses[id].(*types.Func)
	}
	if !ok {
		return nil, refactor.Refusef("%q at %s is not a function or method", id.Name, ws.Position(id.Pos()))
	}
	calls, decl := callsOf(ws, fn)
	if len(calls) == 0 {
		return nil, refactor.Refusef("nothing calls %s, so there is no call to inline", fn.Name())
	}
	for _, c := range calls {
		if c.Filename == decl.Filename && decl.Offset <= c.Offset && c.Offset < decl.end {
			return nil, refactor.Refusef("%s calls itself (at %s:%d), so its calls cannot all be inlined; inline the calls outside it one at a time", fn.Name(), ws.Rel(c.Filename), c.Line)
		}
	}
	var steps []refactor.Step
	for i, c := range calls {
		b, err := json.Marshal(args{File: ws.Rel(c.Filename), Line: c.Line, Column: c.Column, Name: fn.Name(), KeepFunction: a.KeepFunction || i < len(calls)-1})
		if err != nil {
			return nil, err
		}
		steps = append(steps, refactor.Step{Refactoring: r, Args: b})
	}
	return steps, nil
}

// declSpan is where a function's declaration is: its file, and the byte
// range from Offset to end.
type declSpan struct {
	token.Position
	end int
}

// callsOf returns the calls to fn in the workspace's packages, by file
// and from the end of each file back, so that a call nested in another's
// arguments comes first; and where fn is declared.
func callsOf(ws *workspace.Workspace, fn *types.Func) ([]token.Position, declSpan) {
	key := ws.Fset.Position(fn.Pos())
	same := func(p token.Pos) bool {
		q := ws.Fset.Position(p)
		return q.Filename == key.Filename && q.Offset == key.Offset
	}
	var decl declSpan
	seen := map[token.Position]bool{}
	var calls []token.Position
	for _, p := range ws.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, f := range p.Syntax {
			ast.Inspect(f, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.FuncDecl:
					if same(n.Name.Pos()) {
						decl = declSpan{ws.Fset.Position(n.Pos()), ws.Fset.Position(n.End()).Offset}
					}
				case *ast.CallExpr:
					id, ok := n.Fun.(*ast.Ident)
					if sel, isSel := n.Fun.(*ast.SelectorExpr); isSel {
						id, ok = sel.Sel, true
					}
					if !ok {
						return true
					}
					if o := p.TypesInfo.Uses[id]; o != nil && same(o.Pos()) {
						if pos := ws.Fset.Position(id.Pos()); !seen[pos] {
							seen[pos] = true
							calls = append(calls, pos)
						}
					}
				}
				return true
			})
		}
	}
	sort.Slice(calls, func(i, j int) bool {
		if calls[i].Filename != calls[j].Filename {
			return calls[i].Filename < calls[j].Filename
		}
		return calls[i].Offset > calls[j].Offset
	})
	return calls, decl
}

func (x *inliner) plan() (*refactor.Plan, error) {
	steps := []func() error{
		x.findCall, x.findCallee, x.scanBody, x.decideShape, x.decideMerges,
		x.decideParams, x.checkFreeNames, x.checkDeclaredNames,
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return nil, err
		}
	}
	callerEdits, err := x.replacement()
	if err != nil {
		return nil, err
	}
	callerEdits = append(callerEdits, x.callerEdits...)
	callerEdits = append(callerEdits, x.imp.Edits()...)
	fes := []edit.FileEdit{{Path: x.tf.Name(), Edits: callerEdits}}

	deleted, why := x.deletable()
	if deleted {
		fes = append(fes, edit.FileEdit{Path: x.dtf.Name(), Edits: x.deletion()})
	}
	fes = edit.Merge(append(fes, x.importEdits(deleted)...))

	what := "function"
	if x.sig.Recv() != nil {
		what = "method"
	}
	summary := fmt.Sprintf("inlined the call to %s %q at %s", what, x.fn.Name(), x.ws.Position(x.call.Pos()))
	if deleted {
		summary += fmt.Sprintf(" and deleted the %s, which has no other uses", what)
	}
	notes := x.notes
	var bound []string
	for _, p := range x.params {
		if p.bind && p.name != "_" && len(p.uses) > 0 {
			bound = append(bound, p.name)
		}
	}
	if len(bound) > 0 {
		notes = append(notes, fmt.Sprintf("kept parameter%s %s as variable%s declared at the top of the inlined code, because using the argument directly could change what the body sees; rename if the name does not fit the caller",
			textutil.Plural(len(bound)), strings.Join(bound, ", "), textutil.Plural(len(bound))))
	}
	if deleted && x.decl.Doc != nil {
		doc := strings.TrimSpace(x.decl.Doc.Text())
		if i := strings.IndexByte(doc, '\n'); i >= 0 {
			doc = doc[:i] + " ..."
		}
		notes = append(notes, fmt.Sprintf("the deleted function's doc comment (%q) went with it; add a comment at the call if it explained something", doc))
	}
	if !deleted && why != "" {
		notes = append(notes, fmt.Sprintf("kept %s: %s", x.fn.Name(), why))
	}
	if deleted && analysis.PublicAPI(x.fn, x.ws.Pkgs) {
		notes = append(notes, refactor.APINote(x.fn.Name(), "deleting it"))
	}
	if added := x.imp.Paths(); len(added) > 0 {
		notes = append(notes, "added import "+strings.Join(quoteAll(added), ", ")+" for the inlined code")
	}
	return &refactor.Plan{Summary: summary, Edits: fes, Notes: notes}, nil
}

// ---------------------------------------------------------------------------
// The call and the callee

func (x *inliner) findCall() error {
	name := x.id.Name
	if o, ok := x.info.Defs[x.id].(*types.Func); ok {
		return refactor.Refusef("%s is the declaration of %q; give the line of a call to it, or all_calls to inline every call", x.ws.Position(x.id.Pos()), o.Name())
	}
	fn, ok := x.info.Uses[x.id].(*types.Func)
	if !ok {
		return refactor.Refusef("%q at %s is not a function or method", name, x.ws.Position(x.id.Pos()))
	}
	x.fn = fn
	path := analysis.PathTo(x.file, x.id)
	i := 1
	if sel, ok := path[1].(*ast.SelectorExpr); ok && sel.Sel == x.id {
		i = 2
	}
	switch ix := path[i].(type) {
	case *ast.IndexExpr, *ast.IndexListExpr: // f[T](...)
		if c, ok := path[i+1].(*ast.CallExpr); ok && c.Fun == ix {
			return refactor.Refusef("%q is generic; inlining generic functions is not supported yet", name)
		}
	}
	call, ok := path[i].(*ast.CallExpr)
	if !ok || call.Fun != path[i-1] {
		return refactor.Refusef("%q at %s is not called there (it is used as a value); only a call can be inlined", name, x.ws.Position(x.id.Pos()))
	}
	x.call, x.path = call, path[i:]
	for _, n := range x.path {
		if f, ok := n.(*ast.FuncLit); ok {
			x.callerFn = f
			break
		}
		if f, ok := n.(*ast.FuncDecl); ok {
			x.callerFn = f
			break
		}
	}
	if x.callerFn == nil {
		return refactor.Refusef("the call to %q at %s is not inside a function (it initializes a package-level variable); only calls in function bodies can be inlined", name, x.ws.Position(call.Pos()))
	}
	x.callerFA = analysis.AnalyzeFunc(x.info, analysis.OutermostFunc(path))
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		s := x.info.Selections[sel]
		switch {
		case s == nil: // pkg.F
		case s.Kind() == types.MethodExpr:
			return refactor.Refusef("%s is a method expression (%s); only ordinary calls x.%s(...) can be inlined", x.quote(sel), x.quote(sel), name)
		case types.IsInterface(s.Recv()):
			return refactor.Refusef("%s calls the interface method %q; which method runs is decided at run time, so there is no body to inline", x.quote(call.Fun), name)
		case len(s.Index()) > 1:
			return refactor.Refusef("%q is promoted from an embedded field of %s; inlining promoted methods is not supported yet", name, x.quote(sel.X))
		}
	}
	return nil
}

func (x *inliner) findCallee() error {
	name := x.fn.Name()
	if x.fn.Pkg() != x.pkg.Types {
		pkgName := "the universe"
		if x.fn.Pkg() != nil {
			pkgName = "package " + x.fn.Pkg().Path()
		}
		return refactor.Refusef("%q is declared in %s; only functions of the calling package (%s) can be inlined for now", name, pkgName, x.pkg.PkgPath)
	}
	x.sig = x.fn.Signature()
	if x.sig.TypeParams().Len() > 0 || x.sig.RecvTypeParams().Len() > 0 {
		return refactor.Refusef("%q is generic; inlining generic functions is not supported yet", name)
	}
	if x.sig.Variadic() {
		return refactor.Refusef("%q is variadic; inlining variadic functions is not supported yet", name)
	}
	for _, f := range x.pkg.Syntax {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && x.info.Defs[fd.Name] == x.fn {
				x.decl, x.dfile = fd, f
			}
		}
	}
	if x.decl == nil {
		return refactor.Refusef("the declaration of %q is not in the loaded source", name)
	}
	if x.decl.Body == nil {
		return refactor.Refusef("%q has no Go body (it is implemented in assembly or linked in)", name)
	}
	if res := x.decl.Type.Results; res != nil {
		for _, f := range res.List {
			if len(f.Names) > 0 {
				return refactor.Refusef("%q has named results; inlining functions with named results is not supported yet", name)
			}
		}
	}
	x.dtf = x.ws.Fset.File(x.dfile.Pos())
	if x.dtf.Name() == x.tf.Name() {
		x.dsrc = x.src
	} else {
		src, err := os.ReadFile(x.dtf.Name())
		if err != nil {
			return err
		}
		x.dsrc = src
	}
	x.calleeFA = analysis.AnalyzeFunc(x.info, x.decl)
	x.fnScope = x.info.Scopes[x.decl.Type]
	return nil
}

// scanBody refuses what cannot move out of the callee's frame and records
// its returns and labels.
func (x *inliner) scanBody() error {
	name := x.fn.Name()
	x.labels = map[string]bool{}
	var err error
	ast.Inspect(x.decl.Body, func(n ast.Node) bool {
		if err != nil {
			return false
		}
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.DeferStmt:
			err = refactor.Refusef("%q uses defer (at %s), which runs when %q returns; inlined, it would run when the caller returns", name, x.ws.Position(n.Pos()), name)
		case *ast.CallExpr:
			if b, ok := x.info.Uses[analysis.IdentOf(n.Fun)].(*types.Builtin); ok && b.Name() == "recover" {
				err = refactor.Refusef("%q calls recover (at %s), whose meaning depends on the function that calls it", name, x.ws.Position(n.Pos()))
			}
		case *ast.ReturnStmt:
			x.returns = append(x.returns, n)
		case *ast.LabeledStmt:
			x.labels[n.Label.Name] = true
		case *ast.BranchStmt:
			if n.Tok == token.GOTO {
				x.hasGoto = true
			}
		}
		return true
	})
	if err != nil {
		return err
	}
	if len(x.labels) > 0 {
		var clash string
		ast.Inspect(x.callerFn, func(n ast.Node) bool {
			if _, ok := n.(*ast.FuncLit); ok && n != x.callerFn {
				return false
			}
			if l, ok := n.(*ast.LabeledStmt); ok && x.labels[l.Label.Name] && clash == "" {
				clash = l.Label.Name
			}
			return true
		})
		if clash != "" {
			return refactor.Refusef("%q and the calling function both use the label %s; rename one first", name, clash)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// How the call is used

func (x *inliner) decideShape() error {
	name := x.fn.Name()
	nres := x.sig.Results().Len()
	switch p := x.path[1].(type) {
	case *ast.GoStmt:
		return refactor.Refusef("the call to %q is in a go statement; inlining it would run the body in the calling goroutine", name)
	case *ast.DeferStmt:
		return refactor.Refusef("the call to %q is deferred; inlining it would run the body now instead of when the caller returns", name)
	case *ast.ExprStmt:
		x.shape, x.stmt = stmtCall, p
		if nres == 0 && x.returnsRightAfter(p) {
			x.shape = terminal
		}
	case *ast.AssignStmt:
		if len(p.Rhs) == 1 && (len(p.Lhs) == nres || (nres == 1 && len(p.Lhs) == 1)) {
			// Go evaluates the left side's operands before the call; in place
			// of a single return, the call's expression keeps that order.
			if bad := x.lhsEvaluatedFirst(p); bad != nil {
				if x.exprBody() == nil {
					return refactor.Refusef("`%s` on the left of the assignment is evaluated before the call to %q, and inlining would run the body first; assign the call to a variable first", x.text(bad), name)
				}
				break // inline the expression in place
			}
			x.shape, x.stmt = assign, p
		}
	case *ast.ValueSpec:
		if len(p.Values) == 1 && len(p.Names) == nres {
			if gd, ok := x.path[2].(*ast.GenDecl); ok && len(gd.Specs) == 1 {
				x.shape, x.stmt = assign, x.path[3].(*ast.DeclStmt)
			}
		}
	case *ast.ReturnStmt:
		if len(p.Results) == 1 && nres > 0 && x.sameResults() {
			x.shape, x.stmt = terminal, p
		}
	}
	if x.stmt != nil {
		if err := x.findBlock(); err != nil {
			if x.exprBody() == nil {
				return err
			}
			x.stmt = nil // fall back to inlining the expression in place
		}
	}
	if x.stmt == nil {
		x.shape = expr
		if nres != 1 {
			return refactor.Refusef("the call to %q is used inside an expression, but %q returns %d values", name, name, nres)
		}
		if x.exprBody() == nil {
			return refactor.Refusef("the call to %q is part of a larger expression or statement header, so only a function whose body is a single return statement can be inlined there; %q has %d statements. Assign the call to a variable first (extract_variable), then inline that statement",
				name, name, len(x.decl.Body.List))
		}
		x.results = x.exprBody().Results
		for _, n := range x.path {
			if s, ok := n.(ast.Stmt); ok {
				x.stmt = s
				break
			}
		}
		return nil
	}
	if x.shape == terminal {
		if _, ok := x.stmt.(*ast.ExprStmt); ok {
			// The caller returns right after: a final bare return in the
			// body would only repeat that.
			if r, ok := lastStmt(x.decl.Body).(*ast.ReturnStmt); ok && len(r.Results) == 0 {
				x.ret = r
			}
		}
		return x.checkBareReturns()
	}
	// Statement and assignment shapes: the body must run to its end.
	last := lastStmt(x.decl.Body)
	for _, r := range x.returns {
		if r != last {
			return refactor.Refusef("%q returns early (at %s); a function with several returns can only be inlined where it is called as \"return %s(...)\"",
				name, x.ws.Position(r.Pos()), name)
		}
	}
	if r, ok := last.(*ast.ReturnStmt); ok {
		x.ret, x.results = r, r.Results
	} else if nres > 0 {
		return refactor.Refusef("%q never returns normally (its last statement is not a return); there are no results to assign", name)
	}
	return nil
}

// lhsEvaluatedFirst returns the first operand on the left of as that Go
// evaluates before the right side and that inlining could move after the
// body: one with a call or that can panic, a package variable, or memory
// read through a pointer or a slice, which the body could change. The
// variables and fields assigned to are not operands; nil if there is none.
func (x *inliner) lhsEvaluatedFirst(as *ast.AssignStmt) ast.Expr {
	stable := func(e ast.Expr) bool {
		_, ok := x.callerFA.Stable(e, false)
		return ok && analysis.MayPanic(x.info, e) == nil
	}
	var check func(e ast.Expr) ast.Expr
	check = func(e ast.Expr) ast.Expr {
		switch e := e.(type) {
		case *ast.Ident:
			return nil
		case *ast.ParenExpr:
			return check(e.X)
		case *ast.SelectorExpr:
			s := x.info.Selections[e]
			switch {
			case s == nil: // a package-qualified variable
				return nil
			case s.Kind() == types.FieldVal && !s.Indirect() && !analysis.IsPointer(x.info.TypeOf(e.X)):
				return check(e.X)
			case !stable(e.X):
				return e.X
			}
			return nil
		case *ast.IndexExpr:
			switch {
			case !stable(e.Index):
				return e.Index
			case analysis.IsArray(x.info.TypeOf(e.X)):
				return check(e.X)
			case !stable(e.X):
				return e.X
			}
			return nil
		case *ast.StarExpr:
			if !stable(e.X) {
				return e.X
			}
			return nil
		}
		return e
	}
	for _, l := range as.Lhs {
		if bad := check(l); bad != nil {
			return bad
		}
	}
	return nil
}

// checkBareReturns refuses a void body whose bare returns, inlined into a
// caller with named results, would be in the scope of a body declaration
// with a result's name: Go rejects a bare return where a result is hidden.
func (x *inliner) checkBareReturns() error {
	cs := x.callerSig()
	if x.sig.Results().Len() > 0 || cs == nil || cs.Results().Len() == 0 || cs.Results().At(0).Name() == "" {
		return nil
	}
	names := map[string]bool{}
	for i := 0; i < cs.Results().Len(); i++ {
		names[cs.Results().At(i).Name()] = true
	}
	for id, o := range x.info.Defs {
		if o != nil && names[id.Name] && x.decl.Body.Pos() <= id.Pos() && id.Pos() < x.decl.Body.End() {
			return refactor.Refusef("%q declares %s (at %s), which has the name of a result of the calling function; its returns would then be rejected (a bare return cannot be used where a result is hidden). Rename it first",
				x.fn.Name(), id.Name, x.ws.Position(id.Pos()))
		}
	}
	return nil
}

// exprBody returns the body's only statement if it is "return e".
func (x *inliner) exprBody() *ast.ReturnStmt {
	if len(x.decl.Body.List) != 1 {
		return nil
	}
	r, ok := x.decl.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(r.Results) != 1 || x.sig.Results().Len() != 1 {
		return nil
	}
	return r
}

// findBlock records the statement list holding x.stmt.
func (x *inliner) findBlock() error {
	i := 0
	for i < len(x.path) && x.path[i] != ast.Node(x.stmt) {
		i++
	}
	if i+1 >= len(x.path) {
		return refactor.Refusef("could not find the statement around the call")
	}
	switch b := x.path[i+1].(type) {
	case *ast.BlockStmt, *ast.CaseClause:
		x.block = b
	case *ast.CommClause:
		if b.Comm == x.stmt {
			return refactor.Refusef("the call to %q is the communication of a select case", x.fn.Name())
		}
		x.block = b
	case *ast.LabeledStmt:
		return refactor.Refusef("the call to %q has the label %s; remove or move the label first", x.fn.Name(), b.Label.Name)
	default:
		return refactor.Refusef("the call to %q is in the header of a statement (an if, for or switch); move it to its own statement first", x.fn.Name())
	}
	x.blockScope = x.info.Scopes[x.block]
	if x.blockScope == nil { // a function body shares the function's scope
		switch f := x.path[i+2].(type) {
		case *ast.FuncDecl:
			x.blockScope = x.info.Scopes[f.Type]
		case *ast.FuncLit:
			x.blockScope = x.info.Scopes[f.Type]
		}
	}
	if x.blockScope == nil {
		return refactor.Refusef("could not find the scope of the block around the call")
	}
	return nil
}

// returnsRightAfter reports whether the caller returns right after s, which
// is the last statement of a function without results or is followed by a
// bare return: the body's own returns then do the same as falling off its
// end.
func (x *inliner) returnsRightAfter(s ast.Stmt) bool {
	list := x.listOf(s)
	for i, t := range list {
		if t != s {
			continue
		}
		if i+1 < len(list) {
			r, ok := list[i+1].(*ast.ReturnStmt)
			return ok && len(r.Results) == 0
		}
		body := funcBody(x.callerFn)
		sig := x.callerSig()
		return body != nil && len(body.List) > 0 && body.List[len(body.List)-1] == s && sig != nil && sig.Results().Len() == 0
	}
	return false
}

func (x *inliner) listOf(s ast.Stmt) []ast.Stmt {
	for i, n := range x.path {
		if n != ast.Node(s) || i+1 >= len(x.path) {
			continue
		}
		switch b := x.path[i+1].(type) {
		case *ast.BlockStmt:
			return b.List
		case *ast.CaseClause:
			return b.Body
		case *ast.CommClause:
			return b.Body
		}
	}
	return nil
}

func (x *inliner) callerSig() *types.Signature {
	switch f := x.callerFn.(type) {
	case *ast.FuncDecl:
		if o, ok := x.info.Defs[f.Name].(*types.Func); ok {
			return o.Signature()
		}
	case *ast.FuncLit:
		s, _ := x.info.TypeOf(f).(*types.Signature)
		return s
	}
	return nil
}

// sameResults reports whether the caller's results have the callee's types,
// so that the body's returns can return from the caller directly.
func (x *inliner) sameResults() bool {
	cs := x.callerSig()
	if cs == nil || cs.Results().Len() != x.sig.Results().Len() {
		return false
	}
	for i := 0; i < cs.Results().Len(); i++ {
		if !types.Identical(cs.Results().At(i).Type(), x.sig.Results().At(i).Type()) {
			return false
		}
	}
	return true
}

func funcBody(n ast.Node) *ast.BlockStmt {
	switch f := n.(type) {
	case *ast.FuncDecl:
		return f.Body
	case *ast.FuncLit:
		return f.Body
	}
	return nil
}

func lastStmt(b *ast.BlockStmt) ast.Stmt {
	if len(b.List) == 0 {
		return nil
	}
	return b.List[len(b.List)-1]
}

// ---------------------------------------------------------------------------
// Results

// decideMerges lets a local the body declares at its top level and returns
// take the place of the new variable the call's result is assigned to, so
// "a, b := pair()" becomes pair's statements rather than ending in
// "a, b := a, b". It also marks result positions that need no assignment.
func (x *inliner) decideMerges() error {
	if x.shape != assign {
		return nil
	}
	as, ok := x.stmt.(*ast.AssignStmt)
	if !ok || len(x.results) != len(as.Lhs) {
		return nil
	}
	returned := map[*types.Var]int{}
	for _, r := range x.results {
		if v := x.localVar(r); v != nil {
			returned[v]++
		}
	}
	var existing []existingMerge
	defer func() { x.mergeExisting(existing) }()
	for i, l := range as.Lhs {
		id, ok := l.(*ast.Ident)
		if !ok {
			continue
		}
		if id.Name == "_" {
			continue // resultLines decides
		}
		v := x.localVar(x.results[i])
		if v == nil || returned[v] != 1 || v.Parent() != x.fnScope || x.calleeFA.Escapes(v) {
			continue
		}
		if o, ok := x.info.Uses[id].(*types.Var); ok { // an existing variable
			if x.canMergeExisting(v, id, o) {
				existing = append(existing, existingMerge{i, v, id, o})
			}
			continue
		}
		lobj, _ := x.info.Defs[id].(*types.Var)
		if lobj == nil || v == nil || returned[v] != 1 || v.Parent() != x.fnScope || x.isParam(v) ||
			!types.Identical(v.Type(), lobj.Type()) || x.calleeFA.Escapes(v) {
			continue
		}
		if id.Name != v.Name() && !x.nameFreeInCallee(id.Name) {
			continue
		}
		x.merged[v] = id
		x.dropped[i] = true
	}
	return nil
}

// localVar returns the callee's local variable (not parameter) that e is,
// or nil.
func (x *inliner) localVar(e ast.Expr) *types.Var {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return nil
	}
	v, ok := x.info.Uses[id].(*types.Var)
	if !ok || !x.inDecl(v.Pos()) || x.isParam(v) {
		return nil
	}
	return v
}

func (x *inliner) isParam(v *types.Var) bool {
	if r := x.sig.Recv(); r != nil && r == v {
		return true
	}
	for i := 0; i < x.sig.Params().Len(); i++ {
		if x.sig.Params().At(i) == v {
			return true
		}
	}
	return false
}

// nameFreeInCallee reports whether no identifier in the callee is spelled
// name, so a local can be renamed to it.
func (x *inliner) nameFreeInCallee(name string) bool {
	free := true
	ast.Inspect(x.decl, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == name {
			free = false
		}
		return free
	})
	return free
}

func (x *inliner) inDecl(p token.Pos) bool { return x.decl.Pos() <= p && p < x.decl.End() }

// ---------------------------------------------------------------------------
// Names

// checkFreeNames ensures every name the body refers to from outside the
// function means the same at the call, and maps imported package names to
// the caller file's names for them.
func (x *inliner) checkFreeNames() error {
	pos := x.call.Pos()
	scope := x.pkg.Types.Scope().Innermost(pos)
	for _, id := range analysis.ScopeRefs(x.info, x.decl.Body) {
		obj := x.info.Uses[id]
		if obj == nil || x.inDecl(obj.Pos()) {
			continue
		}
		if pn, ok := obj.(*types.PkgName); ok {
			n, err := x.qualifier(pn.Imported())
			if err != nil {
				return refactor.Refusef("%q uses package %s, %v", x.fn.Name(), pn.Imported().Path(), err)
			}
			if n == "" {
				return refactor.Refusef("%q uses package %s, which the calling file dot-imports; inlining it is not supported", x.fn.Name(), pn.Imported().Path())
			}
			continue
		}
		if obj.Pkg() != nil && obj.Pkg() != x.pkg.Types {
			return refactor.Refusef("%q refers to %s from package %s through a dot import; inlining it is not supported", x.fn.Name(), id.Name, obj.Pkg().Path())
		}
		if _, got := scope.LookupParent(id.Name, pos); got != obj {
			where := "is not visible there"
			if got != nil {
				where = fmt.Sprintf("refers to the %s declared at %s instead", analysis.KindOf(got), x.ws.Position(got.Pos()))
			}
			return refactor.Refusef("%q uses %s (the %s declared at %s), but at the call %s; rename one of them first",
				x.fn.Name(), id.Name, analysis.KindOf(obj), declaredAt(x.ws, obj), where)
		}
	}
	return nil
}

// qualifier returns the caller file's name for package p (adding an import
// if needed), checking that the name is not hidden at the call.
func (x *inliner) qualifier(p *types.Package) (string, error) {
	n, err := x.imp.Name(p)
	if err != nil || n == "" {
		return n, err
	}
	scope := x.pkg.Types.Scope().Innermost(x.call.Pos())
	if _, got := scope.LookupParent(n, x.call.Pos()); got != nil {
		if pn, ok := got.(*types.PkgName); !ok || pn.Imported() != p {
			return "", fmt.Errorf("which the calling file names %s, but at the call %s refers to the %s declared at %s", n, n, analysis.KindOf(got), x.ws.Position(got.Pos()))
		}
	}
	x.usedQ[n] = true
	return n, nil
}

// typeString spells t for the caller's file.
func (x *inliner) typeString(t types.Type) (string, error) {
	var refusal error
	s := types.TypeString(t, func(p *types.Package) string {
		n, err := x.qualifier(p)
		if err != nil && refusal == nil {
			refusal = refactor.Refusef("the inlined code needs the type %s from package %s, %v", types.TypeString(t, nil), p.Path(), err)
		}
		return n
	})
	if refusal != nil {
		return "", refusal
	}
	// Unqualified names in the spelling must not be hidden at the call.
	scope := x.pkg.Types.Scope().Innermost(x.call.Pos())
	var bad error
	walkNamed(t, map[types.Type]bool{}, func(o *types.TypeName) {
		if o.Pkg() != x.pkg.Types && o.Pkg() != nil {
			return
		}
		if _, got := scope.LookupParent(o.Name(), x.call.Pos()); got != o && bad == nil {
			bad = refactor.Refusef("the inlined code needs the type %s, but at the call %s refers to something else", s, o.Name())
		}
	})
	return s, bad
}

// checkDeclaredNames ensures the names the inlined code declares in the
// calling block (the bound parameters and the body's top-level
// declarations) do not clash with names there.
func (x *inliner) checkDeclaredNames() error {
	if x.shape == expr {
		return nil
	}
	type decl struct {
		name string
		what string
	}
	var names []decl
	for _, p := range x.params {
		if p.bind && p.name != "_" {
			names = append(names, decl{p.name, "parameter " + p.name})
		}
	}
	for _, n := range x.fnScope.Names() {
		o := x.fnScope.Lookup(n)
		if v, ok := o.(*types.Var); ok && x.isParam(v) {
			continue
		}
		if v, ok := o.(*types.Var); ok && x.merged[v] != nil {
			continue // becomes the caller's own variable
		}
		names = append(names, decl{n, fmt.Sprintf("%s %s (declared at %s)", analysis.KindOf(o), n, x.ws.Position(o.Pos()))})
	}
	if len(names) > 0 && x.callerHasGoto() {
		return refactor.Refusef("the calling function uses goto, and the inlined code declares %s; a goto must not jump over declarations, so this is not supported", names[0].what)
	}
	outer := x.pkg.Types.Scope().Innermost(x.stmt.Pos())
	for _, d := range names {
		if o := x.blockScope.Lookup(d.name); o != nil {
			return refactor.Refusef("the inlined code declares %s in the calling block, which already declares %s at %s; rename one of them first",
				d.what, d.name, x.ws.Position(o.Pos()))
		}
		_, o := outer.LookupParent(d.name, x.stmt.Pos())
		if o == nil {
			continue
		}
		if u := x.usedLater(o); u != nil {
			return refactor.Refusef("the inlined code declares %s in the calling block, which would hide the %s %s used later at %s; rename one of them first",
				d.what, analysis.KindOf(o), d.name, x.ws.Position(u.Pos()))
		}
	}
	return nil
}

// usedLater returns a use of o in the calling block from the call's
// statement on (but not in the call itself), or nil.
func (x *inliner) usedLater(o types.Object) *ast.Ident {
	var found *ast.Ident
	ast.Inspect(x.block, func(n ast.Node) bool {
		if n == nil || found != nil || n.End() <= x.stmt.Pos() || n == ast.Node(x.call) {
			return false
		}
		if id, ok := n.(*ast.Ident); ok && x.info.Uses[id] == o {
			found = id
		}
		return true
	})
	return found
}

func (x *inliner) callerHasGoto() bool {
	found := false
	ast.Inspect(funcBody(x.callerFn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if b, ok := n.(*ast.BranchStmt); ok && b.Tok == token.GOTO {
			found = true
		}
		return !found
	})
	return found
}

// ---------------------------------------------------------------------------
// Helpers

func (x *inliner) off(p token.Pos) int  { return x.tf.Offset(p) }
func (x *inliner) doff(p token.Pos) int { return x.dtf.Offset(p) }

func (x *inliner) text(n ast.Node) string  { return string(x.src[x.off(n.Pos()):x.off(n.End())]) }
func (x *inliner) dtext(n ast.Node) string { return string(x.dsrc[x.doff(n.Pos()):x.doff(n.End())]) }

func (x *inliner) quote(n ast.Node) string {
	s := strings.Join(strings.Fields(x.text(n)), " ")
	if len(s) > 60 {
		s = s[:57] + "..."
	}
	return "`" + s + "`"
}

// indentOf returns the leading white space of the line holding p in src.
func indentOf(src []byte, tf *token.File, p token.Pos) string {
	start := tf.Offset(tf.LineStart(tf.Line(p)))
	end := start
	for end < len(src) && (src[end] == ' ' || src[end] == '\t') {
		end++
	}
	return string(src[start:end])
}

func walkNamed(t types.Type, seen map[types.Type]bool, f func(*types.TypeName)) {
	if t == nil || seen[t] {
		return
	}
	seen[t] = true
	switch t := t.(type) {
	case *types.Named:
		f(t.Obj())
		if ta := t.TypeArgs(); ta != nil {
			for i := 0; i < ta.Len(); i++ {
				walkNamed(ta.At(i), seen, f)
			}
		}
	case *types.Alias:
		f(t.Obj())
	case *types.Basic:
		if o, ok := types.Universe.Lookup(t.Name()).(*types.TypeName); ok {
			f(o)
		}
	case *types.Pointer:
		walkNamed(t.Elem(), seen, f)
	case *types.Slice:
		walkNamed(t.Elem(), seen, f)
	case *types.Array:
		walkNamed(t.Elem(), seen, f)
	case *types.Map:
		walkNamed(t.Key(), seen, f)
		walkNamed(t.Elem(), seen, f)
	case *types.Chan:
		walkNamed(t.Elem(), seen, f)
	case *types.Signature:
		for _, tup := range []*types.Tuple{t.Params(), t.Results()} {
			for i := 0; i < tup.Len(); i++ {
				walkNamed(tup.At(i).Type(), seen, f)
			}
		}
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			walkNamed(t.Field(i).Type(), seen, f)
		}
	case *types.Interface:
		for i := 0; i < t.NumEmbeddeds(); i++ {
			walkNamed(t.EmbeddedType(i), seen, f)
		}
		for i := 0; i < t.NumExplicitMethods(); i++ {
			walkNamed(t.ExplicitMethod(i).Type(), seen, f)
		}
	}
}

func declaredAt(ws *workspace.Workspace, o types.Object) string {
	if o.Pkg() == nil || !o.Pos().IsValid() {
		return "universe scope (predeclared)"
	}
	return ws.Position(o.Pos())
}

func quoteAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = fmt.Sprintf("%q", s)
	}
	return out
}
