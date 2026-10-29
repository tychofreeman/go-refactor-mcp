package extractstruct

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
)

// into is a struct the fields move into that already exists, one the
// extracted struct holds by value: its type and declaration, and the field
// that holds it (named as the type when embedded).
type into struct {
	tn       *types.TypeName
	st       *ast.StructType
	path     string // its declaring file
	field    string
	embedded bool
}

// planInto plans moving the fields into a struct that already exists,
// one the struct holds by value.
func (x *extraction) planInto(a args) (*refactor.Plan, error) {
	if err := x.findInto(a.Into); err != nil {
		return nil, err
	}
	if err := x.checkIntoNames(); err != nil {
		return nil, err
	}
	if err := x.notPassedAsInterface(x.tn, "the moved fields gone"); err != nil {
		return nil, err
	}
	if err := x.notPassedAsInterface(x.into.tn, "the moved fields"); err != nil {
		return nil, err
	}
	if err := x.checkIntoLiterals(); err != nil {
		return nil, err
	}
	uses, err := x.rewriteUses()
	if err != nil {
		return nil, err
	}
	n, err := x.rewriteLiterals()
	if err != nil {
		return nil, err
	}
	decls, err := x.intoDeclEdits()
	if err != nil {
		return nil, err
	}
	var fes []edit.FileEdit
	for path, es := range x.litEdit {
		if ad := x.adders[path]; ad != nil {
			es = append(es, ad.Edits()...)
		}
		fes = append(fes, edit.FileEdit{Path: path, Edits: es})
	}
	how := "which it embeds"
	if !x.into.embedded {
		how = "which it holds in " + x.into.field
	}
	plan := &refactor.Plan{
		Summary: fmt.Sprintf("moved fields %s of %s into %s, %s (%d literal%s and %d use%s updated)",
			strings.Join(a.Fields, ", "), x.tn.Name(), x.into.tn.Name(), how, n, textutil.Plural(n), uses, textutil.Plural(uses)),
		Edits: edit.Merge(append(fes, decls...)),
	}
	for _, tn := range []*types.TypeName{x.tn, x.into.tn} {
		if analysis.PublicAPI(tn, x.WS.Pkgs) {
			plan.Notes = append(plan.Notes, refactor.APINote(tn.Name(), "moving fields from "+x.tn.Name()+" into "+x.into.tn.Name()))
			break
		}
	}
	return plan, nil
}

// findInto resolves name, the struct type the fields go into: one x's
// struct embeds or holds in a field, by value, declared in its package.
func (x *extraction) findInto(name string) error {
	st := x.tn.Type().Underlying().(*types.Struct)
	var holder *types.Var
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		t, ptr := f.Type(), false
		if p, ok := t.(*types.Pointer); ok {
			t, ptr = p.Elem(), true
		}
		if n, ok := t.(*types.Named); !ok || n.Obj().Name() != name {
			continue
		}
		switch {
		case ptr:
			return refactor.Refusef("%s holds %s through a pointer (%s); values that share one %s would then share the moved fields, so this is not supported", x.tn.Name(), name, f.Name(), name)
		case holder != nil:
			return refactor.Refusef("%s holds %s in two fields, %s and %s; it is not clear which the fields go into", x.tn.Name(), name, holder.Name(), f.Name())
		}
		holder = f
	}
	if holder == nil {
		return refactor.Refusef("%s neither embeds %s nor holds it in a field, so not every use of the fields could reach them there", x.tn.Name(), name)
	}
	tn := holder.Type().(*types.Named).Obj()
	if tn.Pkg() != x.tn.Pkg() {
		return refactor.Refusef("%s is declared in another package; moving fields across packages is not supported yet", name)
	}
	for _, f := range x.Pkg.Syntax {
		ast.Inspect(f, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok || x.Pkg.TypesInfo.Defs[ts.Name] != tn {
				return true
			}
			if s, ok := ts.Type.(*ast.StructType); ok && ts.TypeParams == nil && !ts.Assign.IsValid() {
				x.into = &into{tn: tn, st: s, path: x.WS.Fset.File(f.Pos()).Name(), field: holder.Name(), embedded: holder.Embedded()}
			}
			return false
		})
	}
	if x.into == nil {
		return refactor.Refusef("%s is not declared as a struct type without type parameters", name)
	}
	x.newType = name
	return nil
}

// checkIntoNames refuses a moved field that the struct it goes into
// already has, or, when that struct is embedded, that another embedded
// field also has: one level deeper, the selector would be ambiguous.
func (x *extraction) checkIntoNames() error {
	for n := range x.moving {
		if o, idx, _ := types.LookupFieldOrMethod(types.NewPointer(x.into.tn.Type()), true, x.Pkg.Types, n); o != nil || idx != nil {
			return refactor.Refusef("%s already has a field or method %s", x.into.tn.Name(), n)
		}
	}
	if !x.into.embedded {
		return nil
	}
	st := x.tn.Type().Underlying().(*types.Struct)
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		if !f.Embedded() || f.Name() == x.into.field {
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

// checkIntoLiterals refuses an unkeyed literal of the struct the fields go
// into: it would have to list them too.
func (x *extraction) checkIntoLiterals() error {
	for _, p := range x.WS.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, f := range p.Syntax {
			var bad *ast.CompositeLit
			ast.Inspect(f, func(n ast.Node) bool {
				cl, ok := n.(*ast.CompositeLit)
				if !ok || bad != nil || len(cl.Elts) == 0 {
					return bad == nil
				}
				if _, keyed := cl.Elts[0].(*ast.KeyValueExpr); keyed {
					return true
				}
				if nt, ok := p.TypesInfo.TypeOf(cl).(*types.Named); ok && sameObj(x.WS.Fset, nt.Obj(), x.into.tn) {
					bad = cl
				}
				return bad == nil
			})
			if bad != nil {
				return refactor.Refusef("the unkeyed literal of %s at %s would have to list the moved fields too; give it keys first", x.into.tn.Name(), x.WS.Position(bad.Pos()))
			}
		}
	}
	return nil
}

// rewriteUses puts the holding field into each selector of a moved field,
// when it is a named field: s.depth becomes s.soil.depth.
func (x *extraction) rewriteUses() (int, error) {
	if x.into.embedded {
		return 0, nil
	}
	st := x.tn.Type().Underlying().(*types.Struct)
	var moved []*types.Var
	for i := 0; i < st.NumFields(); i++ {
		if x.moving[st.Field(i).Name()] {
			moved = append(moved, st.Field(i))
		}
	}
	seen := map[string]bool{}
	n := 0
	for _, p := range x.WS.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for se, sel := range p.TypesInfo.Selections {
			if sel.Kind() != types.FieldVal {
				continue
			}
			for _, v := range moved {
				if !sameObj(x.WS.Fset, sel.Obj(), v) {
					continue
				}
				path := x.WS.Fset.Position(se.Pos()).Filename
				k := fmt.Sprintf("%s:%d", path, x.Off(se.Sel.Pos()))
				if seen[k] {
					continue
				}
				seen[k] = true
				if _, err := x.source(path); err != nil {
					return 0, err
				}
				x.litEdit[path] = append(x.litEdit[path], edit.TextEdit{Start: x.Off(se.Sel.Pos()), End: x.Off(se.Sel.Pos()), NewText: x.into.field + "."})
				n++
			}
		}
	}
	return n, nil
}

// rewriteInto rewrites one keyed literal of the struct that sets a moving
// field: the fields join the literal of the holding field, or one made for
// them.
func (x *extraction) rewriteInto(l lit) (bool, error) {
	src, err := x.source(l.path)
	if err != nil {
		return false, err
	}
	cl := l.expr
	text := func(e ast.Node) string { return x.textWithEdits(l.path, src, e) }
	if _, keyed := cl.Elts[0].(*ast.KeyValueExpr); !keyed {
		return false, refactor.Refusef("the unkeyed literal at %s lists the fields in order; give it keys first", x.WS.Position(cl.Pos()))
	}
	var elts, group []string
	at, holder := -1, -1
	var held *ast.CompositeLit
	for _, e := range cl.Elts {
		kv := e.(*ast.KeyValueExpr)
		id, _ := kv.Key.(*ast.Ident)
		switch {
		case id != nil && x.moving[id.Name]:
			if at < 0 {
				at = len(elts)
			}
			group = append(group, text(kv))
			continue
		case id != nil && id.Name == x.into.field:
			inner, ok := ast.Unparen(kv.Value).(*ast.CompositeLit)
			if !ok {
				return false, refactor.Refusef("the literal at %s sets %s from %s, which the moved fields cannot join; set it with a literal first", x.WS.Position(cl.Pos()), x.into.field, types.ExprString(kv.Value))
			}
			if len(inner.Elts) > 0 {
				if _, keyed := inner.Elts[0].(*ast.KeyValueExpr); !keyed {
					return false, refactor.Refusef("the literal of %s at %s is unkeyed; give it keys first", x.into.tn.Name(), x.WS.Position(inner.Pos()))
				}
			}
			holder, held = len(elts), inner
		}
		elts = append(elts, text(kv))
	}
	if len(group) == 0 {
		return false, nil
	}
	if held != nil {
		var inner []string
		for _, e := range held.Elts {
			inner = append(inner, text(e))
		}
		elts[holder] = x.into.field + ": " + text(held.Type) + "{" + strings.Join(append(inner, group...), ", ") + "}"
	} else {
		typ, err := x.typeText(l, src)
		if err != nil {
			return false, err
		}
		elts = append(elts[:at], append([]string{x.into.field + ": " + typ + "{" + strings.Join(group, ", ") + "}"}, elts[at:]...)...)
	}
	return x.writeElts(cl, l, src, elts)
}

// intoDeclEdits move the fields out of the struct, to the end of the
// struct they go into.
func (x *extraction) intoDeclEdits() ([]edit.FileEdit, error) {
	tf := x.WS.Fset.File(x.st.Pos())
	if tf.Line(x.st.Fields.Opening) == tf.Line(x.st.Fields.Closing) {
		return nil, refactor.Refusef("%s is declared on one line; write its fields one a line first", x.tn.Name())
	}
	var lines []string
	var out []edit.TextEdit
	for _, f := range x.fields {
		start := f.Pos()
		if f.Doc != nil {
			start = f.Doc.Pos()
		}
		from := x.Off(tf.LineStart(tf.Line(start)))
		to := nextLine(x.Src, x.Off(f.End()))
		lines = append(lines, string(x.Src[from:to]))
		out = append(out, edit.TextEdit{Start: from, End: to})
	}
	if _, err := x.source(x.into.path); err != nil {
		return nil, err
	}
	itf := x.WS.Fset.File(x.into.st.Pos())
	closing := x.into.st.Fields.Closing
	var in edit.TextEdit
	if itf.Line(x.into.st.Fields.Opening) == itf.Line(closing) {
		var fields []string
		for _, l := range lines {
			fields = append(fields, strings.TrimSpace(l))
		}
		sep := "; "
		if len(x.into.st.Fields.List) == 0 {
			sep = " "
		}
		in = edit.TextEdit{Start: x.Off(closing), End: x.Off(closing), NewText: sep + strings.Join(fields, "; ") + " "}
		if len(x.into.st.Fields.List) > 0 {
			last := x.into.st.Fields.List[len(x.into.st.Fields.List)-1]
			in = edit.TextEdit{Start: x.Off(last.End()), End: x.Off(last.End()), NewText: sep + strings.Join(fields, "; ")}
		}
	} else {
		at := x.Off(itf.LineStart(itf.Line(closing)))
		in = edit.TextEdit{Start: at, End: at, NewText: strings.Join(lines, "")}
	}
	if x.into.path == x.path {
		return []edit.FileEdit{{Path: x.path, Edits: append(out, in)}}, nil
	}
	return []edit.FileEdit{{Path: x.path, Edits: out}, {Path: x.into.path, Edits: []edit.TextEdit{in}}}, nil
}
