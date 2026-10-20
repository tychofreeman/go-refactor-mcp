// Package extractstruct implements extract_struct: move some fields of a
// struct type into a new struct type, embedded in their place.
//
// Embedding keeps every use compiling with the same meaning: x.f now
// selects the field through the embedded struct. What changes is the
// struct's own shape, which composite literals spell out (they are
// rewritten), and which reflection sees (fmt's %+v, encoding/json), so
// values of the type must not reach an interface.
package extractstruct

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
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// ExtractStruct is the "extract_struct" refactoring.
type ExtractStruct struct{}

// New returns the extract_struct refactoring.
func New() *ExtractStruct { return &ExtractStruct{} }

func (*ExtractStruct) Name() string { return "extract_struct" }

func (*ExtractStruct) Description() string {
	return `Move some fields of a struct type into a new struct type, declared just before it and embedded in their place, so every use (m.path) keeps working through the embedded struct. Composite literals are rewritten: mover{path: p} becomes mover{source: source{path: p}}, point3{1, 2, 3} becomes point3{xy{1, 2}, 3}.
Use it to split a struct that holds two things, before giving the new type its own methods (function_to_method) or its own file or package (move_declaration).
Identify the struct by file, line and name, at its declaration or any use; give fields, the names to move (in any order; they keep the struct's order), and new_type.
Only unexported struct types, and only when no value holding the type (itself, a pointer to it, or a slice, map or struct holding it) is passed to an interface parameter or converted to an interface: reflection (fmt's %+v, encoding/json) would see the new embedded field. Assigning one to an interface variable is not checked.
Refuses a generic struct, an embedded field among those to move, part of a field group (x, y int), a new_type the package already declares or that is a field or method of the struct, a name another embedded field also has (it would become ambiguous), an unkeyed literal when the fields to move are not next to each other, and a multi-line literal with comments between its elements.`
}

func (*ExtractStruct) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":     {"type": "string",  "description": "Path of a file containing the struct type's declaration or a use, relative to the workspace root."},
    "line":     {"type": "integer", "description": "1-based line of that occurrence. Omit it to find name as a symbol of the package of file."},
    "name":     {"type": "string",  "description": "The struct type's name; without line, it is found among the package-level names."},
    "column":   {"type": "integer", "description": "Optional 1-based column, only needed to disambiguate."},
    "fields":   {"type": "array", "items": {"type": "string"}, "description": "The fields to move into the new type."},
    "new_type": {"type": "string",  "description": "Name of the new struct type, which is also the embedded field's name."}
  },
  "required": ["file", "name", "fields", "new_type"],
  "additionalProperties": false
}`)
}

type args struct {
	File    string   `json:"file"`
	Line    int      `json:"line"`
	Column  int      `json:"column,omitempty"`
	Name    string   `json:"name"`
	Fields  []string `json:"fields"`
	NewType string   `json:"new_type"`
}

// extraction is one planning run.
type extraction struct {
	ws      *workspace.Workspace
	pkg     *packages.Package
	tn      *types.TypeName
	gen     *ast.GenDecl
	spec    *ast.TypeSpec
	st      *ast.StructType
	path    string // the declaring file
	src     []byte
	newType string

	moving  map[string]bool // the field names that move
	fields  []*ast.Field    // the fields that move, in order
	flat    []string        // every field name, in order (one per name; embedded by type)
	lo, hi  int             // the moving names' range in flat, if contiguous
	contig  bool
	srcs    map[string][]byte
	litEdit map[string][]edit.TextEdit // per file: literal rewrites so far
}

// Plan checks preconditions and computes the edits.
func (*ExtractStruct) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	switch {
	case !token.IsIdentifier(a.NewType):
		return nil, refactor.Refusef("new_type %q is not a valid Go identifier", a.NewType)
	case len(a.Fields) == 0:
		return nil, refactor.Refusef("fields is required: the names of the fields to move")
	}
	x, err := find(ws, locate.IdentQuery{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name})
	if err != nil {
		return nil, err
	}
	x.newType = a.NewType
	if err := x.selectFields(a.Fields); err != nil {
		return nil, err
	}
	if err := x.checkNames(); err != nil {
		return nil, err
	}
	if err := x.checkNotPassedAsInterface(); err != nil {
		return nil, err
	}
	n, err := x.rewriteLiterals()
	if err != nil {
		return nil, err
	}
	var fes []edit.FileEdit
	for path, es := range x.litEdit {
		fes = append(fes, edit.FileEdit{Path: path, Edits: es})
	}
	fes = append(fes, edit.FileEdit{Path: x.path, Edits: x.declEdits()})
	return &refactor.Plan{
		Summary: fmt.Sprintf("moved fields %s of %s into %s, embedded (%d literal%s updated)",
			strings.Join(a.Fields, ", "), x.tn.Name(), x.newType, n, textutil.Plural(n)),
		Edits: edit.Merge(fes),
	}, nil
}

// find resolves the struct type and its declaration.
func find(ws *workspace.Workspace, q locate.IdentQuery) (*extraction, error) {
	pkg, id, err := locate.Ident(ws, q)
	if err != nil {
		return nil, err
	}
	tn, ok := pkg.TypesInfo.ObjectOf(id).(*types.TypeName)
	if !ok || tn.Pkg() == nil || tn.Parent() != tn.Pkg().Scope() {
		return nil, refactor.Refusef("%s at %s is not a package-level type", id.Name, ws.Position(id.Pos()))
	}
	if tn.Exported() {
		return nil, refactor.Refusef("%s is exported, so code outside this module could see its fields by reflection (fmt's %%+v, encoding/json) or write its literals; only unexported struct types are supported", tn.Name())
	}
	path := ws.Fset.Position(tn.Pos()).Filename
	for _, f := range pkg.Syntax {
		if ws.Fset.File(f.Pos()).Name() != path {
			continue
		}
		for _, d := range f.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.TYPE {
				continue
			}
			for _, s := range g.Specs {
				ts := s.(*ast.TypeSpec)
				if pkg.TypesInfo.Defs[ts.Name] != tn {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				switch {
				case ts.TypeParams != nil:
					return nil, refactor.Refusef("%s has type parameters; this is not supported yet", tn.Name())
				case ts.Assign.IsValid() || !ok:
					return nil, refactor.Refusef("%s is not declared as a struct type", tn.Name())
				}
				src, err := os.ReadFile(path)
				if err != nil {
					return nil, err
				}
				return &extraction{ws: ws, pkg: pkg, tn: tn, gen: g, spec: ts, st: st, path: path, src: src,
					srcs: map[string][]byte{path: src}, litEdit: map[string][]edit.TextEdit{}}, nil
			}
		}
	}
	return nil, refactor.Refusef("the declaration of %s is not in a loaded package", tn.Name())
}

// selectFields finds the fields to move.
func (x *extraction) selectFields(names []string) error {
	x.moving = map[string]bool{}
	for _, n := range names {
		x.moving[n] = true
	}
	found := map[string]bool{}
	var idx []int
	for _, f := range x.st.Fields.List {
		if len(f.Names) == 0 {
			name := embeddedName(f.Type)
			if x.moving[name] {
				return refactor.Refusef("%s is an embedded field; moving embedded fields is not supported yet", name)
			}
			x.flat = append(x.flat, name)
			continue
		}
		var in, out []string
		for _, n := range f.Names {
			if x.moving[n.Name] {
				in = append(in, n.Name)
				found[n.Name] = true
				idx = append(idx, len(x.flat))
			} else {
				out = append(out, n.Name)
			}
			x.flat = append(x.flat, n.Name)
		}
		switch {
		case len(in) > 0 && len(out) > 0:
			return refactor.Refusef("%s is declared together with %s; extract all of them or none", strings.Join(in, ", "), strings.Join(out, ", "))
		case len(in) > 0:
			x.fields = append(x.fields, f)
		}
	}
	for _, n := range names {
		if !found[n] {
			return refactor.Refusef("%s has no field %s", x.tn.Name(), n)
		}
	}
	x.lo, x.hi = idx[0], idx[len(idx)-1]
	x.contig = x.hi-x.lo+1 == len(idx)
	return nil
}

func embeddedName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return embeddedName(t.X)
	case *ast.SelectorExpr:
		return t.Sel.Name
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return embeddedName(t.X)
	case *ast.IndexListExpr:
		return embeddedName(t.X)
	}
	return ""
}

// checkNames refuses a new type name that is taken, and a moving field
// that another embedded field also has: one level deeper, it would tie
// with that one, and the selector would be ambiguous.
func (x *extraction) checkNames() error {
	for _, p := range x.ws.Pkgs {
		if p.Types == nil || p.PkgPath != x.pkg.PkgPath {
			continue
		}
		if o := p.Types.Scope().Lookup(x.newType); o != nil {
			return refactor.Refusef("the package already declares %s (at %s)", x.newType, x.ws.Position(o.Pos()))
		}
	}
	if o, idx, _ := types.LookupFieldOrMethod(types.NewPointer(x.tn.Type()), true, x.pkg.Types, x.newType); o != nil || idx != nil {
		return refactor.Refusef("%s already has a field or method %s, the name the embedded field would have", x.tn.Name(), x.newType)
	}
	st := x.tn.Type().Underlying().(*types.Struct)
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		if !f.Embedded() {
			continue
		}
		for n := range x.moving {
			if o, idx, _ := types.LookupFieldOrMethod(f.Type(), true, x.pkg.Types, n); o != nil || idx != nil {
				return refactor.Refusef("the embedded %s also has %s, which would then be ambiguous (both one level down)", f.Name(), n)
			}
		}
	}
	return nil
}

// isT reports whether t is the struct type or a pointer to it, in any
// package variant.
func (x *extraction) isT(t types.Type) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, ok := t.(*types.Named)
	return ok && sameObj(x.ws.Fset, n.Obj(), x.tn)
}

func sameObj(fset *token.FileSet, a, b types.Object) bool {
	pa, pb := fset.Position(a.Pos()), fset.Position(b.Pos())
	return pa == pb && a.Name() == b.Name()
}

// checkNotPassedAsInterface refuses a value holding the type (itself, or
// in a pointer, slice, map or field) passed to an interface parameter or
// converted to an interface: what reflection sees there would change.
func (x *extraction) checkNotPassedAsInterface() error {
	match := func(t types.Type) bool {
		n, ok := t.(*types.Named)
		return ok && sameObj(x.ws.Fset, n.Obj(), x.tn)
	}
	for _, p := range x.ws.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		arg := analysis.PassedAsInterface(p.TypesInfo, p.Syntax, match)
		if arg == nil {
			continue
		}
		what := "a value holding " + x.tn.Name()
		if x.isT(p.TypesInfo.TypeOf(arg)) {
			what = "a " + x.tn.Name()
		}
		return refactor.Refusef("%s is passed as an interface at %s; reflection there (fmt's %%+v, encoding/json, reflect) sees the struct's fields, and would see the new embedded one", what, x.ws.Position(arg.Pos()))
	}
	return nil
}

// lit is a composite literal of the struct type.
type lit struct {
	path string
	pkg  *packages.Package
	expr *ast.CompositeLit
}

// rewriteLiterals rewrites every composite literal of the type that sets a
// moving field, innermost first so that an outer literal carries the
// rewritten inner ones. It returns how many changed.
func (x *extraction) rewriteLiterals() (int, error) {
	var lits []lit
	seen := map[string]bool{}
	for _, p := range x.ws.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, f := range p.Syntax {
			path := x.ws.Fset.File(f.Pos()).Name()
			ast.Inspect(f, func(n ast.Node) bool {
				cl, ok := n.(*ast.CompositeLit)
				if !ok || len(cl.Elts) == 0 {
					return true
				}
				if t := p.TypesInfo.TypeOf(cl); t == nil || !x.isT(t) || analysis.IsPointer(t) {
					return true
				}
				k := fmt.Sprintf("%s:%d", path, x.ws.Fset.Position(cl.Pos()).Offset)
				if !seen[k] {
					seen[k] = true
					lits = append(lits, lit{path: path, pkg: p, expr: cl})
				}
				return true
			})
		}
	}
	sort.Slice(lits, func(i, j int) bool {
		a, b := lits[i].expr, lits[j].expr
		return a.End()-a.Pos() < b.End()-b.Pos()
	})
	n := 0
	for _, l := range lits {
		changed, err := x.rewrite(l)
		if err != nil {
			return 0, err
		}
		if changed {
			n++
		}
	}
	return n, nil
}

// rewrite rewrites one literal, if it sets a moving field.
func (x *extraction) rewrite(l lit) (bool, error) {
	src, err := x.source(l.path)
	if err != nil {
		return false, err
	}
	cl := l.expr
	text := func(e ast.Node) string { return x.textWithEdits(l.path, src, e) }
	var elts []string
	if _, keyed := cl.Elts[0].(*ast.KeyValueExpr); keyed {
		var group []string
		at := -1
		for _, e := range cl.Elts {
			kv := e.(*ast.KeyValueExpr)
			if id, ok := kv.Key.(*ast.Ident); ok && x.moving[id.Name] {
				if at < 0 {
					at = len(elts)
					elts = append(elts, "")
				}
				group = append(group, text(kv))
				continue
			}
			elts = append(elts, text(kv))
		}
		if at < 0 {
			return false, nil
		}
		elts[at] = x.newType + ": " + x.newType + "{" + strings.Join(group, ", ") + "}"
	} else {
		if !x.contig {
			var names []string
			for _, f := range x.fields {
				for _, n := range f.Names {
					names = append(names, n.Name)
				}
			}
			return false, refactor.Refusef("the unkeyed literal at %s lists the fields in order, and %s are not next to each other; give it keys first, or move fields that are",
				x.ws.Position(cl.Pos()), joinAnd(names))
		}
		var group []string
		for i, e := range cl.Elts {
			switch {
			case i < x.lo || i > x.hi:
				elts = append(elts, text(e))
			case i == x.lo:
				elts = append(elts, "")
				fallthrough
			default:
				group = append(group, text(e))
			}
		}
		elts[x.lo] = x.newType + "{" + strings.Join(group, ", ") + "}"
	}

	tf := x.ws.Fset.File(cl.Pos())
	if tf.Line(cl.Lbrace) == tf.Line(cl.Rbrace) {
		x.replace(l.path, src, cl.Lbrace, cl.Rbrace+1, "{"+strings.Join(elts, ", ")+"}")
		return true, nil
	}
	for _, cg := range fileOf(l.pkg, l.path).Comments {
		if cg.Pos() > cl.Lbrace && cg.End() < cl.Rbrace && !insideAny(cg, cl.Elts) {
			return false, refactor.Refusef("the literal at %s has comments between its elements, which rewriting it would lose; this is not supported yet", x.ws.Position(cl.Pos()))
		}
	}
	indent := lineIndent(src, tf.Offset(tf.LineStart(tf.Line(cl.Elts[0].Pos()))))
	closing := lineIndent(src, tf.Offset(tf.LineStart(tf.Line(cl.Rbrace))))
	var b strings.Builder
	b.WriteString("{\n")
	for _, e := range elts {
		b.WriteString(indent + e + ",\n")
	}
	b.WriteString(closing + "}")
	x.replace(l.path, src, cl.Lbrace, cl.Rbrace+1, b.String())
	return true, nil
}

// textWithEdits returns n's source with the literal rewrites inside it
// applied, and drops those rewrites, which n's replacement now carries.
func (x *extraction) textWithEdits(path string, src []byte, n ast.Node) string {
	start, end := x.off(n.Pos()), x.off(n.End())
	var inner, rest []edit.TextEdit
	for _, e := range x.litEdit[path] {
		if start <= e.Start && e.End <= end {
			inner = append(inner, edit.TextEdit{Start: e.Start - start, End: e.End - start, NewText: e.NewText})
		} else {
			rest = append(rest, e)
		}
	}
	x.litEdit[path] = rest
	out, _ := edit.Apply(src[start:end], inner) // rewrites of disjoint literals
	return string(out)
}

func (x *extraction) replace(path string, src []byte, from, to token.Pos, text string) {
	x.litEdit[path] = append(x.litEdit[path], edit.TextEdit{Start: x.off(from), End: x.off(to), NewText: text})
}

func (x *extraction) source(path string) ([]byte, error) {
	if s, ok := x.srcs[path]; ok {
		return s, nil
	}
	s, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	x.srcs[path] = s
	return s, nil
}

// declEdits move the fields out of the struct into the new type, declared
// just before the struct's declaration, and embed it in their place.
func (x *extraction) declEdits() []edit.TextEdit {
	tf := x.ws.Fset.File(x.st.Pos())
	declStart := x.gen.Pos()
	if x.gen.Lparen.IsValid() {
		declStart = x.spec.Pos()
		if x.spec.Doc != nil {
			declStart = x.spec.Doc.Pos()
		}
	} else if x.gen.Doc != nil {
		declStart = x.gen.Doc.Pos()
	}
	at := x.off(tf.LineStart(tf.Line(declStart)))
	declIndent := lineIndent(x.src, at)
	kw := "type " // not inside a type ( ... ) group
	if x.gen.Lparen.IsValid() {
		kw = ""
	}

	if tf.Line(x.st.Fields.Opening) == tf.Line(x.st.Fields.Closing) {
		// type point struct{ x, y int }: rebuild both on one line.
		var kept, moved []string
		for _, f := range x.st.Fields.List {
			t := string(x.src[x.off(f.Pos()):x.off(f.End())])
			if x.isMoving(f) {
				if len(moved) == 0 {
					kept = append(kept, x.newType)
				}
				moved = append(moved, t)
			} else {
				kept = append(kept, t)
			}
		}
		return []edit.TextEdit{
			{Start: at, End: at, NewText: declIndent + kw + x.newType + " struct{ " + strings.Join(moved, "; ") + " }\n\n"},
			{Start: x.off(x.st.Fields.Opening), End: x.off(x.st.Fields.Closing) + 1, NewText: "{ " + strings.Join(kept, "; ") + " }"},
		}
	}

	var lines strings.Builder
	var es []edit.TextEdit
	for i, f := range x.fields {
		start := f.Pos()
		if f.Doc != nil {
			start = f.Doc.Pos()
		}
		from := x.off(tf.LineStart(tf.Line(start)))
		to := nextLine(x.src, x.off(f.End()))
		lines.Write(x.src[from:to])
		text := ""
		if i == 0 {
			text = lineIndent(x.src, x.off(tf.LineStart(tf.Line(f.Pos())))) + x.newType + "\n"
		}
		es = append(es, edit.TextEdit{Start: from, End: to, NewText: text})
	}
	decl := declIndent + kw + x.newType + " struct {\n" + lines.String() + declIndent + "}\n\n"
	return append(es, edit.TextEdit{Start: at, End: at, NewText: decl})
}

func (x *extraction) isMoving(f *ast.Field) bool {
	for _, m := range x.fields {
		if m == f {
			return true
		}
	}
	return false
}

func (x *extraction) off(p token.Pos) int { return x.ws.Fset.Position(p).Offset }

func fileOf(p *packages.Package, path string) *ast.File {
	for _, f := range p.Syntax {
		if p.Fset.File(f.Pos()).Name() == path {
			return f
		}
	}
	return &ast.File{}
}

func insideAny(n ast.Node, elts []ast.Expr) bool {
	for _, e := range elts {
		if e.Pos() <= n.Pos() && n.End() <= e.End() {
			return true
		}
	}
	return false
}

// lineIndent returns the whitespace that starts the line at offset start.
func lineIndent(src []byte, start int) string {
	i := start
	for i < len(src) && (src[i] == ' ' || src[i] == '\t') {
		i++
	}
	return string(src[start:i])
}

// nextLine returns the offset just past the newline that ends off's line.
func nextLine(src []byte, off int) int {
	if i := strings.IndexByte(string(src[off:]), '\n'); i >= 0 {
		return off + i + 1
	}
	return len(src)
}

func joinAnd(names []string) string {
	if len(names) <= 1 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
