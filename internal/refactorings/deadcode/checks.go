package deadcode

import (
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
}

// node is what goes: the spec, or the whole declaration.
func (t *target) node() ast.Node {
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
	switch {
	case name == "_":
		return refactor.Refusef("a blank declaration checks something at compile time (like var _ I = (*T)(nil)); deleting it would drop the check")
	case !method && name == "init":
		return refactor.Refusef("init runs without being called; deleting it would change what the program does")
	case !method && name == "main" && t.pkg.Name == "main":
		return refactor.Refusef("main is the program's entry point")
	case inTest && isFunc && !method && testFunc(name):
		return refactor.Refusef("%s is run by go test, without being called", name)
	case t.obj.Exported() && t.pkg.Name != "main":
		return refactor.Refusef("%s is exported, so code in other modules could use it; this only deletes unexported code (or code in a main package)", name)
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
	}
	if tn, ok := t.obj.(*types.TypeName); ok {
		if n, ok := tn.Type().(*types.Named); ok && n.NumMethods() > 0 {
			var ms []string
			for i := 0; i < n.NumMethods(); i++ {
				ms = append(ms, n.Method(i).Name())
			}
			return refactor.Refusef("%s has methods (%s); remove them first", name, strings.Join(ms, ", "))
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
				effect = effect || analysis.HasEffect(t.pkg.TypesInfo, n)
				return !effect
			})
			if effect {
				return refactor.Refusef("the initializer of %s has side effects, which deleting it would drop", name)
			}
		}
	}
	return t.checkUnused(ws)
}

// checkUnused refuses a declaration used anywhere but inside itself.
func (t *target) checkUnused(ws *workspace.Workspace) error {
	key := analysis.ObjectKey(ws.Fset, origin(t.obj))
	from, to := t.node().Pos(), t.node().End()
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
			return refactor.Refusef("%s is used at %s", t.obj.Name(), ws.Position(id.Pos()))
		}
	}
	return nil
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
