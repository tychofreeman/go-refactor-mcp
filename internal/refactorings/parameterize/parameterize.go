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
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
	"golang.org/x/tools/go/packages"
)

// Parameterize is the "parameterize" refactoring.
type Parameterize struct{}

// New returns the parameterize refactoring.
func New() *Parameterize { return &Parameterize{} }

func (*Parameterize) Name() string { return "parameterize" }

func (*Parameterize) Description() string {
	return `Make functions that are the same but for constants into calls of one new function that takes those constants as parameters: logEmail(to) { log.Printf("email to %s", to) } and logSMS(addr) { log.Printf("sms to %s", addr) } become calls logTo(to, "email to %s") and logTo(addr, "sms to %s") of func logTo(to string, format string) { log.Printf(format, to) }. Each keeps its signature and callers; inline_function with all_calls retires them.
Give file, functions (two or more of one package with the same signature; methods of one type), name for the new function, and parameters: a name for each constant that differs, in the order they appear.
Refuses, saying why: functions that differ in more than constants, or not at all (merge_duplicate_function is for that), a name already taken.`
}

func (*Parameterize) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":       {"type": "string", "description": "Path of a file of the functions' package, relative to the workspace root."},
    "functions":  {"type": "array", "items": {"type": "string"}, "description": "The functions, Name or Type.Method; the first one's names are the new function's."},
    "name":       {"type": "string", "description": "Name of the new function (a method of the same type, for methods)."},
    "parameters": {"type": "array", "items": {"type": "string"}, "description": "A name for each constant that differs, in the order they appear in the first function."}
  },
  "required": ["file", "functions", "name", "parameters"],
  "additionalProperties": false
}`)
}

type args struct {
	File       string   `json:"file"`
	Functions  []string `json:"functions"`
	Name       string   `json:"name"`
	Parameters []string `json:"parameters"`
}

// fun is one of the functions.
type fun struct {
	ws     *workspace.Workspace
	name   string
	pkg    *packages.Package
	obj    *types.Func
	decl   *ast.FuncDecl
	params []types.Object
	recv   types.Object // nil for a function
	path   string
	src    []byte
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
	f := &fun{ws: ws, name: name, pkg: pkg, obj: obj}
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
	if f.src, err = os.ReadFile(f.path); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *fun) off(p token.Pos) int { return f.ws.Fset.Position(p).Offset }

func (f *fun) text(from, to token.Pos) string { return string(f.src[f.off(from):f.off(to)]) }

// sameShape refuses g unless it has f's package, signature and receiver
// type.
func (f *fun) sameShape(g *fun) error {
	switch {
	case g.pkg.PkgPath != f.pkg.PkgPath:
		return refactor.Refusef("%s and %s are in different packages", f.name, g.name)
	case !types.Identical(f.obj.Type(), g.obj.Type()):
		return refactor.Refusef("%s and %s have different signatures", f.name, g.name)
	case (f.recv == nil) != (g.recv == nil) || f.recv != nil && !types.Identical(f.recv.Type(), g.recv.Type()):
		return refactor.Refusef("%s and %s are not methods of one receiver type", f.name, g.name)
	}
	return nil
}

// checkNames refuses a new function name taken in the package (or by a
// method of the receiver's type), or parameter names the body uses.
func (f *fun) checkNames(name string, params []string) error {
	if f.recv != nil {
		if obj, _, _ := types.LookupFieldOrMethod(f.recv.Type(), true, f.pkg.Types, name); obj != nil {
			return refactor.Refusef("%s already has a field or method %s", types.TypeString(f.recv.Type(), types.RelativeTo(f.pkg.Types)), name)
		}
	} else if f.pkg.Types.Scope().Lookup(name) != nil {
		return refactor.Refusef("package %s already declares %s", f.pkg.Name, name)
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

// comparer compares a's body with b's: the same but for constants, with
// parameters matched by position and locals by declaration.
type comparer struct {
	a, b   *fun
	locals map[types.Object]types.Object
	lits   map[*ast.BasicLit]*ast.BasicLit // each of a's constants, and b's there
	stack  []ast.Expr                      // the expressions being compared, of a and b in turn
	bad    [2]string                       // where they first differ in other than a constant
}

var posType = reflect.TypeOf(token.NoPos)

// eq reports whether a and b are the same but for constants.
func (c *comparer) eq(a, b ast.Node) bool {
	switch an := a.(type) {
	case *ast.Ident:
		bn, ok := b.(*ast.Ident)
		if !ok || !c.ident(an, bn) {
			return c.mismatch(a, b)
		}
		return true
	case *ast.BasicLit:
		bn, ok := b.(*ast.BasicLit)
		if !ok || an.Kind != bn.Kind {
			return c.mismatch(a, b)
		}
		c.lits[an] = bn
		return true
	case *ast.CommentGroup:
		return true
	}
	if reflect.TypeOf(a) != reflect.TypeOf(b) {
		return c.mismatch(a, b)
	}
	if ea, ok := a.(ast.Expr); ok {
		c.stack = append(c.stack, ea, b.(ast.Expr))
		defer func() { c.stack = c.stack[:len(c.stack)-2] }()
	}
	va, vb := reflect.ValueOf(a).Elem(), reflect.ValueOf(b).Elem()
	for i := range va.NumField() {
		fa, fb := va.Field(i), vb.Field(i)
		if fa.Type() == posType {
			continue
		}
		switch fa.Kind() {
		case reflect.Interface, reflect.Pointer:
			if fa.IsNil() || fb.IsNil() {
				if fa.IsNil() != fb.IsNil() {
					return c.mismatch(a, b)
				}
				continue
			}
			na, okA := fa.Interface().(ast.Node)
			nb, okB := fb.Interface().(ast.Node)
			if okA && okB && !c.eq(na, nb) {
				return false
			}
		case reflect.Slice:
			if fa.Len() != fb.Len() {
				return c.mismatch(a, b)
			}
			for j := range fa.Len() {
				na, okA := fa.Index(j).Interface().(ast.Node)
				nb, okB := fb.Index(j).Interface().(ast.Node)
				if okA && okB && !c.eq(na, nb) {
					return false
				}
			}
		case reflect.Int, reflect.Bool, reflect.String:
			if fa.Interface() != fb.Interface() {
				return c.mismatch(a, b)
			}
		}
	}
	return true
}

// ident reports whether a and b mean the same: parameters at the same
// position, locals declared at the same place, or the same object.
func (c *comparer) ident(a, b *ast.Ident) bool {
	ia, ib := c.a.pkg.TypesInfo, c.b.pkg.TypesInfo
	if d := ia.Defs[a]; d != nil {
		if e := ib.Defs[b]; e != nil {
			c.locals[d] = e
			return true
		}
		return false
	}
	oa, ob := ia.Uses[a], ib.Uses[b]
	if i := slices.Index(c.a.params, oa); i >= 0 && oa != nil {
		return c.b.params[i] == ob
	}
	if oa != nil && oa == c.a.recv {
		return ob == c.b.recv
	}
	if l, ok := c.locals[oa]; ok {
		return l == ob
	}
	return a.Name == b.Name && oa == ob
}

// mismatch notes where a and b first differ: the innermost expressions
// around them that are not names. It returns false.
func (c *comparer) mismatch(a, b ast.Node) bool {
	if c.bad[0] != "" {
		return false
	}
	ea, eb := a, b
	if _, ident := a.(*ast.Ident); ident && len(c.stack) >= 2 {
		ea, eb = c.stack[len(c.stack)-2], c.stack[len(c.stack)-1]
	}
	if x, ok := ea.(ast.Expr); ok {
		c.bad[0], c.bad[1] = types.ExprString(x), types.ExprString(eb.(ast.Expr))
	} else {
		c.bad[0], c.bad[1] = fmt.Sprintf("%s (line %d)", reflect.TypeOf(a).Elem().Name(), c.a.ws.Fset.Position(a.Pos()).Line), fmt.Sprintf("line %d", c.b.ws.Fset.Position(b.Pos()).Line)
	}
	return false
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
		c := &comparer{a: first, b: g, locals: map[types.Object]types.Object{}, lits: map[*ast.BasicLit]*ast.BasicLit{}}
		if !c.eq(first.decl.Body, g.decl.Body) {
			return nil, refactor.Refusef("%s and %s differ at %s and %s, which are not constants; parameterize makes a parameter only of constants", first.name, g.name, c.bad[0], c.bad[1])
		}
		lits[i+1] = c.lits
		for p, x := range c.lits {
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
	info := first.pkg.TypesInfo
	text := first.text(first.decl.Pos(), first.decl.End())
	base := first.off(first.decl.Pos())
	type rep struct {
		at   int
		end  int
		text string
	}
	reps := []rep{{first.off(first.decl.Name.Pos()) - base, first.off(first.decl.Name.End()) - base, a.Name}}
	var added []string
	for i, p := range at {
		reps = append(reps, rep{first.off(p.Pos()) - base, first.off(p.End()) - base, a.Parameters[i]})
		t := info.Types[p].Type
		if b, ok := t.(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
			t = types.Default(t)
		}
		added = append(added, a.Parameters[i]+" "+types.TypeString(t, types.RelativeTo(first.pkg.Types)))
	}
	closing := first.off(first.decl.Type.Params.Closing) - base
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
		es := []edit.TextEdit{{Start: g.off(body.Lbrace) + 1, End: g.off(body.Rbrace), NewText: stmt}}
		if i == 0 {
			es = append(es, edit.TextEdit{Start: g.off(g.decl.End()), End: g.off(g.decl.End()), NewText: "\n\n" + text})
		}
		edits = append(edits, edit.FileEdit{Path: g.path, Edits: es})
	}
	return &refactor.Plan{
		Summary: fmt.Sprintf("made %s calls of a new %s(..., %s)", strings.Join(a.Functions, ", "), a.Name, strings.Join(added, ", ")),
		Edits:   edit.Merge(edits),
	}, nil
}
