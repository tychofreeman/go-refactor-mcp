// Package analysis holds the expression and data-flow facts that several
// refactorings need to decide whether moving or duplicating an expression
// preserves behavior.
//
// The central question is "can this expression's value change, or does
// evaluating it do anything?". Effects answers it with a Summary of what
// evaluating code can do: its side effects, what it reads that other
// code can change, how it can panic and what new values it makes, from
// one walk of the code and one table of standard-library facts, with the
// module's own functions known by their bodies. Summary.Stable says
// whether an expression's value depends only on constants, functions and
// local variables that no other code can reach.
package analysis

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// Func holds facts about one function body (a FuncDecl, or a FuncLit at
// package level). Closures inside it are part of it.
type Func struct {
	Node ast.Node
	Info *types.Info

	// Addressed holds locals whose address is taken: &v, slicing an array
	// variable, or calling a pointer-receiver method on it.
	Addressed map[*types.Var]bool
	// Captured holds locals referenced inside a closure that does not
	// declare them; the closure may read or write them at any time.
	Captured map[*types.Var]bool
	// CapturedAsync holds the locals in Captured that some closure not
	// known to be synchronous references. A synchronous closure (see
	// SyncLit) runs only while the statement creating it runs, so it
	// cannot touch the variable while other code runs.
	CapturedAsync map[*types.Var]bool
	// AsyncLits lists, for each local in CapturedAsync, the closures not
	// known to be synchronous that reference it.
	AsyncLits map[*types.Var][]*ast.FuncLit
	// Modified lists where each local is written: assignment (also of a
	// field or array element of it), ++/--, or a range loop with =.
	Modified  map[*types.Var][]ast.Node
	freshVars map[*types.Var]bool // the locals that only hold memory the function made, computed on first use (fresh)
}

// SyncLit reports whether lit, whose parent node is parent, only runs
// while the statement creating it runs: it is called immediately (not by
// go or defer), or passed directly to a function known to call it only
// before returning (sort.Slice, ast.Inspect, ...). goDefer holds the calls
// of go and defer statements.
func SyncLit(info *types.Info, lit *ast.FuncLit, parent ast.Node, goDefer map[*ast.CallExpr]bool) bool {
	call, ok := parent.(*ast.CallExpr)
	if !ok || goDefer[call] {
		return false
	}
	if ast.Unparen(call.Fun) == lit {
		return true
	}
	for _, a := range call.Args {
		if ast.Unparen(a) == lit {
			return CallsBeforeReturning(info, call)
		}
	}
	return false
}

// CallsBeforeReturning reports whether call is of a function or method
// known to call the functions passed to it only before it returns
// (ast.Inspect, sort.Slice, the TypeScript compiler's ForEachChild).
func CallsBeforeReturning(info *types.Info, call *ast.CallExpr) bool {
	fn, ok := info.Uses[IdentOf(call.Fun)].(*types.Func)
	if !ok || fn.Pkg() == nil {
		return false
	}
	key := fn.Pkg().Path() + "." + fn.Name()
	if fn.Signature().Recv() != nil {
		key = fn.FullName()
	}
	_, ok = syncCalls[key]
	return ok
}

// AnalyzeFunc computes the facts for fn.
func AnalyzeFunc(info *types.Info, fn ast.Node) *Func {
	f := &Func{
		Node: fn, Info: info,
		Addressed:     map[*types.Var]bool{},
		Captured:      map[*types.Var]bool{},
		CapturedAsync: map[*types.Var]bool{},
		AsyncLits:     map[*types.Var][]*ast.FuncLit{},
		Modified:      map[*types.Var][]ast.Node{},
	}
	address := func(e ast.Expr) {
		if v := f.RootVar(e); v != nil {
			f.Addressed[v] = true
		}
	}
	modify := func(e ast.Expr) {
		if v := f.RootVar(e); v != nil {
			f.Modified[v] = append(f.Modified[v], e)
		}
	}
	var lits []*ast.FuncLit
	sync := map[*ast.FuncLit]bool{}
	goDefer := map[*ast.CallExpr]bool{}
	var stack []ast.Node
	ast.Inspect(fn, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		var parent ast.Node
		if len(stack) > 0 {
			parent = stack[len(stack)-1]
		}
		stack = append(stack, n)
		switch n := n.(type) {
		case *ast.GoStmt:
			goDefer[n.Call] = true
		case *ast.DeferStmt:
			goDefer[n.Call] = true
		case *ast.ParenExpr:
			// SyncLit looks through parentheses; keep the call as the parent.
			if len(stack) > 1 {
				stack[len(stack)-1] = parent
			}
		case *ast.FuncLit:
			lits = append(lits, n)
			sync[n] = SyncLit(info, n, parent, goDefer)
		case *ast.UnaryExpr:
			if n.Op == token.AND {
				address(n.X)
			}
		case *ast.SliceExpr:
			if IsArray(info.TypeOf(n.X)) {
				address(n.X)
			}
		case *ast.SelectorExpr:
			if sel := info.Selections[n]; sel != nil && sel.Kind() == types.MethodVal && PtrRecv(sel) && !IsPointer(info.TypeOf(n.X)) {
				address(n.X)
			}
		case *ast.AssignStmt:
			for _, l := range n.Lhs {
				if id, ok := l.(*ast.Ident); ok && n.Tok == token.DEFINE && info.Defs[id] != nil {
					continue // a new variable, not a modification
				}
				modify(l)
			}
		case *ast.IncDecStmt:
			modify(n.X)
		case *ast.RangeStmt:
			if n.Tok == token.ASSIGN {
				if n.Key != nil {
					modify(n.Key)
				}
				if n.Value != nil {
					modify(n.Value)
				}
			}
		}
		return true
	})
	for _, lit := range lits {
		ast.Inspect(lit.Body, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				if v, ok := info.Uses[id].(*types.Var); ok && f.IsLocal(v) && (v.Pos() < lit.Pos() || v.Pos() >= lit.End()) {
					f.Captured[v] = true
					if !sync[lit] {
						f.CapturedAsync[v] = true
						if l := f.AsyncLits[v]; len(l) == 0 || l[len(l)-1] != lit {
							f.AsyncLits[v] = append(l, lit)
						}
					}
				}
			}
			return true
		})
	}
	return f
}

// Outside returns a Func for code in no function, or parsed apart from
// any (a value given for a new argument): no local is known to escape,
// and no type assertion to be one with a comma, ok.
func Outside(info *types.Info) *Func {
	return &Func{Info: info, Addressed: map[*types.Var]bool{}, Captured: map[*types.Var]bool{}, CapturedAsync: map[*types.Var]bool{}, AsyncLits: map[*types.Var][]*ast.FuncLit{}, Modified: map[*types.Var][]ast.Node{}}
}

// FuncAround returns the facts of the function some code is in, from
// path, the nodes around it in either order: the outermost FuncDecl or
// FuncLit, whose facts cover the literals in it; or, when there is none,
// those of code in no function.
func FuncAround(info *types.Info, path []ast.Node) *Func {
	var fn ast.Node
	for _, n := range path {
		switch n.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			if fn == nil || n.Pos() < fn.Pos() {
				fn = n
			}
		}
	}
	if fn == nil {
		return Outside(info)
	}
	return AnalyzeFunc(info, fn)
}

// Escapes reports whether code other than direct reads and writes in this
// function body can reach v.
func (f *Func) Escapes(v *types.Var) bool { return f.Addressed[v] || f.Captured[v] }

// IsLocal reports whether v is a variable declared in this function
// (including its parameters and results).
func (f *Func) IsLocal(v *types.Var) bool {
	return f.Node != nil && !v.IsField() && v.Parent() != nil && v.Pkg() != nil && v.Parent() != v.Pkg().Scope() &&
		v.Pos() >= f.Node.Pos() && v.Pos() < f.Node.End()
}

// RootVar returns the local variable that e is part of (through fields of
// struct values and elements of arrays), or nil.
func (f *Func) RootVar(e ast.Expr) *types.Var {
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		if v, ok := f.Info.ObjectOf(e).(*types.Var); ok && f.IsLocal(v) {
			return v
		}
	case *ast.SelectorExpr:
		if sel := f.Info.Selections[e]; sel != nil && sel.Kind() == types.FieldVal && !IsPointer(f.Info.TypeOf(e.X)) {
			return f.RootVar(e.X)
		}
	case *ast.IndexExpr:
		if IsArray(f.Info.TypeOf(e.X)) {
			return f.RootVar(e.X)
		}
	}
	return nil
}

// LocalsRead returns the local variables e reads, in order of appearance.
func (f *Func) LocalsRead(e ast.Node) []*types.Var {
	var out []*types.Var
	seen := map[*types.Var]bool{}
	for _, id := range ScopeRefs(f.Info, e) {
		if v, ok := f.Info.Uses[id].(*types.Var); ok && f.IsLocal(v) && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// AddressOperands returns the operands whose values e's address depends
// on (a pointer it goes through, a slice and the index into it), or, if
// its address is not made of variables and such operands, the
// sub-expression that is not.
func (f *Func) AddressOperands(e ast.Expr) ([]ast.Expr, ast.Node) {
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		if _, ok := f.Info.Uses[e].(*types.Var); ok {
			return nil, nil
		}
	case *ast.SelectorExpr:
		if sel := f.Info.Selections[e]; sel != nil && sel.Kind() == types.FieldVal {
			if sel.Indirect() || IsPointer(f.Info.TypeOf(e.X)) {
				return []ast.Expr{e.X}, nil
			}
			return f.AddressOperands(e.X)
		}
	case *ast.IndexExpr:
		if IsArray(f.Info.TypeOf(e.X)) {
			ops, c := f.AddressOperands(e.X)
			if c != nil {
				return nil, c
			}
			return append(ops, e.Index), nil
		}
		return []ast.Expr{e.X, e.Index}, nil
	case *ast.StarExpr:
		return []ast.Expr{e.X}, nil
	}
	return nil, e
}

// Addressed returns the field selections in n whose address an & takes
// rather than reading them: the x.f of &x.f, and x.g of &x.g.f when g is
// a struct value. Each maps to the pointer it goes through (x, when x is
// a pointer), or nil: taking the address reads only that pointer, and
// can panic only when it is nil.
func Addressed(info *types.Info, n ast.Node) map[*ast.SelectorExpr]ast.Expr {
	out := map[*ast.SelectorExpr]ast.Expr{}
	ast.Inspect(n, func(n ast.Node) bool {
		u, ok := n.(*ast.UnaryExpr)
		if !ok || u.Op != token.AND {
			return true
		}
		e := ast.Unparen(u.X)
		for {
			sel, ok := e.(*ast.SelectorExpr)
			if !ok {
				break
			}
			s := info.Selections[sel]
			if s == nil || s.Kind() != types.FieldVal {
				break
			}
			if s.Indirect() {
				if len(s.Index()) == 1 && IsPointer(info.TypeOf(sel.X)) {
					out[sel] = sel.X
				}
				break
			}
			out[sel] = nil
			e = ast.Unparen(sel.X)
		}
		return true
	})
	return out
}

// addressedIn returns the operands in n whose address an & takes rather
// than reading them: the field selections of Addressed, and indexed
// elements (&xs[i] reads xs and i, not the element). Each maps to the
// pointer the address goes through, or nil.
func addressedIn(info *types.Info, n ast.Node) map[ast.Expr]ast.Expr {
	out := map[ast.Expr]ast.Expr{}
	for sel, p := range Addressed(info, n) {
		out[sel] = p
	}
	ast.Inspect(n, func(n ast.Node) bool {
		if u, ok := n.(*ast.UnaryExpr); ok && u.Op == token.AND {
			if ix, ok := ast.Unparen(u.X).(*ast.IndexExpr); ok {
				out[ix] = nil
			}
		}
		return true
	})
	return out
}

// DependsOnPosition reports whether the value of vs, a spec of a const
// group, depends on where it stands in the group: it uses iota, the
// spec's index, or it has no values and repeats an earlier spec's
// expression. Taking out a spec before it changes its value.
func DependsOnPosition(info *types.Info, vs *ast.ValueSpec) bool {
	if len(vs.Values) == 0 {
		return true
	}
	uses := false
	for _, v := range vs.Values {
		ast.Inspect(v, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && info.Uses[id] == types.Universe.Lookup("iota") {
				uses = true
			}
			return !uses
		})
	}
	return uses
}

func isFloatOrComplex(t types.Type) bool {
	b, ok := under(t).(*types.Basic)
	return ok && b.Info()&(types.IsFloat|types.IsComplex) != 0
}

func isUnsigned(t types.Type) bool {
	b, ok := under(t).(*types.Basic)
	return ok && b.Info()&types.IsUnsigned != 0
}

// ScopeRefs lists the identifiers in e that are resolved through lexical
// scope: not field or method names after a dot, and not field keys in
// struct literals.
func ScopeRefs(info *types.Info, e ast.Node) []*ast.Ident {
	skip := map[*ast.Ident]bool{}
	ast.Inspect(e, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			skip[n.Sel] = true
		case *ast.CompositeLit:
			if IsStruct(info.TypeOf(n)) {
				for _, el := range n.Elts {
					if kv, ok := el.(*ast.KeyValueExpr); ok {
						if id, ok := kv.Key.(*ast.Ident); ok {
							skip[id] = true
						}
					}
				}
			}
		}
		return true
	})
	var out []*ast.Ident
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && !skip[id] {
			out = append(out, id)
		}
		return true
	})
	return out
}

// PathTo returns the nodes enclosing target, innermost (target) first, or
// nil if target is not in root.
func PathTo(root, target ast.Node) []ast.Node {
	var stack, found []ast.Node
	ast.Inspect(root, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if n.Pos() > target.Pos() || n.End() < target.End() {
			return false
		}
		stack = append(stack, n)
		if n == target {
			for i := len(stack) - 1; i >= 0; i-- {
				found = append(found, stack[i])
			}
			return false
		}
		return true
	})
	return found
}

// OutermostFunc returns the outermost FuncDecl or FuncLit on path.
func OutermostFunc(path []ast.Node) ast.Node {
	var fn ast.Node
	for _, n := range path {
		switch n.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			fn = n
		}
	}
	return fn
}

// TypeString spells t as it must appear in file, which belongs to pkg,
// using the file's import names. If t needs a package the file does not
// import, missing is that package's path.
func TypeString(t types.Type, file *ast.File, pkg *types.Package, imports map[string]*types.Package) (s, missing string) {
	names := map[string]string{}
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		switch {
		case imp.Name != nil && imp.Name.Name == "_":
		case imp.Name != nil && imp.Name.Name == ".":
			names[path] = ""
		case imp.Name != nil:
			names[path] = imp.Name.Name
		default:
			if p := imports[path]; p != nil {
				names[path] = p.Name()
			}
		}
	}
	s = types.TypeString(t, func(p *types.Package) string {
		if p == pkg {
			return ""
		}
		if n, ok := names[p.Path()]; ok {
			return n
		}
		missing = p.Path()
		return p.Name()
	})
	return s, missing
}

// ImportMap returns pkg's direct imports by path.
func ImportMap(pkg *types.Package) map[string]*types.Package {
	m := map[string]*types.Package{}
	for _, p := range pkg.Imports() {
		m[p.Path()] = p
	}
	return m
}

// IdentOf returns the identifier naming e (x, pkg.x, x[T]), or nil.
func IdentOf(e ast.Expr) *ast.Ident {
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		return e
	case *ast.SelectorExpr:
		return e.Sel
	case *ast.IndexExpr:
		return IdentOf(e.X)
	case *ast.IndexListExpr:
		return IdentOf(e.X)
	}
	return nil
}

// Defines reports whether id is defined where it appears, rather than
// assigned: a new variable of :=, or the symbolic variable of a type
// switch (switch v := x.(type)), whose Defs entry is nil. A variable that
// := redeclares is in Uses instead.
func Defines(info *types.Info, id *ast.Ident) bool {
	_, ok := info.Defs[id]
	return ok
}

// PtrRecv reports whether the selected method has a pointer receiver.
func PtrRecv(sel *types.Selection) bool {
	fn, ok := sel.Obj().(*types.Func)
	if !ok {
		return false
	}
	recv := fn.Signature().Recv()
	return recv != nil && IsPointer(recv.Type())
}

func IsPointer(t types.Type) bool {
	_, ok := under(t).(*types.Pointer)
	return ok
}

func IsArray(t types.Type) bool {
	_, ok := under(t).(*types.Array)
	return ok
}

func IsSlice(t types.Type) bool {
	_, ok := under(t).(*types.Slice)
	return ok
}

// IsStruct reports whether t is a struct or a pointer to one.
func IsStruct(t types.Type) bool {
	if p, ok := under(t).(*types.Pointer); ok {
		t = p.Elem()
	}
	_, ok := under(t).(*types.Struct)
	return ok
}

func IsString(t types.Type) bool {
	b, ok := under(t).(*types.Basic)
	return ok && b.Info()&types.IsString != 0
}

func IsMapOrChan(t types.Type) bool {
	switch under(t).(type) {
	case *types.Map, *types.Chan:
		return true
	}
	return false
}

// IsReference reports whether values of t refer to shared storage.
func IsReference(t types.Type) bool {
	switch under(t).(type) {
	case *types.Slice, *types.Map, *types.Chan, *types.Pointer, *types.Signature:
		return true
	}
	return false
}

func under(t types.Type) types.Type {
	if t == nil {
		return nil
	}
	return t.Underlying()
}
