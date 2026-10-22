package analysis

import (
	"go/ast"
	"go/token"
	"go/types"
)

// BecomesInterface returns the first expression in files whose value
// holds a type match accepts and that may be converted to an interface
// there, or nil. Only there can the methods of what it holds matter to a
// type assertion, type switch or reflection: a type that implements one
// more interface behaves the same while none of its values reaches one.
// A value may be converted wherever an interface or a type parameter is
// wanted (passed, assigned, returned, stored, sent, compared, a map key,
// panic), and in any place not known to keep its type.
func BecomesInterface(info *types.Info, files []*ast.File, match func(types.Type) bool) ast.Expr {
	var found ast.Expr
	for _, f := range files {
		var stack []ast.Node
		ast.Inspect(f, func(n ast.Node) bool {
			if found != nil {
				return false
			}
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			stack = append(stack, n)
			e, ok := n.(ast.Expr)
			if !ok {
				return true
			}
			tv, ok := info.Types[e]
			if !ok || !tv.IsValue() || !Holds(tv.Type, match) || keepsType(info, stack) {
				return true
			}
			found = e
			return false
		})
		if found != nil {
			break
		}
	}
	return found
}

// keepsType reports whether the expression at the end of stack keeps
// its type where it is used: no interface or type parameter is wanted
// there. What it yields instead (a pointer, an element) is checked as an
// expression of its own.
func keepsType(info *types.Info, stack []ast.Node) bool {
	e := stack[len(stack)-1].(ast.Expr)
	if len(stack) < 2 {
		return false
	}
	switch p := stack[len(stack)-2].(type) {
	case *ast.ParenExpr, *ast.StarExpr, *ast.UnaryExpr, *ast.SliceExpr, *ast.ExprStmt, *ast.IncDecStmt:
		return true
	case *ast.SelectorExpr:
		return p.X == e
	case *ast.IndexExpr:
		if p.X == e {
			return true
		}
		if m, ok := under(info.TypeOf(p.X)).(*types.Map); ok {
			return concrete(m.Key())
		}
		return true
	case *ast.CallExpr:
		return keptByCall(info, p, e)
	case *ast.CompositeLit:
		for i, elt := range p.Elts {
			if elt == e {
				return concrete(element(info.TypeOf(p), i, nil))
			}
		}
	case *ast.KeyValueExpr:
		lit, ok := stack[len(stack)-3].(*ast.CompositeLit)
		if !ok {
			return false
		}
		if p.Key == e {
			if m, ok := under(info.TypeOf(lit)).(*types.Map); ok {
				return concrete(m.Key())
			}
			return true
		}
		return concrete(element(info.TypeOf(lit), -1, p.Key))
	case *ast.AssignStmt:
		for _, l := range p.Lhs {
			if l == e {
				return true
			}
		}
		for i, r := range p.Rhs {
			if r == e && len(p.Lhs) == len(p.Rhs) {
				return concrete(info.TypeOf(p.Lhs[i]))
			}
		}
	case *ast.ValueSpec:
		for i, v := range p.Values {
			if v == e && len(p.Names) == len(p.Values) {
				return concrete(info.TypeOf(p.Names[i]))
			}
		}
	case *ast.ReturnStmt:
		results := enclosingResults(info, stack)
		for i, r := range p.Results {
			if r == e && results != nil && results.Len() == len(p.Results) {
				return concrete(results.At(i).Type())
			}
		}
	case *ast.BinaryExpr:
		other := p.X
		if other == e {
			other = p.Y
		}
		return concrete(info.TypeOf(other))
	case *ast.SendStmt:
		if ch, ok := under(info.TypeOf(p.Chan)).(*types.Chan); ok && p.Value == e {
			return concrete(ch.Elem())
		}
	case *ast.RangeStmt:
		return p.X == e && p.Tok != token.ASSIGN
	}
	return false
}

// keptByCall reports whether arg keeps its type in call: as what is
// called, or as an argument where no interface or type parameter is
// wanted.
func keptByCall(info *types.Info, call *ast.CallExpr, arg ast.Expr) bool {
	if call.Fun == arg {
		return true
	}
	tv := info.Types[call.Fun]
	switch {
	case tv.IsType():
		return concrete(tv.Type)
	case tv.IsBuiltin():
		id, _ := ast.Unparen(call.Fun).(*ast.Ident)
		if id == nil {
			return false
		}
		switch id.Name {
		case "append":
			if s, ok := under(info.TypeOf(call)).(*types.Slice); ok && call.Args[0] != arg && !call.Ellipsis.IsValid() {
				return concrete(s.Elem())
			}
			return true
		case "delete":
			if m, ok := under(info.TypeOf(call.Args[0])).(*types.Map); ok && call.Args[0] != arg {
				return concrete(m.Key())
			}
			return true
		case "len", "cap", "copy", "clear", "min", "max", "close":
			return true
		}
		return false
	}
	if generic(info, call.Fun) {
		return false // its parameter may be a type parameter
	}
	sig, ok := under(tv.Type).(*types.Signature)
	if !ok {
		return false
	}
	ps := sig.Params()
	for i, a := range call.Args {
		if a != arg {
			continue
		}
		switch {
		case sig.Variadic() && i >= ps.Len()-1 && !call.Ellipsis.IsValid():
			return concrete(ps.At(ps.Len() - 1).Type().(*types.Slice).Elem())
		case i < ps.Len():
			return concrete(ps.At(i).Type())
		}
	}
	return false
}

// generic reports whether fun names an instance of a generic function.
func generic(info *types.Info, fun ast.Expr) bool {
	switch f := ast.Unparen(fun).(type) {
	case *ast.Ident:
		_, ok := info.Instances[f]
		return ok
	case *ast.SelectorExpr:
		_, ok := info.Instances[f.Sel]
		return ok
	case *ast.IndexExpr:
		return true
	case *ast.IndexListExpr:
		return true
	}
	return false
}

// element is the type of a composite literal's element: the i'th field
// of a struct (or the one key names), or a slice's, array's or map's
// element; nil when not known.
func element(lit types.Type, i int, key ast.Expr) types.Type {
	switch u := under(lit).(type) {
	case *types.Struct:
		if id, ok := key.(*ast.Ident); ok {
			for j := 0; j < u.NumFields(); j++ {
				if u.Field(j).Name() == id.Name {
					return u.Field(j).Type()
				}
			}
			return nil
		}
		if i >= 0 && i < u.NumFields() {
			return u.Field(i).Type()
		}
	case *types.Slice:
		return u.Elem()
	case *types.Array:
		return u.Elem()
	case *types.Map:
		return u.Elem()
	}
	return nil
}

// enclosingResults is the results of the function the end of stack is
// in, or nil.
func enclosingResults(info *types.Info, stack []ast.Node) *types.Tuple {
	for i := len(stack) - 1; i >= 0; i-- {
		var t types.Type
		switch fn := stack[i].(type) {
		case *ast.FuncLit:
			t = info.TypeOf(fn)
		case *ast.FuncDecl:
			if o := info.Defs[fn.Name]; o != nil {
				t = o.Type()
			}
		default:
			continue
		}
		if sig, ok := t.(*types.Signature); ok {
			return sig.Results()
		}
		return nil
	}
	return nil
}

// concrete reports whether a value put where t is wanted keeps its type:
// t is known and neither an interface nor a type parameter.
func concrete(t types.Type) bool {
	return t != nil && !types.IsInterface(t)
}
