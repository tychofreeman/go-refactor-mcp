// Package inlinestruct implements inline_struct: the fields and methods
// of a struct that another holds move into the holder, the inverse of
// extract_struct.
package inlinestruct

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/site"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// InlineStruct is the "inline_struct" refactoring.
type InlineStruct struct{ refactor.Named[args] }

// New returns the inline_struct refactoring.
func New() *InlineStruct { return &InlineStruct{Named: refactor.Called[args]("inline_struct")} }

func (*InlineStruct) Description() string {
	return `Move the fields and methods of a struct that another holds into the holder, and delete it: the inverse of extract_struct. With type Bed struct { Soil; name string } and type Soil struct { depth int; loam bool }, inlining Soil into Bed gives type Bed struct { depth int; loam bool; name string }; b.Soil.depth becomes b.depth, Bed{Soil: Soil{depth: 3}} becomes Bed{depth: 3}, and Soil's methods become Bed's (b.Soil.till() becomes b.till()). A named field (soil Soil) works the same way.
Identify the holder by file and name, and field: the embedded type's name or the field's name.
elsewhere: keep inlines a copy when the struct is used outside the holder and keeps it for those uses; otherwise that is refused. prefer: outer keeps the holder's method where both have one, inner replaces it with the inlined struct's, which changes what its calls do; otherwise that is refused.
Refuses, saying why: a field both have; the held struct used as a whole (b.Soil), or held by pointer; a literal without keys; a generic struct, or one declared or with methods in another file; a method that uses its receiver as a whole; methods that would make a named field's holder newly implement an interface; a holder whose fields reflection may see.`
}

type args struct {
	File      string `json:"file" desc:"Path of the file declaring the holder, relative to the module root."`
	Name      string `json:"name" desc:"The struct holding the one to inline."`
	Field     string `json:"field" desc:"The field holding it: the embedded type's name, or the field's name."`
	Elsewhere string `json:"elsewhere,omitempty" desc:"When the inlined struct is used outside the holder: refuse (the default), or keep, which keeps it for those uses and inlines a copy."`
	Prefer    string `json:"prefer,omitempty" desc:"When both have a method of the same name: outer keeps the holder's, inner replaces it with the inlined struct's."`
}

// inlining is one inline_struct: the struct inner, held by outer in the
// field fld, moves into it.
type inlining struct {
	*site.Site
	a                  args
	outer, inner       *types.Named
	outerAST, innerAST *ast.StructType
	innerDecl          *ast.GenDecl
	fld                *ast.Field
	fieldVar           *types.Var
	embedded           bool
	methods            []*ast.FuncDecl          // inner's methods
	outerMethods       map[string]*ast.FuncDecl // outer's, by name
	clash              map[string]bool          // methods both have
	edits              map[string][]edit.TextEdit
	seen               map[string]bool
	deleted            [][2]token.Pos     // ranges of this file deleted whole
	innerUses          map[token.Pos]bool // uses of inner's name the change rewrites or removes
	notes              []string
}

// Plan checks preconditions and computes the edits.
func (*InlineStruct) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if !slices.Contains([]string{"", "refuse", "keep"}, a.Elsewhere) || !slices.Contains([]string{"", "outer", "inner"}, a.Prefer) {
		return nil, refactor.Refusef("elsewhere is refuse or keep, and prefer outer or inner")
	}
	st, err := site.At(ws, ws.Abs(a.File))
	if err != nil {
		return nil, err
	}
	x := &inlining{Site: st, a: a, edits: map[string][]edit.TextEdit{}, seen: map[string]bool{}, innerUses: map[token.Pos]bool{}, clash: map[string]bool{}, outerMethods: map[string]*ast.FuncDecl{}}
	for _, step := range []func() error{x.find, x.checkFields, x.findMethods, x.checkMethodSet, x.rewriteUses, x.checkReflected, x.moveFields, x.moveMethods, x.deleteInner} {
		if err := step(); err != nil {
			return nil, err
		}
	}
	var fes []edit.FileEdit
	for path, es := range x.edits {
		fes = append(fes, edit.FileEdit{Path: path, Edits: es})
	}
	slices.SortFunc(fes, func(p, q edit.FileEdit) int { return strings.Compare(p.Path, q.Path) })
	if ast.IsExported(a.Name) {
		x.notes = append(x.notes, refactor.APINote(a.Name, "inlining "+x.inner.Obj().Name()+" into it"))
	}
	return &refactor.Plan{
		Summary: fmt.Sprintf("inlined %s into %s: its fields and %d method%s", x.inner.Obj().Name(), a.Name, len(x.methods), textutil.Plural(len(x.methods))),
		Edits:   fes,
		Notes:   x.notes,
	}, nil
}

// find locates the holder, the field and the held struct.
func (x *inlining) find() error {
	_, spec := typeSpec(x.File, x.a.Name)
	tn, _ := x.Pkg.Types.Scope().Lookup(x.a.Name).(*types.TypeName)
	if spec == nil || tn == nil {
		return refactor.Refusef("%s declares no type %s", x.a.File, x.a.Name)
	}
	st, ok := spec.Type.(*ast.StructType)
	if !ok || spec.TypeParams != nil {
		return refactor.Refusef("%s is not a struct type, or is generic", x.a.Name)
	}
	x.outer, x.outerAST = tn.Type().(*types.Named), st
	for _, f := range st.Fields.List {
		if (len(f.Names) == 0 && embeddedName(f.Type) == x.a.Field) || (len(f.Names) == 1 && f.Names[0].Name == x.a.Field) {
			x.fld = f
		}
	}
	if x.fld == nil {
		return refactor.Refusef("%s has no field %s", x.a.Name, x.a.Field)
	}
	if _, ok := x.fld.Type.(*ast.StarExpr); ok {
		return refactor.Refusef("%s holds %s by pointer, so inlining it would turn a nil pointer, which panics when used, into fields that do not", x.a.Name, x.a.Field)
	}
	x.embedded = len(x.fld.Names) == 0
	ost := x.outer.Underlying().(*types.Struct)
	for i := range ost.NumFields() {
		if ost.Field(i).Name() == x.a.Field {
			x.fieldVar = ost.Field(i)
		}
	}
	inner, _ := x.Info.TypeOf(x.fld.Type).(*types.Named)
	if inner == nil || inner.Obj().Pkg() != x.Pkg.Types || inner.TypeArgs().Len() > 0 {
		return refactor.Refusef("%s is not a struct type of this package without type parameters", x.a.Field)
	}
	gen, ispec := typeSpec(x.File, inner.Obj().Name())
	if ispec == nil {
		return refactor.Refusef("%s is not declared in %s; move_declaration it there first", inner.Obj().Name(), x.a.File)
	}
	ist, ok := ispec.Type.(*ast.StructType)
	if !ok {
		return refactor.Refusef("%s is not a struct type", inner.Obj().Name())
	}
	if len(gen.Specs) > 1 {
		return refactor.Refusef("%s is declared in a group of types; move_declaration it out of the group first", inner.Obj().Name())
	}
	x.inner, x.innerAST, x.innerDecl = inner, ist, gen
	x.innerUses[ispec.Name.Pos()] = true
	x.innerUses[typeIdent(x.fld.Type).Pos()] = true
	return nil
}

// checkFields refuses a field name both structs have, or that one has as
// a field and the other as a method.
func (x *inlining) checkFields() error {
	ost, ist := x.outer.Underlying().(*types.Struct), x.inner.Underlying().(*types.Struct)
	outerNames := map[string]bool{}
	for i := range ost.NumFields() {
		if f := ost.Field(i); f != x.fieldVar {
			outerNames[f.Name()] = true
		}
	}
	for i := range x.outer.NumMethods() {
		outerNames[x.outer.Method(i).Name()] = true
	}
	for i := range ist.NumFields() {
		if n := ist.Field(i).Name(); outerNames[n] {
			return refactor.Refusef("%s and %s both have %s, and the field would hide or clash with it; rename one first", x.a.Name, x.inner.Obj().Name(), n)
		}
	}
	for i := range x.inner.NumMethods() {
		m := x.inner.Method(i).Name()
		for j := range ost.NumFields() {
			if f := ost.Field(j); f != x.fieldVar && f.Name() == m {
				return refactor.Refusef("%s has a field %s and %s a method %s; rename one first", x.a.Name, m, x.inner.Obj().Name(), m)
			}
		}
	}
	return nil
}

// findMethods finds the methods of both types in this file, and those
// both have.
func (x *inlining) findMethods() error {
	file := x.TF.Name()
	for _, d := range x.File.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Recv == nil {
			continue
		}
		switch typeIdent(fd.Recv.List[0].Type).Name {
		case x.inner.Obj().Name():
			x.methods = append(x.methods, fd)
		case x.a.Name:
			x.outerMethods[fd.Name.Name] = fd
		}
	}
	for i := range x.inner.NumMethods() {
		m := x.inner.Method(i)
		if x.WS.Fset.Position(m.Pos()).Filename != file {
			return refactor.Refusef("%s's method %s is in another file; move_declaration it into %s first", x.inner.Obj().Name(), m.Name(), x.a.File)
		}
		if x.outerMethods[m.Name()] != nil {
			x.clash[m.Name()] = true
		}
	}
	for i := range x.outer.NumMethods() {
		m := x.outer.Method(i)
		if x.WS.Fset.Position(m.Pos()).Filename != file && x.clash[m.Name()] {
			return refactor.Refusef("%s's method %s is in another file; move_declaration it into %s first", x.a.Name, m.Name(), x.a.File)
		}
	}
	for m := range x.clash {
		if x.a.Prefer == "" {
			return refactor.Refusef("%s and %s both have a method %s; give prefer outer to keep %s's, or inner to use %s's instead", x.a.Name, x.inner.Obj().Name(), m, x.a.Name, x.inner.Obj().Name())
		}
		if x.a.Prefer == "inner" {
			x.notes = append(x.notes, fmt.Sprintf("%s.%s is now what %s.%s was: its calls do what %s's did, which changes what they do", x.a.Name, m, x.inner.Obj().Name(), m, x.inner.Obj().Name()))
			x.deleteDecl(x.outerMethods[m])
		}
	}
	for _, md := range x.methods {
		recv := md.Recv.List[0]
		if len(recv.Names) == 0 {
			continue
		}
		v := x.Info.Defs[recv.Names[0]]
		var bad ast.Node
		inspectParents(md.Body, func(n ast.Node, parent ast.Node) {
			if id, ok := n.(*ast.Ident); ok && x.Info.Uses[id] == v && bad == nil {
				if sel, ok := parent.(*ast.SelectorExpr); !ok || sel.X != id {
					bad = id
				}
			}
		})
		if bad != nil {
			return refactor.Refusef("%s's method %s uses its receiver as a whole on line %d, which %s cannot stand for", x.inner.Obj().Name(), md.Name.Name, x.TF.Line(bad.Pos()), x.a.Name)
		}
	}
	return nil
}

// rewriteUses rewrites every selection through the field (b.Soil.depth
// becomes b.depth) and every literal of the holder that gives it, in
// every package, and refuses a use of the field as a whole, a literal
// without keys, and uses of the inner struct elsewhere unless kept.
func (x *inlining) rewriteUses() error {
	var elsewhere []string
	for _, p := range x.WS.Pkgs {
		for _, f := range p.Syntax {
			name := x.WS.Fset.File(f.Pos()).Name()
			if !x.WS.Contains(name) {
				continue
			}
			var err error
			inspectParents(f, func(n, parent ast.Node) {
				if err != nil {
					return
				}
				switch n := n.(type) {
				case *ast.SelectorExpr:
					err = x.selection(p.TypesInfo, name, n, parent)
				case *ast.CompositeLit:
					err = x.literal(p.TypesInfo, name, n)
				case *ast.Ident:
					if tn, ok := p.TypesInfo.Uses[n].(*types.TypeName); ok && sameObj(x.WS.Fset, tn, x.inner.Obj()) && !x.innerUses[n.Pos()] && !x.inRecv(n) {
						elsewhere = append(elsewhere, x.WS.Position(n.Pos()))
					}
				}
			})
			if err != nil {
				return err
			}
		}
	}
	if len(elsewhere) > 0 && x.a.Elsewhere != "keep" {
		return refactor.Refusef("%s is used elsewhere than in %s (%s); give elsewhere keep to inline a copy and keep %s for those uses", x.inner.Obj().Name(), x.a.Name, textutil.FirstFew(elsewhere, 3), x.inner.Obj().Name())
	}
	x.a.Elsewhere = map[bool]string{true: "keep", false: ""}[len(elsewhere) > 0]
	return nil
}

// selection handles a selector: x.Soil inside x.Soil.depth loses .Soil;
// x.Soil alone is refused.
func (x *inlining) selection(info *types.Info, path string, sel *ast.SelectorExpr, parent ast.Node) error {
	s := info.Selections[sel]
	if s == nil || s.Kind() != types.FieldVal || !sameObj(x.WS.Fset, s.Obj(), x.fieldVar) {
		return nil
	}
	outer, ok := parent.(*ast.SelectorExpr)
	if !ok || outer.X != sel {
		return refactor.Refusef("%s is used as a whole at %s; inline_struct needs every use to go through its fields or methods", types.ExprString(sel), x.WS.Position(sel.Pos()))
	}
	if x.clash[outer.Sel.Name] && x.a.Prefer == "outer" {
		if os := info.Selections[outer]; os != nil && os.Kind() == types.MethodVal {
			return refactor.Refusef("%s at %s calls %s's %s, and without %s it would call %s's own %s; give prefer inner, or change the call first", types.ExprString(outer), x.WS.Position(outer.Pos()), x.inner.Obj().Name(), outer.Sel.Name, x.a.Field, x.a.Name, outer.Sel.Name)
		}
	}
	x.add(path, sel.X.End(), sel.Sel.End(), "")
	return nil
}

// literal rewrites a literal of the holder that gives the field: the
// inner literal's elements take its place.
func (x *inlining) literal(info *types.Info, path string, lit *ast.CompositeLit) error {
	t := info.TypeOf(lit)
	if n, ok := t.(*types.Named); !ok || !sameObj(x.WS.Fset, n.Obj(), x.outer.Obj()) {
		return nil
	}
	if len(lit.Elts) > 0 {
		if _, ok := lit.Elts[0].(*ast.KeyValueExpr); !ok {
			return refactor.Refusef("the literal of %s at %s has no keys; give it keys first", x.a.Name, x.WS.Position(lit.Pos()))
		}
	}
	for i, e := range lit.Elts {
		kv := e.(*ast.KeyValueExpr)
		if id, ok := kv.Key.(*ast.Ident); !ok || id.Name != x.a.Field {
			continue
		}
		inner, ok := ast.Unparen(kv.Value).(*ast.CompositeLit)
		if !ok {
			return refactor.Refusef("the literal of %s at %s gives %s a value that is not a literal; inline_struct needs %s{...} there", x.a.Name, x.WS.Position(kv.Pos()), x.a.Field, x.inner.Obj().Name())
		}
		if inner.Type != nil {
			x.innerUses[typeIdent(inner.Type).Pos()] = true
		}
		var parts []string
		for _, ie := range inner.Elts {
			if _, ok := ie.(*ast.KeyValueExpr); !ok {
				return refactor.Refusef("the literal of %s at %s has no keys; give it keys first", x.inner.Obj().Name(), x.WS.Position(inner.Pos()))
			}
			parts = append(parts, x.textAt(path, ie.Pos(), ie.End()))
		}
		start, end := kv.Pos(), kv.End()
		if len(parts) == 0 {
			switch {
			case i+1 < len(lit.Elts):
				end = lit.Elts[i+1].Pos()
			case i > 0:
				start = lit.Elts[i-1].End()
			}
		}
		x.add(path, start, end, strings.Join(parts, ", "))
	}
	return nil
}

// checkReflected refuses a holder whose values reach reflection that sees
// field names, and notes the calls of fmt that print one.
func (x *inlining) checkReflected() error {
	match := func(t types.Type) bool {
		n, ok := t.(*types.Named)
		return ok && sameObj(x.WS.Fset, n.Obj(), x.outer.Obj())
	}
	var printed []string
	for _, ref := range analysis.ReflectedIn(x.WS.Pkgs, x.WS.Fset, match) {
		switch ref.Sight {
		case analysis.Prints, analysis.PrintsNames:
			printed = append(printed, x.WS.Position(ref.Arg.Pos())+" "+ref.Callee)
		case analysis.SeesNames:
			return refactor.Refusef("a value holding %s is %s at %s, where reflection may look at its fields, and would see the inlined ones; only fmt's printing is allowed, with a note", x.a.Name, ref.Passed(), x.WS.Position(ref.Arg.Pos()))
		}
	}
	if len(printed) > 0 {
		x.notes = append(x.notes, "what these calls print changes, since fmt prints the inlined fields in "+x.a.Name+" now: "+textutil.FirstFew(printed, 5))
	}
	return nil
}

// knownMethods are the method names through which the standard library
// treats a value differently.
var knownMethods = []string{"String", "Error", "Format", "GoString", "MarshalJSON", "UnmarshalJSON", "MarshalText", "UnmarshalText", "MarshalBinary", "UnmarshalBinary"}

// checkMethodSet refuses, for a named field, moving a method that could
// make the holder implement an interface it did not: one of knownMethods,
// or a method of an interface of the module. An embedded struct's methods
// were promoted already.
func (x *inlining) checkMethodSet() error {
	if x.embedded {
		return nil
	}
	ifaceMethods := map[string]bool{}
	for _, p := range x.WS.Pkgs {
		for _, o := range p.TypesInfo.Defs {
			if tn, ok := o.(*types.TypeName); ok {
				if it, ok := tn.Type().Underlying().(*types.Interface); ok {
					for i := range it.NumMethods() {
						ifaceMethods[it.Method(i).Name()] = true
					}
				}
			}
		}
	}
	for _, md := range x.methods {
		if m := md.Name.Name; !x.clash[m] && (slices.Contains(knownMethods, m) || ifaceMethods[m]) {
			return refactor.Refusef("moving %s's method %s would give %s a method %s, through which it could implement an interface it does not now, and be printed or used differently", x.inner.Obj().Name(), m, x.a.Name, m)
		}
	}
	return nil
}

// moveFields replaces the field with the inner struct's fields.
func (x *inlining) moveFields() error {
	start := x.fld.Pos()
	if x.fld.Doc != nil {
		start = x.fld.Doc.Pos()
	}
	from := lineStart(x.Src, x.Off(start))
	indent := gocode.IndentOf(x.Src, from)
	var lines []string
	for _, f := range x.innerAST.Fields.List {
		if f.Doc != nil {
			for _, c := range f.Doc.List {
				lines = append(lines, indent+c.Text)
			}
		}
		line := indent + x.Text(f)
		if f.Comment != nil {
			line += " " + x.Text(f.Comment)
		}
		lines = append(lines, line)
	}
	x.add(x.TF.Name(), x.WS.Fset.File(start).Pos(from), x.WS.Fset.File(start).Pos(textutil.NextLine(x.Src, x.Off(x.fld.End()))), strings.Join(lines, "\n")+"\n")
	return nil
}

// moveMethods makes the inner struct's methods the holder's: their
// receivers name the holder, or, when the inner struct stays, a copy
// does. A method of both is kept or dropped as prefer says.
func (x *inlining) moveMethods() error {
	for _, md := range x.methods {
		if x.clash[md.Name.Name] && x.a.Prefer == "outer" {
			if x.a.Elsewhere != "keep" {
				x.deleteDecl(md)
			}
			continue
		}
		recv := typeIdent(md.Recv.List[0].Type)
		if x.a.Elsewhere != "keep" {
			x.add(x.TF.Name(), recv.Pos(), recv.End(), x.a.Name)
			continue
		}
		start := md.Pos()
		if md.Doc != nil {
			start = md.Doc.Pos()
		}
		text := x.textAt(x.TF.Name(), start, recv.Pos()) + x.a.Name + x.textAt(x.TF.Name(), recv.End(), md.End())
		x.add(x.TF.Name(), md.End(), md.End(), "\n\n"+text)
	}
	return nil
}

// deleteInner deletes the inner struct's declaration, unless it stays
// for its other uses.
func (x *inlining) deleteInner() error {
	if x.a.Elsewhere != "keep" {
		x.deleteDecl(x.innerDecl)
	}
	return nil
}

// deleteDecl deletes a top-level declaration of this file with its doc
// comment and the blank line after it.
func (x *inlining) deleteDecl(d ast.Decl) {
	start := d.Pos()
	switch d := d.(type) {
	case *ast.FuncDecl:
		if d.Doc != nil {
			start = d.Doc.Pos()
		}
	case *ast.GenDecl:
		if d.Doc != nil {
			start = d.Doc.Pos()
		}
	}
	tf := x.WS.Fset.File(start)
	from, to := lineStart(x.Src, x.Off(start)), textutil.NextLine(x.Src, x.Off(d.End()))
	if to < len(x.Src) && x.Src[to] == '\n' {
		to++
	}
	x.deleted = append(x.deleted, [2]token.Pos{d.Pos(), d.End()})
	x.add(x.TF.Name(), tf.Pos(from), tf.Pos(to), "")
}

// add records an edit of path from start to end, once however many
// package variants share the file, and none inside a deleted declaration.
func (x *inlining) add(path string, start, end token.Pos, text string) {
	if path == x.TF.Name() {
		for _, r := range x.deleted {
			if start >= r[0] && end <= r[1] && start != end {
				return
			}
		}
	}
	ps, pe := x.WS.Fset.Position(start), x.WS.Fset.Position(end)
	key := fmt.Sprint(path, ps.Offset, pe.Offset, text)
	if x.seen[key] {
		return
	}
	x.seen[key] = true
	x.edits[path] = append(x.edits[path], edit.TextEdit{Start: ps.Offset, End: pe.Offset, NewText: text})
}

// textAt returns the source of path between start and end.
func (x *inlining) textAt(path string, start, end token.Pos) string {
	src := x.Src
	if path != x.TF.Name() {
		s, err := site.At(x.WS, path)
		if err != nil {
			return ""
		}
		src = s.Src
	}
	return string(src[x.WS.Fset.Position(start).Offset:x.WS.Fset.Position(end).Offset])
}

// inRecv reports whether id is the type of the receiver of one of the
// inner struct's methods.
func (x *inlining) inRecv(id *ast.Ident) bool {
	for _, md := range x.methods {
		if typeIdent(md.Recv.List[0].Type) == id {
			return true
		}
	}
	return false
}

// typeSpec finds the declaration of the type name in file.
func typeSpec(file *ast.File, name string) (*ast.GenDecl, *ast.TypeSpec) {
	for _, d := range file.Decls {
		gen, ok := d.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, s := range gen.Specs {
			if ts := s.(*ast.TypeSpec); ts.Name.Name == name {
				return gen, ts
			}
		}
	}
	return nil, nil
}

// typeIdent returns the identifier naming the type in e: T, *T or pkg.T.
func typeIdent(e ast.Expr) *ast.Ident {
	switch e := e.(type) {
	case *ast.Ident:
		return e
	case *ast.StarExpr:
		return typeIdent(e.X)
	case *ast.SelectorExpr:
		return e.Sel
	case *ast.IndexExpr:
		return typeIdent(e.X)
	case *ast.IndexListExpr:
		return typeIdent(e.X)
	}
	return &ast.Ident{}
}

// embeddedName is the name an embedded field of type e has.
func embeddedName(e ast.Expr) string { return typeIdent(e).Name }

// sameObj reports whether a and b are the same object, though they may
// come from different variants of a package (the package and its test).
func sameObj(fset *token.FileSet, a, b types.Object) bool {
	return a != nil && b != nil && fset.Position(a.Pos()) == fset.Position(b.Pos()) && a.Name() == b.Name()
}

// inspectParents calls f for every node under root with its parent.
func inspectParents(root ast.Node, f func(n, parent ast.Node)) {
	var stack []ast.Node
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		var parent ast.Node
		if len(stack) > 0 {
			parent = stack[len(stack)-1]
		}
		f(n, parent)
		stack = append(stack, n)
		return true
	})
}

// lineStart returns the offset of the start of the line off is on.
func lineStart(src []byte, off int) int {
	for off > 0 && src[off-1] != '\n' {
		off--
	}
	return off
}
