// Package parameterize implements "parameterize": functions that are
// the same but for constants become calls of one function that takes
// those constants as parameters.
package parameterize

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"reflect"
	"slices"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/pattern"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/site"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// Parameterize is the "parameterize" refactoring.
type Parameterize struct{ refactor.Named[args] }

// New returns the parameterize refactoring.
func New() *Parameterize { return &Parameterize{Named: refactor.Called[args]("parameterize")} }

func (*Parameterize) Description() string {
	return `Make functions that are the same but for constants into calls of one new function that takes those constants as parameters: logMist(bed) { log.Printf("mist on %s", bed) } and logDrip(row) { log.Printf("drip on %s", row) } become calls logOn(bed, "mist on %s") and logOn(row, "drip on %s") of func logOn(bed string, format string) { log.Printf(format, bed) }. Each keeps its signature and callers; inline_function with all_calls retires them.
Give file, functions (two or more of one package with the same signature; methods of one type), name for the new function, and parameters: a name for each constant that differs, in the order they appear.
Refuses, saying why: functions that differ in more than constants, or not at all (merge_duplicate_function is for that), a name already taken.`
}

type args struct {
	File       string   `json:"file" desc:"Path of a file of the functions' package, relative to the workspace root."`
	Functions  []string `json:"functions" desc:"The functions, Name or Type.Method; the first one's names are the new function's."`
	Name       string   `json:"name" desc:"Name of the new function (a method of the same type, for methods)."`
	Parameters []string `json:"parameters" desc:"A name for each constant that differs, in the order they appear in the first function."`
}

// fun is one of the functions.
type fun struct {
	site.Site
	name   string
	obj    *types.Func
	decl   *ast.FuncDecl
	params []types.Object
	recv   types.Object // nil for a function
	path   string
}

// findFun finds the function name (Name or Type.Method) of file's package.
func findFun(ws *workspace.Workspace, file, name string) (*fun, error) {
	pkg, id, err := locate.Ident(ws, locate.IdentQuery{File: file, Name: name})
	if err != nil {
		return nil, err
	}
	obj, ok := pkg.TypesInfo.Defs[id].(*types.Func)
	if !ok {
		return nil, refactor.Refusef("%s is not a function or method", name)
	}
	f := &fun{Site: site.Site{WS: ws, Pkg: pkg}, name: name, obj: obj}
	for _, file := range pkg.Syntax {
		for _, d := range file.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Name == id {
				f.decl = fd
			}
		}
	}
	if f.decl == nil || f.decl.Body == nil {
		return nil, refactor.Refusef("cannot find the body of %s", name)
	}
	if obj.Signature().TypeParams().Len() > 0 {
		return nil, refactor.Refusef("%s is generic; not supported yet", name)
	}
	for _, fl := range f.decl.Type.Params.List {
		if len(fl.Names) == 0 {
			return nil, refactor.Refusef("the parameters of %s have no names; name them first", name)
		}
		for _, n := range fl.Names {
			f.params = append(f.params, pkg.TypesInfo.Defs[n])
		}
	}
	if r := f.decl.Recv; r != nil {
		if len(r.List[0].Names) != 1 || r.List[0].Names[0].Name == "_" {
			return nil, refactor.Refusef("the receiver of %s has no name; name it first", name)
		}
		f.recv = pkg.TypesInfo.Defs[r.List[0].Names[0]]
	}
	f.path = ws.Fset.Position(f.decl.Pos()).Filename
	if f.Src, err = os.ReadFile(f.path); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *fun) text(from, to token.Pos) string { return string(f.Src[f.Off(from):f.Off(to)]) }

// sameShape refuses g unless it has f's package, signature and receiver
// type.
func (f *fun) sameShape(g *fun) error {
	switch {
	case g.Pkg.PkgPath != f.Pkg.PkgPath:
		return refactor.Refusef("%s and %s are in different packages", f.name, g.name)
	case !types.Identical(f.obj.Type(), g.obj.Type()):
		return refactor.Refusef("%s and %s have different signatures", f.name, g.name)
	case (f.recv == nil) != (g.recv == nil) || f.recv != nil && !types.Identical(f.recv.Type(), g.recv.Type()):
		return refactor.Refusef("%s and %s are not methods of one receiver type", f.name, g.name)
	}
	return nil
}

// where is where a match against g failed, on each side: the innermost
// expressions around it that are not names, or the statement's kind and
// line.
func (g *fun) where(miss *pattern.Miss) (string, string) {
	p, x := miss.P, miss.X
	if miss.AroundP != nil {
		p, x = miss.AroundP, miss.AroundX
	}
	pe, okP := p.(ast.Expr)
	xe, okX := x.(ast.Expr)
	if okP && okX {
		return types.ExprString(pe), types.ExprString(xe)
	}
	return fmt.Sprintf("%s (line %d)", reflect.TypeOf(p).Elem().Name(), g.WS.Fset.Position(p.Pos()).Line), fmt.Sprintf("line %d", g.WS.Fset.Position(x.Pos()).Line)
}

// checkNames refuses a new function name taken in the package (or by a
// method of the receiver's type), or parameter names the body uses.
func (f *fun) checkNames(name string, params []string) error {
	if f.recv != nil {
		if obj, _, _ := types.LookupFieldOrMethod(f.recv.Type(), true, f.Pkg.Types, name); obj != nil {
			return refactor.Refusef("%s already has a field or method %s", types.TypeString(f.recv.Type(), types.RelativeTo(f.Pkg.Types)), name)
		}
	} else if f.Pkg.Types.Scope().Lookup(name) != nil {
		return refactor.Refusef("package %s already declares %s", f.Pkg.Name, name)
	}
	used := map[string]bool{}
	ast.Inspect(f.decl, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			used[id.Name] = true
		}
		return true
	})
	for _, p := range params {
		if !token.IsIdentifier(p) {
			return refactor.Refusef("parameter %q is not a Go identifier", p)
		}
		if used[p] {
			return refactor.Refusef("%s already uses the name %s; choose another for the parameter", f.name, p)
		}
	}
	return nil
}

// Plan checks preconditions and computes the edits.
func (*Parameterize) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	switch {
	case len(a.Functions) < 2:
		return nil, refactor.Refusef("give two or more functions")
	case !token.IsIdentifier(a.Name):
		return nil, refactor.Refusef("name %q is not a Go identifier", a.Name)
	}
	var fs []*fun
	for _, n := range a.Functions {
		f, err := findFun(ws, a.File, n)
		if err != nil {
			return nil, err
		}
		if len(fs) > 0 {
			if err := fs[0].sameShape(f); err != nil {
				return nil, err
			}
		}
		fs = append(fs, f)
	}
	first := fs[0]
	// The constants of the first function that differ in some other, in
	// the order they appear, and each function's constant there.
	var at []*ast.BasicLit
	lits := make([]map[*ast.BasicLit]*ast.BasicLit, len(fs))
	for i, g := range fs[1:] {
		found := map[*ast.BasicLit]*ast.BasicLit{}
		m := &pattern.Matcher{
			Pkg: first.Pkg,
			Compare: pattern.Exactly{Literal: func(p, x *ast.BasicLit) bool {
				found[p] = x
				return true
			}},
		}
		a := m.At(g.Pkg.TypesInfo)
		a.Locals = map[types.Object]types.Object{}
		for j, p := range first.params {
			a.Locals[p] = g.params[j]
		}
		if first.recv != nil {
			a.Locals[first.recv] = g.recv
		}
		if !a.MatchStmt(first.decl.Body, g.decl.Body) {
			at, there := g.where(a.Miss)
			return nil, refactor.Refusef("%s and %s differ at %s and %s, which are not constants; parameterize makes a parameter only of constants", first.name, g.name, at, there)
		}
		lits[i+1] = found
		for p, x := range found {
			if p.Value != x.Value && !slices.Contains(at, p) {
				at = append(at, p)
			}
		}
	}
	slices.SortFunc(at, func(x, y *ast.BasicLit) int { return int(x.Pos() - y.Pos()) })
	if len(at) == 0 {
		return nil, refactor.Refusef("%s do not differ; merge_duplicate_function merges functions that are the same", strings.Join(a.Functions, " and "))
	}
	if len(at) != len(a.Parameters) {
		var vals []string
		for _, p := range at {
			vals = append(vals, p.Value)
		}
		return nil, refactor.Refusef("the functions differ in %d constant%s (%s in %s), so give %d name%s in parameters, not %d", len(at), textutil.Plural(len(at)), strings.Join(vals, ", "), first.name, len(at), textutil.Plural(len(at)), len(a.Parameters))
	}
	if err := first.checkNames(a.Name, a.Parameters); err != nil {
		return nil, err
	}

	// The new function: the first one, its constants made parameters.
	info := first.Pkg.TypesInfo
	text := first.text(first.decl.Pos(), first.decl.End())
	base := first.Off(first.decl.Pos())
	type rep struct {
		at   int
		end  int
		text string
	}
	reps := []rep{{first.Off(first.decl.Name.Pos()) - base, first.Off(first.decl.Name.End()) - base, a.Name}}
	var added []string
	for i, p := range at {
		reps = append(reps, rep{first.Off(p.Pos()) - base, first.Off(p.End()) - base, a.Parameters[i]})
		t := info.Types[p].Type
		if b, ok := t.(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
			t = types.Default(t)
		}
		added = append(added, a.Parameters[i]+" "+types.TypeString(t, types.RelativeTo(first.Pkg.Types)))
	}
	closing := first.Off(first.decl.Type.Params.Closing) - base
	sep := ", "
	if first.decl.Type.Params.NumFields() == 0 {
		sep = ""
	}
	reps = append(reps, rep{closing, closing, sep + strings.Join(added, ", ")})
	slices.SortFunc(reps, func(x, y rep) int { return y.at - x.at })
	for _, r := range reps {
		text = text[:r.at] + r.text + text[r.end:]
	}

	// Each function's body: a call of the new one with its constants.
	var edits []edit.FileEdit
	for i, g := range fs {
		var args []string
		for _, p := range g.params {
			args = append(args, p.Name())
		}
		for _, p := range at {
			lit := p
			if i > 0 {
				lit = lits[i][p]
			}
			args = append(args, lit.Value)
		}
		call := a.Name + "(" + strings.Join(args, ", ") + ")"
		if g.recv != nil {
			call = g.recv.Name() + "." + call
		}
		if g.obj.Signature().Results().Len() > 0 {
			call = "return " + call
		}
		body := g.decl.Body
		stmt := " " + call + " "
		if ws.Fset.Position(body.Lbrace).Line != ws.Fset.Position(body.Rbrace).Line {
			stmt = "\n\t" + call + "\n"
		}
		es := []edit.TextEdit{{Start: g.Off(body.Lbrace) + 1, End: g.Off(body.Rbrace), NewText: stmt}}
		if i == 0 {
			es = append(es, edit.TextEdit{Start: g.Off(g.decl.End()), End: g.Off(g.decl.End()), NewText: "\n\n" + text})
		}
		edits = append(edits, edit.FileEdit{Path: g.path, Edits: es})
	}
	return &refactor.Plan{
		Summary: fmt.Sprintf("made %s calls of a new %s(..., %s)", strings.Join(a.Functions, ", "), a.Name, strings.Join(added, ", ")),
		Edits:   edit.Merge(edits),
	}, nil
}
