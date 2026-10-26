package analysis

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

// A Write is a value a struct field is given: Value, or, where what it
// is given cannot be followed, nil, with Why saying how it is written.
type Write struct {
	Info  *types.Info
	Value ast.Expr
	Pos   token.Pos
	Why   string
}

// FieldWrites returns every write of field in pkgs: keys of composite
// literals, elements of positional ones, and assignments; an
// op-assignment, an assignment of a call's several results and taking
// its address are writes without a value.
func FieldWrites(fset *token.FileSet, pkgs []*packages.Package, field *types.Var) []Write {
	want := fset.Position(field.Pos())
	same := func(obj types.Object) bool {
		return obj != nil && obj.Name() == field.Name() && fset.Position(obj.Pos()) == want
	}
	var out []Write
	seen := map[token.Position]bool{}
	add := func(w Write) {
		if p := fset.Position(w.Pos); !seen[p] {
			seen[p] = true
			out = append(out, w)
		}
	}
	for _, p := range pkgs {
		info := p.TypesInfo
		if info == nil {
			continue
		}
		for _, f := range p.Syntax {
			ast.Inspect(f, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.CompositeLit:
					st, ok := info.TypeOf(n).Underlying().(*types.Struct)
					if !ok || len(n.Elts) == 0 {
						return true
					}
					if _, keyed := n.Elts[0].(*ast.KeyValueExpr); keyed {
						return true
					}
					for i := range min(st.NumFields(), len(n.Elts)) {
						if same(st.Field(i)) {
							add(Write{Info: info, Value: n.Elts[i], Pos: n.Elts[i].Pos()})
						}
					}
				case *ast.Ident:
					if same(info.Uses[n]) {
						if w, ok := writeAt(info, f, n); ok {
							add(w)
						}
					}
				}
				return true
			})
		}
	}
	return out
}

// writeAt returns the write that id, a use of the field, makes, if any.
func writeAt(info *types.Info, f *ast.File, id *ast.Ident) (Write, bool) {
	path, _ := astutil.PathEnclosingInterval(f, id.Pos(), id.End())
	if kv, ok := path[1].(*ast.KeyValueExpr); ok && kv.Key == id {
		return Write{Info: info, Value: kv.Value, Pos: kv.Value.Pos()}, true
	}
	sel, ok := path[1].(*ast.SelectorExpr)
	if !ok || sel.Sel != id {
		return Write{}, false
	}
	var e ast.Expr = sel
	path = path[1:]
	for len(path) > 1 {
		paren, ok := path[1].(*ast.ParenExpr)
		if !ok {
			break
		}
		e, path = paren, path[1:]
	}
	switch u := path[1].(type) {
	case *ast.AssignStmt:
		for i, l := range u.Lhs {
			switch {
			case l != e:
			case len(u.Lhs) != len(u.Rhs):
				return Write{Info: info, Pos: sel.Pos(), Why: "one of a call's results"}, true
			case u.Tok == token.ASSIGN || u.Tok == token.DEFINE:
				return Write{Info: info, Value: u.Rhs[i], Pos: u.Rhs[i].Pos()}, true
			default:
				return Write{Info: info, Pos: sel.Pos(), Why: u.Tok.String() + " on it"}, true
			}
		}
	case *ast.IncDecStmt:
		return Write{Info: info, Pos: sel.Pos(), Why: u.Tok.String() + " on it"}, true
	case *ast.UnaryExpr:
		if u.Op == token.AND {
			return Write{Info: info, Pos: sel.Pos(), Why: "its address taken"}, true
		}
	}
	return Write{}, false
}

// MayEqual reports whether w can give the field a value equal to c, a
// constant of type t: a constant equal to it, a value of t's default
// type, or of an interface that type implements, or a write that
// cannot be followed.
func (w Write) MayEqual(c constant.Value, t types.Type) bool {
	if w.Value == nil {
		return true
	}
	tv := w.Info.Types[w.Value]
	dt := types.Default(t)
	switch {
	case tv.IsNil() || tv.Type == nil:
		return false
	case tv.Value != nil:
		return types.Identical(types.Default(tv.Type), dt) && tv.Value.Kind() == c.Kind() && constant.Compare(tv.Value, token.EQL, c)
	case types.IsInterface(tv.Type):
		return types.Implements(dt, tv.Type.Underlying().(*types.Interface))
	}
	return types.Identical(tv.Type, dt)
}
