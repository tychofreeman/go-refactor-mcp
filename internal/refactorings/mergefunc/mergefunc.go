// Package mergefunc implements merge_duplicate_function: replace every use
// of a function with an identical one, and delete it.
//
// Two functions are identical when their signatures are and their bodies
// match node for node, ignoring comments and layout: parameters and locals
// may have other names (consistently), and every other name must mean the
// same object in both. Then a call of one does exactly what a call of the
// other does, so every use can switch.
package mergefunc

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
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

// MergeDuplicate is the "merge_duplicate_function" refactoring.
type MergeDuplicate struct{ refactor.Named[args] }

// New returns the merge_duplicate_function refactoring.
func New() *MergeDuplicate {
	return &MergeDuplicate{Named: refactor.Called[args]("merge_duplicate_function")}
}

func (*MergeDuplicate) Description() string {
	return `Replace every use of a function with an identical one, and delete it: with func trim(s string) string { return strings.TrimSpace(s) } and func clean(t string) string { return strings.TrimSpace(t) }, merging trim into clean turns trim(x) into clean(x) everywhere and deletes trim. Identical means the same signature and bodies that match node for node; parameters and locals may be named differently, every other name must mean the same. The refusal shows the first difference.
Identify the function to delete by file and name, or by line at its declaration or any use, and the one to keep by keep_file (any file of its package) and keep_name.
Refuses, saying why: methods, generic functions, a function to keep that a use cannot reach or could import only through an import cycle.`
}

type args struct {
	File     string `json:"file" desc:"Path of a file containing the function to delete, at its declaration or a use, relative to the workspace root."`
	Line     int    `json:"line,omitempty" desc:"1-based line of that occurrence. Omit it to find name as a symbol of the package of file."`
	Name     string `json:"name" desc:"The name of the function to delete; without line, the package-level name or Type.Method."`
	Column   int    `json:"column,omitempty" desc:"Optional 1-based column, only needed to disambiguate."`
	KeepFile string `json:"keep_file" desc:"A file of the package declaring the function to keep."`
	KeepName string `json:"keep_name" desc:"The name of the function to keep."`
}

// fn is one of the two functions.
type fn struct {
	obj  *types.Func
	pkg  *packages.Package // a variant declaring it
	decl *ast.FuncDecl
	path string
	src  []byte // the file's contents
}

// Plan checks preconditions and computes the edits.
func (*MergeDuplicate) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	pkg, id, err := locate.Ident(ws, locate.IdentQuery{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name})
	if err != nil {
		return nil, err
	}
	dup, err := funcOf(ws, pkg.TypesInfo.ObjectOf(id), a.Name)
	if err != nil {
		return nil, err
	}
	kpkg, kfile := ws.FileSyntax(ws.Abs(a.KeepFile))
	if kfile == nil {
		return nil, refactor.Refusef("%s is not part of the loaded packages", a.KeepFile)
	}
	keep, err := funcOf(ws, kpkg.Types.Scope().Lookup(a.KeepName), a.KeepName)
	if err != nil {
		return nil, err
	}
	switch {
	case analysis.ObjectKey(ws.Fset, dup.obj) == analysis.ObjectKey(ws.Fset, keep.obj):
		return nil, refactor.Refusef("%s and %s are the same function", a.Name, a.KeepName)
	}
	if err := sameSignature(dup.obj, keep.obj); err != nil {
		return nil, err
	}
	if err := compareBodies(ws, dup, keep); err != nil {
		return nil, err
	}
	return replaceUses(ws, dup, keep)
}

// funcOf checks that o is a package-level, non-generic function declared
// in the workspace, and finds its declaration.
func funcOf(ws *workspace.Workspace, o types.Object, name string) (*fn, error) {
	f, ok := o.(*types.Func)
	switch {
	case o == nil:
		return nil, refactor.Refusef("the package declares no %s", name)
	case !ok:
		return nil, refactor.Refusef("%s is not a function", name)
	case f.Signature().Recv() != nil:
		return nil, refactor.Refusef("%s is a method; merging methods is not supported", name)
	case f.Signature().TypeParams().Len() > 0:
		return nil, refactor.Refusef("%s has type parameters; this is not supported yet", name)
	}
	path := ws.Fset.Position(f.Pos()).Filename
	if !ws.Contains(path) {
		return nil, refactor.Refusef("%s is declared outside the workspace", name)
	}
	for _, p := range ws.Pkgs {
		for _, file := range p.Syntax {
			for _, d := range file.Decls {
				if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && p.TypesInfo.Defs[fd.Name] != nil && analysis.ObjectKey(ws.Fset, p.TypesInfo.Defs[fd.Name]) == analysis.ObjectKey(ws.Fset, f) {
					if fd.Body == nil {
						return nil, refactor.Refusef("%s has no body in Go source", name)
					}
					src, err := os.ReadFile(path)
					if err != nil {
						return nil, err
					}
					return &fn{obj: f, pkg: p, decl: fd, path: path, src: src}, nil
				}
			}
		}
	}
	return nil, refactor.Refusef("the declaration of %s is not in a loaded package", name)
}

// sameSignature refuses functions whose types differ (comparing by package
// path, so that a package's variants agree).
func sameSignature(a, b *types.Func) error {
	q := func(p *types.Package) string { return p.Path() }
	strip := func(s *types.Signature) string {
		var ps, rs []string
		for i := 0; i < s.Params().Len(); i++ {
			ps = append(ps, types.TypeString(s.Params().At(i).Type(), q))
		}
		for i := 0; i < s.Results().Len(); i++ {
			rs = append(rs, types.TypeString(s.Results().At(i).Type(), q))
		}
		return fmt.Sprintf("%v %v %v", ps, rs, s.Variadic())
	}
	if strip(a.Signature()) != strip(b.Signature()) {
		rel := func(p *types.Package) string { return p.Name() }
		return refactor.Refusef("%s has type %s, and %s has type %s", a.Name(), types.TypeString(a.Type(), rel), b.Name(), types.TypeString(b.Type(), rel))
	}
	return nil
}

// tok is one step of a function's canonical form.
type tok struct {
	key  string    // what must match
	pos  token.Pos // where, for a message
	show string    // how to name it in a message
	obj  types.Object
}

// canon is the function's signature and body as a sequence of tokens: each
// node's kind, operator and optional parts, the end of each node, literals
// by value, and names by what they mean. Its own locals (and parameters) are numbered in
// order of appearance, so their names do not matter; a call of itself is
// "self".
func canon(ws *workspace.Workspace, f *fn) []tok {
	info := f.pkg.TypesInfo
	locals := map[types.Object]int{}
	var out []tok
	var stack []ast.Node
	visit := func(n ast.Node) bool {
		if n == nil {
			done := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			out = append(out, tok{key: ")", pos: done.End() - 1, show: "the end of " + analysis.Describe(done)})
			return true
		}
		if _, ok := n.(*ast.CommentGroup); ok {
			return false
		}
		stack = append(stack, n)
		t := tok{key: fmt.Sprintf("%T", n), pos: n.Pos(), show: analysis.Describe(n) + " (" + f.excerpt(ws, n) + ")"}
		switch n := n.(type) {
		case *ast.BasicLit:
			t.key += " " + n.Value
			t.show = n.Value
		case *ast.BinaryExpr:
			t.key += " " + n.Op.String()
		case *ast.UnaryExpr:
			t.key += " " + n.Op.String()
		case *ast.AssignStmt:
			t.key += " " + n.Tok.String()
		case *ast.IncDecStmt:
			t.key += " " + n.Tok.String()
		case *ast.BranchStmt:
			t.key += " " + n.Tok.String()
		case *ast.RangeStmt:
			t.key += " " + n.Tok.String()
		case *ast.GenDecl:
			t.key += " " + n.Tok.String()
		case *ast.ChanType:
			t.key += fmt.Sprint(" ", n.Dir)
		// Optional children of one node give the same tokens wherever they
		// stand (s[:n] and s[n:]), so which ones are there is part of the key.
		case *ast.SliceExpr:
			t.key += fmt.Sprintf(" low=%t high=%t max=%t", n.Low != nil, n.High != nil, n.Max != nil)
		case *ast.ForStmt:
			t.key += fmt.Sprintf(" init=%t cond=%t post=%t", n.Init != nil, n.Cond != nil, n.Post != nil)
		case *ast.CallExpr:
			if n.Ellipsis.IsValid() {
				t.key += " ..." // append(xs, ys...), not append(xs, ys)
			}
		case *ast.Ellipsis:
		case *ast.Ident:
			t.show = n.Name
			o := info.ObjectOf(n)
			t.obj = o
			switch {
			case o == nil:
				t.key += " ?" + n.Name
			case analysis.ObjectKey(ws.Fset, o) == analysis.ObjectKey(ws.Fset, f.obj):
				t.key += " self"
			case o.Pos().IsValid() && f.decl.Pos() <= o.Pos() && o.Pos() < f.decl.End():
				i, ok := locals[o]
				if !ok {
					i = len(locals)
					locals[o] = i
				}
				t.key += fmt.Sprintf(" local%d", i)
			case o.Parent() == types.Universe:
				t.key += " " + n.Name
			default:
				if pn, ok := o.(*types.PkgName); ok {
					t.key += " package " + pn.Imported().Path()
				} else {
					t.key += " " + analysis.ObjectKey(ws.Fset, o)
				}
			}
		}
		out = append(out, t)
		return true
	}
	ast.Inspect(f.decl.Type, visit)
	ast.Inspect(f.decl.Body, visit)
	return out
}

// excerpt returns the start of n's source, for a message: its first line,
// shortened if long.
func (f *fn) excerpt(ws *workspace.Workspace, n ast.Node) string {
	start, end := ws.Fset.Position(n.Pos()).Offset, ws.Fset.Position(n.End()).Offset
	s := string(f.src[start:end])
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if len(s) > 30 {
		s = s[:27] + "..."
	}
	return s
}

// compareBodies refuses functions whose canonical forms differ, naming
// the first difference.
func compareBodies(ws *workspace.Workspace, dup, keep *fn) error {
	a, b := canon(ws, dup), canon(ws, keep)
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i].key == b[i].key {
			continue
		}
		x, y := a[i], b[i]
		if x.obj != nil && y.obj != nil && x.show == y.show && !strings.HasSuffix(x.key, " self") {
			return refactor.Refusef("the bodies differ: %s at %s is the %s declared at %s, where %s has the %s declared at %s",
				x.show, ws.Position(x.pos), kind(x.obj), ws.Position(x.obj.Pos()), keep.obj.Name(), kind(y.obj), ws.Position(y.obj.Pos()))
		}
		return refactor.Refusef("the bodies differ: %s at %s where %s has %s at %s", x.show, ws.Position(x.pos), keep.obj.Name(), y.show, ws.Position(y.pos))
	}
	if len(a) != len(b) {
		return refactor.Refusef("the bodies differ in length: %s and %s do not match", dup.obj.Name(), keep.obj.Name())
	}
	return nil
}

func kind(o types.Object) string {
	switch o.(type) {
	case *types.Func:
		return "function"
	case *types.Var:
		return "variable"
	case *types.Const:
		return "constant"
	case *types.TypeName:
		return "type"
	case *types.PkgName:
		return "import"
	}
	return "name"
}

// replaceUses switches every use of dup to keep, and deletes dup with the
// imports only it used.
func replaceUses(ws *workspace.Workspace, dup, keep *fn) (*refactor.Plan, error) {
	type fileEdits struct {
		adder *imports.Adder
		edits []edit.TextEdit
	}
	files := map[string]*fileEdits{}
	fileFor := func(p *packages.Package, path string) (*fileEdits, error) {
		if fe := files[path]; fe != nil {
			return fe, nil
		}
		_, f := ws.FileSyntax(path)
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		for _, file := range p.Syntax {
			if ws.Fset.File(file.Pos()).Name() == path {
				f = file
			}
		}
		fe := &fileEdits{adder: imports.New(ws, p, f, src)}
		files[path] = fe
		return fe, nil
	}
	off := func(p token.Pos) int { return ws.Fset.Position(p).Offset }
	deps := dependencies(keep.pkg)
	seen := map[string]bool{}
	refs := 0
	for _, p := range ws.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for id, o := range p.TypesInfo.Uses {
			if o == nil || analysis.ObjectKey(ws.Fset, o) != analysis.ObjectKey(ws.Fset, dup.obj) {
				continue
			}
			pos := ws.Fset.Position(id.Pos())
			if pos.Filename == dup.path && dup.decl.Pos() <= id.Pos() && id.Pos() < dup.decl.End() {
				continue // inside dup, which goes
			}
			k := fmt.Sprintf("%s:%d", pos.Filename, pos.Offset)
			if seen[k] {
				continue
			}
			seen[k] = true
			where := ws.Position(id.Pos())
			scope := p.Types.Scope().Innermost(id.Pos())
			lookup := func(name string) types.Object {
				if scope == nil {
					return nil
				}
				_, o := scope.LookupParent(name, id.Pos())
				return o
			}
			text := keep.obj.Name()
			if p.PkgPath == keep.pkg.PkgPath {
				if o := lookup(text); o == nil || analysis.ObjectKey(ws.Fset, o) != analysis.ObjectKey(ws.Fset, keep.obj) {
					return nil, shadowed(ws, where, text, o)
				}
			} else {
				if !keep.obj.Exported() {
					return nil, refactor.Refusef("%s in package %s is unexported, so package %s cannot call it", keep.obj.Name(), keep.pkg.Name, p.Name)
				}
				if deps[p.PkgPath] {
					return nil, refactor.Refusef("package %s would import %s, which already depends on it, an import cycle", p.PkgPath, keep.pkg.PkgPath)
				}
				fe, err := fileFor(p, pos.Filename)
				if err != nil {
					return nil, err
				}
				q, err := fe.adder.Name(keep.obj.Pkg())
				if err != nil {
					return nil, refactor.Refusef("%s cannot import %s: %v", ws.Rel(pos.Filename), keep.pkg.PkgPath, err)
				}
				if o := lookup(q); o != nil {
					if _, isPkg := o.(*types.PkgName); !isPkg {
						return nil, shadowed(ws, where, q, o)
					}
				}
				text = q + "." + text
			}
			fe, err := fileFor(p, pos.Filename)
			if err != nil {
				return nil, err
			}
			fe.edits = append(fe.edits, edit.TextEdit{Start: off(id.Pos()), End: off(id.End()), NewText: text})
			refs++
		}
	}

	// Delete dup, and the imports of its file that only it used.
	fe, err := fileFor(dup.pkg, dup.path)
	if err != nil {
		return nil, err
	}
	src, err := os.ReadFile(dup.path)
	if err != nil {
		return nil, err
	}
	start := off(dup.decl.Pos())
	if dup.decl.Doc != nil {
		start = off(dup.decl.Doc.Pos())
	}
	start = strings.LastIndexByte(string(src[:start]), '\n') + 1
	end := off(dup.decl.End())
	if i := strings.IndexByte(string(src[end:]), '\n'); i >= 0 {
		end += i + 1
	} else {
		end = len(src)
	}
	fe.edits = append(fe.edits, edit.TextEdit{Start: start, End: end})
	var file *ast.File
	for _, f := range dup.pkg.Syntax {
		if ws.Fset.File(f.Pos()).Name() == dup.path {
			file = f
		}
	}
	for _, imp := range file.Imports {
		pn := pkgNameOf(dup.pkg.TypesInfo, imp)
		if pn == nil {
			continue
		}
		inside, outside := false, false
		for id, o := range dup.pkg.TypesInfo.Uses {
			if o != pn || id.Pos() < file.FileStart || id.Pos() >= file.FileEnd {
				continue
			}
			if dup.decl.Pos() <= id.Pos() && id.Pos() < dup.decl.End() {
				inside = true
			} else {
				outside = true
			}
		}
		if inside && !outside {
			fe.adder.Remove(imp)
		}
	}

	var fes []edit.FileEdit
	for path, fe := range files {
		fes = append(fes, edit.FileEdit{Path: path, Edits: append(fe.edits, fe.adder.Edits()...)})
	}
	plan := &refactor.Plan{
		Summary: fmt.Sprintf("replaced %s with %s.%s (%d reference%s updated) and deleted %s",
			dup.obj.Name(), keep.pkg.Name, keep.obj.Name(), refs, textutil.Plural(refs), dup.obj.Name()),
		Edits: edit.Merge(fes),
	}
	if analysis.PublicAPI(dup.obj, ws.Pkgs) {
		plan.Notes = append(plan.Notes, refactor.APINote(dup.obj.Name(), "deleting it"))
	}
	return plan, nil
}

func shadowed(ws *workspace.Workspace, where, name string, o types.Object) error {
	if o == nil {
		return refactor.Refusef("at %s, %s is not visible", where, name)
	}
	return refactor.Refusef("at %s, %s is the %s declared at %s; rename it first", where, name, kind(o), ws.Position(o.Pos()))
}

// dependencies returns the paths of the packages p depends on.
func dependencies(p *packages.Package) map[string]bool {
	deps := map[string]bool{}
	var walk func(*packages.Package)
	walk = func(p *packages.Package) {
		for path, d := range p.Imports {
			if !deps[path] {
				deps[path] = true
				walk(d)
			}
		}
	}
	walk(p)
	return deps
}

// pkgNameOf returns the name an import declares (nil for a blank or dot
// import).
func pkgNameOf(info *types.Info, imp *ast.ImportSpec) *types.PkgName {
	var o types.Object
	if imp.Name != nil {
		o = info.Defs[imp.Name]
	} else {
		o = info.Implicits[imp]
	}
	pn, _ := o.(*types.PkgName)
	if pn != nil && (pn.Name() == "_" || pn.Name() == ".") {
		return nil
	}
	return pn
}
