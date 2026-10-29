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
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/site"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// ExtractStruct is the "extract_struct" refactoring.
type ExtractStruct struct{}

// New returns the extract_struct refactoring.
func New() *ExtractStruct { return &ExtractStruct{} }

func (*ExtractStruct) Name() string { return "extract_struct" }

func (*ExtractStruct) Description() string {
	return `Move some fields of a struct into a new struct type, embedded in their place, so that every use (m.path) keeps working: moving path and mode of type mover struct { path string; mode int; n int } into source declares type source struct { path string; mode int } and makes mover struct { source; n int }, and mover{path: p} becomes mover{source: source{path: p}}. To hold it as a named field instead, then rename mover.source (to src: m.path becomes m.src.path). Use it to split a struct that holds two things, before giving the new type methods or a file of its own.
With into in place of new_type, the fields go to the end of a struct that already exists, one the struct embeds or holds in a field by value: moving depth of Bed into Soil, held as soil, makes b.depth b.soil.depth and Bed{depth: 3} Bed{soil: Soil{depth: 3}}.
Identify the struct by file and name, or by line at its declaration or a use; fields are the names to move, new_type the new type's name.
Refuses, saying why: a struct whose values are passed to an interface (reflection, fmt's %+v and encoding/json would see the new field), a generic struct, an embedded field or part of a field group to move, a name already taken, an unkeyed literal whose moved fields are not next to each other. For an exported struct the result notes that the module's API changed.`
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
    "new_type": {"type": "string",  "description": "Name of the new struct type, which is also the embedded field's name."},
    "into":     {"type": "string",  "description": "Instead of new_type: an existing struct type the struct embeds or holds in a field, by value, to move the fields into."}
  },
  "required": ["file", "name", "fields"],
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
	Into    string   `json:"into"`
}

// extraction is one planning run.
type extraction struct {
	site.Site
	tn      *types.TypeName
	gen     *ast.GenDecl
	spec    *ast.TypeSpec
	st      *ast.StructType
	path    string // the declaring file
	newType string
	adders  map[string]*imports.Adder // by file, for literals outside the struct's package

	moving  map[string]bool // the field names that move
	fields  []*ast.Field    // the fields that move, in order
	flat    []string        // every field name, in order (one per name; embedded by type)
	lo, hi  int             // the moving names' range in flat, if contiguous
	contig  bool
	srcs    map[string][]byte
	litEdit map[string][]edit.TextEdit // per file: literal rewrites so far
	into    *into                      // the existing struct the fields move into, or nil for a new one
}

// Plan checks preconditions and computes the edits.
func (*ExtractStruct) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	switch {
	case a.Into != "" && a.NewType != "":
		return nil, refactor.Refusef("give new_type for a new struct, or into for one that exists, not both")
	case a.Into == "" && !token.IsIdentifier(a.NewType):
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
	if a.Into != "" {
		return x.planInto(a)
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
		if a := x.adders[path]; a != nil {
			es = append(es, a.Edits()...)
		}
		fes = append(fes, edit.FileEdit{Path: path, Edits: es})
	}
	fes = append(fes, edit.FileEdit{Path: x.path, Edits: x.declEdits()})
	plan := &refactor.Plan{
		Summary: fmt.Sprintf("moved fields %s of %s into %s, embedded (%d literal%s updated)",
			strings.Join(a.Fields, ", "), x.tn.Name(), x.newType, n, textutil.Plural(n)),
		Edits: edit.Merge(fes),
	}
	if analysis.PublicAPI(x.tn, ws.Pkgs) {
		plan.Notes = append(plan.Notes, refactor.APINote(x.tn.Name(), "moving its fields into "+x.newType)+" (their literals cannot name the moved fields; reflection sees the embedded field)")
	}
	return plan, nil
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
				return &extraction{
					Site:    site.Site{WS: ws, Pkg: pkg, Src: src},
					tn:      tn,
					gen:     g,
					spec:    ts,
					st:      st,
					path:    path,
					srcs:    map[string][]byte{path: src},
					litEdit: map[string][]edit.TextEdit{},
				}, nil
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
	for _, p := range x.WS.Pkgs {
		if p.Types == nil || p.PkgPath != x.Pkg.PkgPath {
			continue
		}
		if o := p.Types.Scope().Lookup(x.newType); o != nil {
			return refactor.Refusef("the package already declares %s (at %s)", x.newType, x.WS.Position(o.Pos()))
		}
	}
	if o, idx, _ := types.LookupFieldOrMethod(types.NewPointer(x.tn.Type()), true, x.Pkg.Types, x.newType); o != nil || idx != nil {
		return refactor.Refusef("%s already has a field or method %s, the name the embedded field would have", x.tn.Name(), x.newType)
	}
	st := x.tn.Type().Underlying().(*types.Struct)
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		if !f.Embedded() {
			continue
		}
		for n := range x.moving {
			if o, idx, _ := types.LookupFieldOrMethod(f.Type(), true, x.Pkg.Types, n); o != nil || idx != nil {
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
	return ok && sameObj(x.WS.Fset, n.Obj(), x.tn)
}

func sameObj(fset *token.FileSet, a, b types.Object) bool {
	pa, pb := fset.Position(a.Pos()), fset.Position(b.Pos())
	return pa == pb && a.Name() == b.Name()
}

// checkNotPassedAsInterface refuses a value holding the type (itself, or
// in a pointer, slice, map or field) passed to an interface parameter or
// converted to an interface: what reflection sees there would change.
func (x *extraction) checkNotPassedAsInterface() error {
	return x.notPassedAsInterface(x.tn, "the new embedded one")
}

// notPassedAsInterface refuses a value holding tn passed as an interface,
// where reflection would see the struct's changed fields (what names
// them).
func (x *extraction) notPassedAsInterface(tn *types.TypeName, what string) error {
	match := func(t types.Type) bool {
		n, ok := t.(*types.Named)
		return ok && sameObj(x.WS.Fset, n.Obj(), tn)
	}
	for _, p := range x.WS.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		arg := analysis.PassedAsInterface(p.TypesInfo, p.Syntax, match)
		if arg == nil {
			continue
		}
		holding := "a value holding " + tn.Name()
		t := p.TypesInfo.TypeOf(arg)
		if pt, ok := t.(*types.Pointer); ok {
			t = pt.Elem()
		}
		if match(t) {
			holding = "a " + tn.Name()
		}
		return refactor.Refusef("%s is passed as an interface at %s; reflection there (fmt's %%+v, encoding/json, reflect) sees the struct's fields, and would see %s", holding, x.WS.Position(arg.Pos()), what)
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
	for _, p := range x.WS.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, f := range p.Syntax {
			path := x.WS.Fset.File(f.Pos()).Name()
			ast.Inspect(f, func(n ast.Node) bool {
				cl, ok := n.(*ast.CompositeLit)
				if !ok || len(cl.Elts) == 0 {
					return true
				}
				if t := p.TypesInfo.TypeOf(cl); t == nil || !x.isT(t) || analysis.IsPointer(t) {
					return true
				}
				k := fmt.Sprintf("%s:%d", path, x.WS.Fset.Position(cl.Pos()).Offset)
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
	if x.into != nil {
		return x.rewriteInto(l)
	}
	src, err := x.source(l.path)
	if err != nil {
		return false, err
	}
	cl := l.expr
	text := func(e ast.Node) string { return x.textWithEdits(l.path, src, e) }
	typ, err := x.typeText(l, src)
	if err != nil {
		return false, err
	}
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
		elts[at] = x.newType + ": " + typ + "{" + strings.Join(group, ", ") + "}"
	} else {
		if !x.contig {
			var names []string
			for _, f := range x.fields {
				for _, n := range f.Names {
					names = append(names, n.Name)
				}
			}
			return false, refactor.Refusef("the unkeyed literal at %s lists the fields in order, and %s are not next to each other; give it keys first, or move fields that are",
				x.WS.Position(cl.Pos()), joinAnd(names))
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
		elts[x.lo] = typ + "{" + strings.Join(group, ", ") + "}"
	}

	return x.writeElts(cl, l, src, elts)
}

// writeElts writes elts as the elements of the literal cl, on one line
// if it was on one, else one a line.
func (x *extraction) writeElts(cl *ast.CompositeLit, l lit, src []byte, elts []string) (bool, error) {
	tf := x.WS.Fset.File(cl.Pos())
	if tf.Line(cl.Lbrace) == tf.Line(cl.Rbrace) {
		x.replace(l.path, src, cl.Lbrace, cl.Rbrace+1, "{"+strings.Join(elts, ", ")+"}")
		return true, nil
	}
	for _, cg := range fileOf(l.pkg, l.path).Comments {
		if cg.Pos() > cl.Lbrace && cg.End() < cl.Rbrace && !insideAny(cg, cl.Elts) {
			return false, refactor.Refusef("the literal at %s has comments between its elements, which rewriting it would lose; this is not supported yet", x.WS.Position(cl.Pos()))
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

// typeText is how l's file writes the new type: by its name in the
// struct's package, qualified in another (importing the struct's package
// if the file does not).
func (x *extraction) typeText(l lit, src []byte) (string, error) {
	if l.pkg.Types == nil || l.pkg.Types.Path() == x.tn.Pkg().Path() {
		return x.newType, nil
	}
	if !token.IsExported(x.newType) {
		return "", refactor.Refusef("the literal at %s is in package %s, and package %s cannot name the unexported %s; give new_type an exported name",
			x.WS.Position(l.expr.Pos()), l.pkg.Name, l.pkg.Name, x.newType)
	}
	if x.adders == nil {
		x.adders = map[string]*imports.Adder{}
	}
	a := x.adders[l.path]
	if a == nil {
		a = imports.New(x.WS, l.pkg, fileOf(l.pkg, l.path), src)
		x.adders[l.path] = a
	}
	name, err := a.Name(x.tn.Pkg())
	if err != nil {
		return "", err
	}
	return name + "." + x.newType, nil
}

// textWithEdits returns n's source with the literal rewrites inside it
// applied, and drops those rewrites, which n's replacement now carries.
func (x *extraction) textWithEdits(path string, src []byte, n ast.Node) string {
	start, end := x.Off(n.Pos()), x.Off(n.End())
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
	x.litEdit[path] = append(x.litEdit[path], edit.TextEdit{Start: x.Off(from), End: x.Off(to), NewText: text})
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
	tf := x.WS.Fset.File(x.st.Pos())
	declStart := x.gen.Pos()
	if x.gen.Lparen.IsValid() {
		declStart = x.spec.Pos()
		if x.spec.Doc != nil {
			declStart = x.spec.Doc.Pos()
		}
	} else if x.gen.Doc != nil {
		declStart = x.gen.Doc.Pos()
	}
	at := x.Off(tf.LineStart(tf.Line(declStart)))
	declIndent := lineIndent(x.Src, at)
	kw := "type " // not inside a type ( ... ) group
	if x.gen.Lparen.IsValid() {
		kw = ""
	}

	if tf.Line(x.st.Fields.Opening) == tf.Line(x.st.Fields.Closing) {
		// type point struct{ x, y int }: rebuild both on one line.
		var kept, moved []string
		for _, f := range x.st.Fields.List {
			t := string(x.Src[x.Off(f.Pos()):x.Off(f.End())])
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
			{Start: x.Off(x.st.Fields.Opening), End: x.Off(x.st.Fields.Closing) + 1, NewText: "{ " + strings.Join(kept, "; ") + " }"},
		}
	}

	var lines strings.Builder
	var es []edit.TextEdit
	for i, f := range x.fields {
		start := f.Pos()
		if f.Doc != nil {
			start = f.Doc.Pos()
		}
		from := x.Off(tf.LineStart(tf.Line(start)))
		to := nextLine(x.Src, x.Off(f.End()))
		lines.Write(x.Src[from:to])
		text := ""
		if i == 0 {
			text = lineIndent(x.Src, x.Off(tf.LineStart(tf.Line(f.Pos())))) + x.newType + "\n"
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
