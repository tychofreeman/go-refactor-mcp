// Package fold implements "fold": replace code that does what an
// existing function does with calls to it.
package fold

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"slices"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
	"golang.org/x/tools/go/packages"
)

// Fold is the "fold" refactoring.
type Fold struct{}

// New returns the fold refactoring.
func New() *Fold { return &Fold{} }

func (*Fold) Name() string { return "fold" }

func (*Fold) Description() string {
	return `Replace code that does what an existing function does with calls to it: every expression in the module that matches the function's body, with its parameters standing for any expressions of their types, becomes a call. The function stays. Not extract_function, which makes a new function from lines at one place: fold reuses a function that exists, written or extracted, at every other place.
Use it after writing a small function to name what code does (a getter, a helper, a method that calls an old function), so the code uses it: u.Email becomes u.email() for func (u User) email() string { return u.Email }; truncate(subject, 20) inside a method of SMS becomes s.fit(subject) for func (s *SMS) fit(text string) string { return truncate(text, 20) }.
Identify the function by file and name (Name or Type.Method). Its body must be one return of one value, using every parameter. Every other name in the body must mean the same at a match. A method's receiver is what matched it, or, when the body does not use it, the receiver of the method of the same type the match is in. With line, only matches starting on that line of file (or of in) are folded, to move callers over one at a time.
Leaves, and says why: a match that is assigned or has its address taken, one where an argument with effects or that could panic would be evaluated a different number of times or in another order, one in another package when the function is not exported, and one in no method of the receiver's type when the call needs the receiver from there.`
}

func (*Fold) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file": {"type": "string",  "description": "Path of the file that declares the function, relative to the workspace root."},
    "name": {"type": "string",  "description": "The function: Name, or Type.Method for a method."},
    "line": {"type": "integer", "description": "Optional: fold only the matches that start on this line of file (or of in)."},
    "in":   {"type": "string",  "description": "Optional, with line: the file whose line it is, if not file."}
  },
  "required": ["file", "name"],
  "additionalProperties": false
}`)
}

type foldArgs struct {
	File string `json:"file"`
	Name string `json:"name"`
	Line int    `json:"line,omitempty"`
	In   string `json:"in,omitempty"`
}

// folder finds the code that does what fn does and replaces it with calls.
type folder struct {
	ws    *workspace.Workspace
	pkg   *packages.Package // fn's
	fn    *types.Func
	decl  *ast.FuncDecl
	body  ast.Expr             // what fn returns: the pattern
	holes map[types.Object]int // fn's parameters (and receiver), and how often the body uses each
	recv  types.Object         // fn's receiver, or nil

	n       int      // occurrences folded
	skipped []string // why matches were left, "file:line: reason"
	by      map[string][]edit.TextEdit
	adders  map[string]*imports.Adder
	order   []string
}

// pattern reads fn's declaration: its body must be one return of one
// value, which uses every parameter.
func (f *folder) pattern(id *ast.Ident) error {
	for _, file := range f.pkg.Syntax {
		for _, d := range file.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Name == id {
				f.decl = fd
			}
		}
	}
	name := f.fn.Name()
	if f.decl == nil || f.decl.Body == nil {
		return refactor.Refusef("cannot find the body of %s", name)
	}
	sig := f.fn.Signature()
	if sig.TypeParams().Len() > 0 || sig.RecvTypeParams().Len() > 0 {
		return refactor.Refusef("%s is generic; folding into generic functions is not supported yet", name)
	}
	if sig.Variadic() {
		return refactor.Refusef("%s is variadic; folding into variadic functions is not supported yet", name)
	}
	if len(f.decl.Body.List) != 1 {
		return refactor.Refusef("fold takes a function whose body is one return statement of one value; %s's body has %d statements", name, len(f.decl.Body.List))
	}
	ret, ok := f.decl.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return refactor.Refusef("fold takes a function whose body is one return statement of one value; %s's is not", name)
	}
	f.body = ret.Results[0]
	info := f.pkg.TypesInfo
	for _, fl := range f.decl.Type.Params.List {
		if len(fl.Names) == 0 {
			return refactor.Refusef("the parameters of %s have no names", name)
		}
		for _, n := range fl.Names {
			f.holes[info.Defs[n]] = 0
		}
	}
	if r := f.decl.Recv; r != nil && len(r.List[0].Names) == 1 && r.List[0].Names[0].Name != "_" {
		f.recv = info.Defs[r.List[0].Names[0]]
		f.holes[f.recv] = 0
	}
	ast.Inspect(f.body, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if c, hole := f.holes[info.Uses[id]]; hole {
				f.holes[info.Uses[id]] = c + 1
			}
		}
		return true
	})
	for o, c := range f.holes {
		if c == 0 && o != f.recv {
			return refactor.Refusef("%s's body does not use its parameter %s, so a call could not be built from what matches", name, o.Name())
		}
	}
	return nil
}

// find folds every match of the pattern in the workspace, or with line
// only those that start on that line of the file in.
func (f *folder) find(in string, line int) error {
	f.by, f.adders = map[string][]edit.TextEdit{}, map[string]*imports.Adder{}
	seen := map[token.Position]bool{}
	declFile := f.ws.Fset.Position(f.decl.Pos()).Filename
	for _, p := range f.ws.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, file := range p.Syntax {
			path := f.ws.Fset.Position(file.Pos()).Filename
			if in != "" && path != in {
				continue
			}
			var stack []ast.Node
			var err error
			ast.Inspect(file, func(n ast.Node) bool {
				if n == nil {
					stack = stack[:len(stack)-1]
					return false
				}
				if err != nil {
					return false
				}
				if fd, ok := n.(*ast.FuncDecl); ok && path == declFile && fd.Pos() == f.decl.Pos() {
					return false // fn itself
				}
				x, isExpr := n.(ast.Expr)
				pos := f.ws.Fset.Position(n.Pos())
				if isExpr && !seen[pos] && (line == 0 || pos.Line == line) {
					var folded bool
					if folded, err = f.try(p, file, x, stack); folded {
						seen[pos] = true
						return false
					}
				}
				stack = append(stack, n)
				return true
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// try folds x, an expression of file of package p inside the nodes stack
// (outermost first), if it matches the pattern and the call means the same.
func (f *folder) try(p *packages.Package, file *ast.File, x ast.Expr, stack []ast.Node) (bool, error) {
	info := p.TypesInfo
	if tv, ok := info.Types[x]; !ok || !tv.IsValue() || tv.Value != nil {
		return false, nil // a type, or a constant: nothing to fold
	}
	b := map[types.Object]ast.Expr{}
	if !f.match(f.body, x, info, b) {
		return false, nil
	}
	where := f.ws.Position(x.Pos())
	skip := func(why string) (bool, error) {
		f.skipped = append(f.skipped, where+": "+why)
		return false, nil
	}
	if len(stack) > 0 && assigned(stack[len(stack)-1], x) {
		return skip("it is assigned or its address taken")
	}
	if p.PkgPath != f.pkg.PkgPath && !f.fn.Exported() {
		return skip(fmt.Sprintf("%s is not exported, so package %s cannot call it", f.fn.Name(), p.Name))
	}
	// Arguments are evaluated once, before the body: one that has effects
	// or could panic must be used once, and be what the body evaluates
	// first.
	impure := 0
	for o, e := range b {
		if analysis.HasEffect(info, e) || analysis.MayPanic(info, e) != nil {
			impure++
			if f.holes[o] > 1 {
				return skip(fmt.Sprintf("%s, used %d times in the body, would be evaluated once", types.ExprString(e), f.holes[o]))
			}
			if !f.first(o) {
				return skip(fmt.Sprintf("%s would be evaluated before the rest of the body, not in its order", types.ExprString(e)))
			}
		}
	}
	if impure > 1 {
		return skip("more than one argument has effects or could panic")
	}
	src, err := os.ReadFile(f.ws.Fset.Position(file.Pos()).Filename)
	if err != nil {
		return false, err
	}
	text := func(e ast.Expr) string {
		return string(src[f.ws.Fset.Position(e.Pos()).Offset:f.ws.Fset.Position(e.End()).Offset])
	}
	var args []string
	for _, fl := range f.decl.Type.Params.List {
		for _, n := range fl.Names {
			args = append(args, text(b[f.pkg.TypesInfo.Defs[n]]))
		}
	}
	call := f.fn.Name() + "(" + strings.Join(args, ", ") + ")"
	switch {
	case f.fn.Signature().Recv() != nil:
		recv, why := f.receiver(info, b, stack, text)
		if why != "" {
			return skip(why)
		}
		call = recv + "." + call
	case p.PkgPath != f.pkg.PkgPath:
		path := f.ws.Fset.Position(file.Pos()).Filename
		if f.adders[path] == nil {
			f.adders[path] = imports.New(f.ws, p, file, src)
		}
		q, err := f.adders[path].Name(f.fn.Pkg())
		if err != nil {
			return skip(err.Error())
		}
		call = q + "." + call
	}
	path := f.ws.Fset.Position(x.Pos()).Filename
	if f.by[path] == nil {
		f.order = append(f.order, path)
	}
	f.by[path] = append(f.by[path], edit.TextEdit{Start: f.ws.Fset.Position(x.Pos()).Offset, End: f.ws.Fset.Position(x.End()).Offset, NewText: call})
	f.n++
	return true, nil
}

// first reports whether the hole o is the first thing the body evaluates
// that matters: the body has neither effects nor panics but in its
// holes, or o is an argument of the body's outermost call whose other
// arguments have neither.
func (f *folder) first(o types.Object) bool {
	info := f.pkg.TypesInfo
	if !analysis.HasEffect(info, f.body) && analysis.MayPanic(info, f.body) == nil {
		return true
	}
	call, ok := ast.Unparen(f.body).(*ast.CallExpr)
	if !ok {
		return false
	}
	for _, a := range call.Args {
		if id, ok := ast.Unparen(a).(*ast.Ident); ok && info.Uses[id] == o {
			continue
		}
		if analysis.HasEffect(info, a) || analysis.MayPanic(info, a) != nil {
			return false
		}
	}
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		return true
	case *ast.SelectorExpr:
		_, pkgName := info.Uses[identOf(fun.X)].(*types.PkgName)
		return pkgName
	}
	return false
}

// receiver is the receiver of the call: what matched the body's receiver,
// or, when the body does not use it, the receiver of the method of the
// same type the occurrence is in; or why there is none.
func (f *folder) receiver(info *types.Info, b map[types.Object]ast.Expr, stack []ast.Node, text func(ast.Expr) string) (string, string) {
	recvT := f.fn.Signature().Recv().Type()
	_, ptr := recvT.(*types.Pointer)
	if e, ok := b[f.recv]; ok {
		_, isPtr := info.TypeOf(e).(*types.Pointer)
		switch ast.Unparen(e).(type) {
		case *ast.Ident, *ast.SelectorExpr, *ast.IndexExpr:
			return text(e), ""
		case *ast.CallExpr:
			if ptr && !isPtr {
				return "", types.ExprString(e) + " cannot have its address taken for a pointer receiver"
			}
			return text(e), ""
		}
		if ptr && !isPtr {
			return "", types.ExprString(e) + " cannot have its address taken for a pointer receiver"
		}
		return "(" + text(e) + ")", ""
	}
	base := baseName(recvT)
	for i := len(stack) - 1; i >= 0; i-- {
		fd, ok := stack[i].(*ast.FuncDecl)
		if !ok || fd.Recv == nil || len(fd.Recv.List[0].Names) != 1 {
			continue
		}
		r := info.Defs[fd.Recv.List[0].Names[0]]
		if r != nil && base != nil && sameObj(baseName(r.Type()), base) && fd.Recv.List[0].Names[0].Name != "_" {
			return fd.Recv.List[0].Names[0].Name, ""
		}
	}
	return "", fmt.Sprintf("it is not in a method of %s, whose receiver the call needs", base.Name())
}

// edits are the folds, with the imports they need, by file.
func (f *folder) edits() []edit.FileEdit {
	var out []edit.FileEdit
	for _, path := range f.order {
		es := f.by[path]
		if a := f.adders[path]; a != nil {
			es = append(es, a.Edits()...)
		}
		out = append(out, edit.FileEdit{Path: path, Edits: es})
	}
	return out
}

// match reports whether x, of the package whose info is info, is the
// pattern p (of fn's package) with its holes filled in b.
func (f *folder) match(p, x ast.Expr, info *types.Info, b map[types.Object]ast.Expr) bool {
	p, x = ast.Unparen(p), ast.Unparen(x)
	pinfo := f.pkg.TypesInfo
	sub := func(p, x ast.Expr) bool {
		if p == nil || x == nil {
			return p == nil && x == nil
		}
		return f.match(p, x, info, b)
	}
	switch pp := p.(type) {
	case *ast.Ident:
		po := pinfo.Uses[pp]
		if _, hole := f.holes[po]; hole && po != nil {
			if prev, ok := b[po]; ok {
				return types.ExprString(prev) == types.ExprString(x)
			}
			xt := info.TypeOf(x)
			if xt == nil || !info.Types[x].IsValue() {
				return false
			}
			if po == f.recv {
				if !sameObj(baseName(xt), baseName(po.Type())) {
					return false
				}
			} else if !types.AssignableTo(xt, po.Type()) && types.TypeString(xt, nil) != types.TypeString(po.Type(), nil) {
				return false
			}
			b[po] = x
			return true
		}
		xi, ok := x.(*ast.Ident)
		return ok && xi.Name == pp.Name && sameObj(po, info.Uses[xi])
	case *ast.BasicLit:
		xb, ok := x.(*ast.BasicLit)
		return ok && xb.Kind == pp.Kind && xb.Value == pp.Value
	case *ast.SelectorExpr:
		xs, ok := x.(*ast.SelectorExpr)
		return ok && xs.Sel.Name == pp.Sel.Name && sub(pp.X, xs.X) && sameObj(pinfo.Uses[pp.Sel], info.Uses[xs.Sel])
	case *ast.CallExpr:
		xc, ok := x.(*ast.CallExpr)
		if !ok || len(xc.Args) != len(pp.Args) || xc.Ellipsis.IsValid() != pp.Ellipsis.IsValid() || !sub(pp.Fun, xc.Fun) {
			return false
		}
		for i := range pp.Args {
			if !sub(pp.Args[i], xc.Args[i]) {
				return false
			}
		}
		return true
	case *ast.BinaryExpr:
		xb, ok := x.(*ast.BinaryExpr)
		return ok && xb.Op == pp.Op && sub(pp.X, xb.X) && sub(pp.Y, xb.Y)
	case *ast.UnaryExpr:
		xu, ok := x.(*ast.UnaryExpr)
		return ok && xu.Op == pp.Op && sub(pp.X, xu.X)
	case *ast.StarExpr:
		xs, ok := x.(*ast.StarExpr)
		return ok && sub(pp.X, xs.X)
	case *ast.IndexExpr:
		xi, ok := x.(*ast.IndexExpr)
		return ok && sub(pp.X, xi.X) && sub(pp.Index, xi.Index)
	case *ast.SliceExpr:
		xs, ok := x.(*ast.SliceExpr)
		return ok && xs.Slice3 == pp.Slice3 && sub(pp.X, xs.X) && sub(pp.Low, xs.Low) && sub(pp.High, xs.High) && sub(pp.Max, xs.Max)
	case *ast.TypeAssertExpr:
		xt, ok := x.(*ast.TypeAssertExpr)
		return ok && pp.Type != nil && xt.Type != nil && sub(pp.X, xt.X) && types.Identical(pinfo.TypeOf(pp.Type), info.TypeOf(xt.Type))
	case *ast.CompositeLit:
		xc, ok := x.(*ast.CompositeLit)
		if !ok || len(xc.Elts) != len(pp.Elts) || types.TypeString(pinfo.TypeOf(pp), nil) != types.TypeString(info.TypeOf(xc), nil) {
			return false
		}
		for i := range pp.Elts {
			if !sub(pp.Elts[i], xc.Elts[i]) {
				return false
			}
		}
		return true
	case *ast.KeyValueExpr:
		xk, ok := x.(*ast.KeyValueExpr)
		return ok && sub(pp.Key, xk.Key) && sub(pp.Value, xk.Value)
	}
	return false
}

// sameObj reports whether a and b, from any variants of their packages,
// are the same object.
func sameObj(a, b types.Object) bool {
	switch {
	case a == nil || b == nil:
		return false
	case a.Name() != b.Name():
		return false
	case a.Pkg() == nil || b.Pkg() == nil:
		return a.Pkg() == nil && b.Pkg() == nil
	}
	if pa, ok := a.(*types.PkgName); ok {
		pb, ok := b.(*types.PkgName)
		return ok && pa.Imported().Path() == pb.Imported().Path()
	}
	return a.Pkg().Path() == b.Pkg().Path() && a.Pos() == b.Pos()
}

// baseName is the type name of t, T or *T, or nil.
func baseName(t types.Type) types.Object {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := t.(*types.Named); ok {
		return n.Obj()
	}
	return nil
}

// identOf is e's identifier, if it is one.
func identOf(e ast.Expr) *ast.Ident {
	id, _ := ast.Unparen(e).(*ast.Ident)
	return id
}

// assigned reports whether x is written by parent: the left of an
// assignment, ++ or --, or the operand of &.
func assigned(parent ast.Node, x ast.Expr) bool {
	switch p := parent.(type) {
	case *ast.AssignStmt:
		return slices.Contains(p.Lhs, x)
	case *ast.IncDecStmt:
		return p.X == x
	case *ast.UnaryExpr:
		return p.Op == token.AND && p.X == x
	case *ast.RangeStmt:
		return p.Key == x || p.Value == x
	}
	return false
}

// limit keeps the first n of s, noting how many more there are.
func limit(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return append(s[:n:n], fmt.Sprintf("and %d more", len(s)-n))
}

// Plan checks preconditions and computes the edits.
func (*Fold) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a foldArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	pkg, id, err := locate.Ident(ws, locate.IdentQuery{File: a.File, Name: a.Name})
	if err != nil {
		return nil, err
	}
	fn, ok := pkg.TypesInfo.Defs[id].(*types.Func)
	if !ok {
		return nil, refactor.Refusef("%s is not a function or method", a.Name)
	}
	f := &folder{ws: ws, pkg: pkg, fn: fn, holes: map[types.Object]int{}}
	if err := f.pattern(id); err != nil {
		return nil, err
	}
	in := ""
	if a.Line > 0 {
		in = ws.Abs(a.File)
		if a.In != "" {
			in = ws.Abs(a.In)
		}
	}
	if err := f.find(in, a.Line); err != nil {
		return nil, err
	}
	if f.n == 0 {
		why := ""
		if len(f.skipped) > 0 {
			why = "; what matched was left: " + strings.Join(limit(f.skipped, 5), "; ")
		}
		return nil, refactor.Refusef("nothing in the module matches the body of %s (%s)%s", fn.Name(), types.ExprString(f.body), why)
	}
	plan := &refactor.Plan{
		Summary: fmt.Sprintf("folded %d occurrence%s of %s into calls of %s", f.n, textutil.Plural(f.n), types.ExprString(f.body), fn.Name()),
		Edits:   f.edits(),
	}
	if len(f.skipped) > 0 {
		plan.Notes = append(plan.Notes, "left as they are: "+strings.Join(limit(f.skipped, 5), "; "))
	}
	return plan, nil
}
