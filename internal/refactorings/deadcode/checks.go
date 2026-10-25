package deadcode

import (
	"cmp"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// target is the declaration to delete: decl, or spec within it when decl
// groups several specs.
type target struct {
	obj  types.Object
	pkg  *packages.Package
	file *ast.File
	path string
	decl ast.Decl
	spec ast.Spec
	// group is what goes with it, itself included: a use inside one of
	// them does not count.
	group []*target
	// For a field: its declaration, the struct type it is in, and that
	// type's name.
	field    *ast.Field
	strct    *ast.StructType
	typeName types.Object
}

// node is what goes: the spec, or the whole declaration.
func (t *target) node() ast.Node {
	if t.field != nil {
		return t.field
	}
	if t.spec != nil {
		return t.spec
	}
	return t.decl
}

// find locates the declaration of the top-level object at the position.
func find(ws *workspace.Workspace, pkg *packages.Package, obj types.Object) (*target, error) {
	path := ws.Fset.Position(obj.Pos()).Filename
	for _, f := range pkg.Syntax {
		if ws.Fset.File(f.Pos()).Name() != path {
			continue
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Name.Pos() == obj.Pos() {
					return &target{obj: obj, pkg: pkg, file: f, path: path, decl: d}, nil
				}
			case *ast.GenDecl:
				for _, s := range d.Specs {
					var names []*ast.Ident
					switch s := s.(type) {
					case *ast.TypeSpec:
						names = []*ast.Ident{s.Name}
					case *ast.ValueSpec:
						names = s.Names
					}
					for _, n := range names {
						if n.Pos() != obj.Pos() {
							continue
						}
						t := &target{obj: obj, pkg: pkg, file: f, path: path, decl: d}
						if len(d.Specs) > 1 {
							t.spec = s
						}
						return t, nil
					}
				}
			}
		}
	}
	return nil, refactor.Refusef("%s (at %s) is not a top-level declaration", obj.Name(), ws.Position(obj.Pos()))
}

// check refuses a declaration whose deletion could change something.
func (t *target) check(ws *workspace.Workspace) error {
	name := t.obj.Name()
	inTest := strings.HasSuffix(t.path, "_test.go")
	fn, isFunc := t.obj.(*types.Func)
	method := isFunc && fn.Signature().Recv() != nil
	if t.field != nil {
		return t.checkField(ws)
	}
	switch {
	case name == "_":
		return refactor.Refusef("a blank declaration checks something at compile time (like var _ I = (*T)(nil)); deleting it would drop the check")
	case !method && name == "init":
		return refactor.Refusef("init runs without being called; deleting it would change what the program does")
	case !method && name == "main" && t.pkg.Name == "main":
		return refactor.Refusef("main is the program's entry point")
	case inTest && isFunc && !method && testFunc(name):
		return refactor.Refusef("%s is run by go test, without being called", name)
	}
	if doc := docOf(t); hasDirective(doc) {
		return refactor.Refusef("%s has a //go: or //export directive, which the toolchain reads; deleting it is not supported", name)
	}
	if method {
		if iface, at, found := analysis.InterfaceWithMethod(ws.TypesInfos(), fn.Pkg().Path(), name); found {
			if iface != nil {
				return refactor.Refusef("the interface %s (at %s) has a method %s, which a type could implement through this one", iface.Name(), ws.Position(at), name)
			}
			return refactor.Refusef("the interface at %s has a method %s, which a type could implement through this one", ws.Position(at), name)
		}
		if s := dependencyInterface(ws, fn); s != "" {
			return refactor.Refusef("%s through %s, so code that only sees it as that interface (as fmt.Println calls String, and io.Copy WriteTo) can call it; deleting it would change what that code does", s, name)
		}
	}
	if tn, ok := t.obj.(*types.TypeName); ok {
		if n, ok := tn.Type().(*types.Named); ok && n.NumMethods() > 0 {
			var ms []string
			for i := 0; i < n.NumMethods(); i++ {
				if !t.grouped(n.Method(i)) {
					ms = append(ms, n.Method(i).Name())
				}
			}
			if len(ms) > 0 {
				return refactor.Refusef("%s has methods (%s); remove them first, or with it in also", name, strings.Join(ms, ", "))
			}
		}
	}
	if vs, ok := t.valueSpec(); ok {
		if len(vs.Names) > 1 {
			return refactor.Refusef("%s is declared together with other names (%s); deleting only some of them is not supported", name, names(vs))
		}
		if g, ok := t.decl.(*ast.GenDecl); ok && g.Tok == token.CONST {
			for _, later := range g.Specs[slices.Index(g.Specs, ast.Spec(vs))+1:] {
				if lv := later.(*ast.ValueSpec); analysis.DependsOnPosition(t.pkg.TypesInfo, lv) {
					return refactor.Refusef("%s (at %s) uses iota or repeats an earlier expression, so deleting %s would change its value; delete the later constants too, or give them values of their own", lv.Names[0].Name, ws.Position(lv.Pos()), name)
				}
			}
		}
		for _, v := range vs.Values {
			effect := false
			ast.Inspect(v, func(n ast.Node) bool {
				if _, lit := n.(*ast.FuncLit); lit {
					return false
				}
				call, isCall := n.(*ast.CallExpr)
				effect = effect || analysis.HasEffect(t.pkg.TypesInfo, n) && !(isCall && analysis.ReadOnlyCall(t.pkg.TypesInfo, call))
				return !effect
			})
			if effect {
				return refactor.Refusef("the initializer of %s has side effects, which deleting it would drop", name)
			}
		}
	}
	return t.checkUnused(ws)
}

// apiNote is the note for removing t when it is part of the module's
// API.
func (t *target) apiNote() (string, bool) {
	if !analysis.PublicAPI(t.obj, []*packages.Package{t.pkg}) {
		return "", false
	}
	name := t.obj.Name()
	if t.field != nil {
		if !t.typeName.Exported() {
			return "", false
		}
		name = t.typeName.Name() + "." + name
	}
	return refactor.APINote(name, "removing it"), true
}

// dependencyInterface says which interface, declared by a dependency or
// predeclared (error), the receiver type of fn implements through fn, as
// "*src implements io.WriterTo", or "" if none: code in the dependency can
// call fn without naming it (fmt.Println calls String, io.Copy calls
// WriteTo), so it is not dead.
func dependencyInterface(ws *workspace.Workspace, fn *types.Func) string {
	recv := fn.Signature().Recv().Type()
	if p, ok := recv.(*types.Pointer); ok {
		recv = p.Elem()
	}
	named, ok := recv.(*types.Named)
	if !ok {
		return ""
	}
	noQualifier := func(*types.Package) string { return "" }
	check := func(it *types.Interface, label string) string {
		has := false
		for i := 0; i < it.NumMethods(); i++ {
			m := it.Method(i)
			has = has || m.Name() == fn.Name() && (m.Exported() || m.Pkg() != nil && m.Pkg().Path() == fn.Pkg().Path())
		}
		if !has {
			return ""
		}
		for _, T := range []types.Type{named, types.NewPointer(named)} {
			if types.Implements(T, it) {
				return types.TypeString(T, noQualifier) + " implements " + label
			}
		}
		return ""
	}
	if s := check(types.Universe.Lookup("error").Type().Underlying().(*types.Interface), "error"); s != "" {
		return s
	}
	for _, d := range analysis.Dependencies(ws.Pkgs...) {
		for _, tn := range analysis.PackageInterfaces(d.Types) {
			if s := check(tn.Type().Underlying().(*types.Interface), tn.Type().String()); s != "" {
				return s
			}
		}
	}
	return ""
}

// checkUnused refuses a declaration used anywhere but inside itself,
// naming every use, so that one change can remove them all.
func (t *target) checkUnused(ws *workspace.Workspace) error {
	key := analysis.ObjectKey(ws.Fset, origin(t.obj))
	from, to := t.node().Pos(), t.node().End()
	var used []token.Pos
	for _, p := range ws.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for id, o := range p.TypesInfo.Uses {
			if o == nil || analysis.ObjectKey(ws.Fset, origin(o)) != key {
				continue
			}
			if ws.Fset.Position(id.Pos()).Filename == t.path && from <= id.Pos() && id.Pos() < to {
				continue // inside itself (a recursive call)
			}
			if t.inGroup(ws, id.Pos()) {
				continue // inside what goes with it
			}
			used = append(used, id.Pos())
		}
	}
	if len(used) == 0 {
		return nil
	}
	slices.SortFunc(used, func(a, b token.Pos) int {
		pa, pb := ws.Fset.Position(a), ws.Fset.Position(b)
		return cmp.Or(cmp.Compare(pa.Filename, pb.Filename), cmp.Compare(pa.Line, pb.Line), cmp.Compare(pa.Column, pb.Column))
	})
	where := make([]string, len(used))
	for i, pos := range used {
		where[i] = ws.Position(pos)
	}
	where = slices.Compact(where) // a package and its test variant see the same uses
	if len(where) == 1 {
		return refactor.Refusef("%s is used at %s", t.obj.Name(), where[0])
	}
	return refactor.Refusef("%s is used in %d places: %s; change or remove them first", t.obj.Name(), len(where), strings.Join(where, ", "))
}

// inGroup reports whether pos is inside a declaration of t's group.
func (t *target) inGroup(ws *workspace.Workspace, pos token.Pos) bool {
	file := ws.Fset.Position(pos).Filename
	return slices.ContainsFunc(t.group, func(g *target) bool {
		return g.path == file && g.node().Pos() <= pos && pos < g.node().End()
	})
}

// grouped reports whether obj goes with t, in its group.
func (t *target) grouped(obj types.Object) bool {
	return slices.ContainsFunc(t.group, func(g *target) bool { return g.obj.Pos() == obj.Pos() })
}

func origin(o types.Object) types.Object {
	switch o := o.(type) {
	case *types.Func:
		return o.Origin()
	case *types.Var:
		return o.Origin()
	}
	return o
}

func (t *target) valueSpec() (*ast.ValueSpec, bool) {
	if vs, ok := t.spec.(*ast.ValueSpec); ok {
		return vs, true
	}
	if g, ok := t.decl.(*ast.GenDecl); ok && len(g.Specs) == 1 {
		vs, ok := g.Specs[0].(*ast.ValueSpec)
		return vs, ok
	}
	return nil, false
}

func names(vs *ast.ValueSpec) string {
	var out []string
	for _, n := range vs.Names {
		out = append(out, n.Name)
	}
	return strings.Join(out, ", ")
}

// testFunc reports whether name is one go test runs.
func testFunc(name string) bool {
	for _, p := range []string{"Test", "Benchmark", "Fuzz", "Example"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// docOf returns the comments that belong to the target: its spec's doc,
// and its declaration's.
func docOf(t *target) []*ast.CommentGroup {
	var out []*ast.CommentGroup
	if t.field != nil {
		return []*ast.CommentGroup{t.field.Doc}
	}
	switch d := t.decl.(type) {
	case *ast.FuncDecl:
		out = append(out, d.Doc)
	case *ast.GenDecl:
		out = append(out, d.Doc)
	}
	switch s := t.spec.(type) {
	case *ast.ValueSpec:
		out = append(out, s.Doc)
	case *ast.TypeSpec:
		out = append(out, s.Doc)
	}
	return out
}

func hasDirective(groups []*ast.CommentGroup) bool {
	for _, g := range groups {
		if g == nil {
			continue
		}
		for _, c := range g.List {
			if strings.HasPrefix(c.Text, "//go:") || strings.HasPrefix(c.Text, "//export ") {
				return true
			}
		}
	}
	return false
}

// span returns the bytes to delete: whole lines, from the doc comment (if
// any) to the end of the line the declaration ends on.
func (t *target) span(tf *token.File, src []byte) (int, int) {
	if t.field != nil {
		return t.fieldSpan(tf, src)
	}
	start := t.node().Pos()
	switch n := t.node().(type) {
	case *ast.FuncDecl:
		if n.Doc != nil {
			start = n.Doc.Pos()
		}
	case *ast.GenDecl:
		if n.Doc != nil {
			start = n.Doc.Pos()
		}
	case *ast.ValueSpec:
		if n.Doc != nil {
			start = n.Doc.Pos()
		}
	case *ast.TypeSpec:
		if n.Doc != nil {
			start = n.Doc.Pos()
		}
	}
	from := tf.Offset(tf.LineStart(tf.Line(start)))
	end := tf.Offset(t.node().End())
	if i := strings.IndexByte(string(src[end:]), '\n'); i >= 0 {
		end += i + 1
	} else {
		end = len(src)
	}
	return from, end
}

// findField locates the declaration of field, a field of a struct type
// declared in pkg.
func findField(ws *workspace.Workspace, pkg *packages.Package, field *types.Var) (*target, error) {
	path := ws.Fset.Position(field.Pos()).Filename
	for _, f := range pkg.Syntax {
		if ws.Fset.File(f.Pos()).Name() != path {
			continue
		}
		var t *target
		ast.Inspect(f, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if t != nil || !ok {
				return t == nil
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				return true
			}
			for _, fl := range st.Fields.List {
				if fl.Pos() <= field.Pos() && field.Pos() < fl.End() {
					t = &target{obj: field, pkg: pkg, file: f, path: path, field: fl, strct: st, typeName: pkg.TypesInfo.Defs[ts.Name]}
				}
			}
			return t == nil
		})
		if t != nil {
			return t, nil
		}
	}
	return nil, refactor.Refusef("%s (at %s) is not a field of a declared struct type; only those of a type's own struct are supported", field.Name(), ws.Position(field.Pos()))
}

// checkField refuses a field whose deletion could change something.
func (t *target) checkField(ws *workspace.Workspace) error {
	name, tn := t.obj.Name(), t.typeName.Name()
	switch {
	case name == "_":
		return refactor.Refusef("a blank field changes the layout of %s or whether it can be compared; deleting it would change that", tn)
	case len(t.field.Names) > 1:
		var ns []string
		for _, n := range t.field.Names {
			ns = append(ns, n.Name)
		}
		return refactor.Refusef("%s is declared together with other fields (%s); deleting only some of them is not supported", name, strings.Join(ns, ", "))
	}
	if t.obj.(*types.Var).Embedded() {
		return refactor.Refusef("%s is embedded in %s: the fields and methods it promotes would go with it", name, tn)
	}
	key := analysis.ObjectKey(ws.Fset, t.typeName)
	isT := func(t types.Type) bool {
		n, ok := t.(*types.Named)
		return ok && analysis.ObjectKey(ws.Fset, n.Origin().Obj()) == key
	}
	for _, p := range ws.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		var unkeyed ast.Node
		for _, f := range p.Syntax {
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if ok && unkeyed == nil && len(lit.Elts) > 0 && isT(p.TypesInfo.TypeOf(lit)) {
					if _, kv := lit.Elts[0].(*ast.KeyValueExpr); !kv {
						unkeyed = lit
					}
				}
				return unkeyed == nil
			})
		}
		if unkeyed != nil {
			return refactor.Refusef("an unkeyed literal of %s at %s lists every field; give it keys first", tn, ws.Position(unkeyed.Pos()))
		}
		if arg := analysis.PassedAsInterface(p.TypesInfo, p.Syntax, isT); arg != nil {
			return refactor.Refusef("a value holding %s is passed as an interface at %s; reflection there (fmt's %%v, encoding/json, reflect.DeepEqual) sees its fields, and would no longer see %s", tn, ws.Position(arg.Pos()), name)
		}
	}
	return t.checkUnused(ws)
}

// fieldSpan returns the bytes to delete for a field: its whole lines, with
// its doc and line comments, when nothing else is on them; otherwise the
// field and the separator before the next one or after the previous one.
func (t *target) fieldSpan(tf *token.File, src []byte) (int, int) {
	start, end := t.field.Pos(), t.field.End()
	if t.field.Doc != nil {
		start = t.field.Doc.Pos()
	}
	if t.field.Comment != nil {
		end = t.field.Comment.End()
	}
	from, to := tf.Offset(start), tf.Offset(end)
	lineStart, lineEnd := tf.Offset(tf.LineStart(tf.Line(start))), len(src)
	if i := strings.IndexByte(string(src[to:]), '\n'); i >= 0 {
		lineEnd = to + i + 1
	}
	if strings.TrimSpace(string(src[lineStart:from])) == "" && strings.TrimSpace(string(src[to:lineEnd])) == "" {
		return lineStart, lineEnd
	}
	list := t.strct.Fields.List
	switch i := slices.Index(list, t.field); {
	case i+1 < len(list):
		return from, tf.Offset(list[i+1].Pos())
	case i > 0:
		return tf.Offset(list[i-1].End()), to
	}
	return from, to
}
