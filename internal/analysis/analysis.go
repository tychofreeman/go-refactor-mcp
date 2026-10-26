// Package analysis holds the expression and data-flow facts that several
// refactorings need to decide whether moving or duplicating an expression
// preserves behavior.
//
// The central question is "can this expression's value change, or does
// evaluating it do anything?". Stable answers it conservatively: an
// expression is stable when its value depends only on constants, functions
// and local variables that no other code can reach.
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
	Modified map[*types.Var][]ast.Node
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
	fn, ok := info.Uses[IdentOf(call.Fun)].(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Signature().Recv() != nil {
		return false
	}
	for _, a := range call.Args {
		if ast.Unparen(a) == lit {
			_, ok := syncCalls[fn.Pkg().Path()+"."+fn.Name()]
			return ok
		}
	}
	return false
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

// Outside returns a Func for code whose function is not known: no local
// is known to escape, and no type assertion to be one with a comma, ok.
func Outside(info *types.Info) *Func {
	return &Func{Info: info, Addressed: map[*types.Var]bool{}, Captured: map[*types.Var]bool{}, CapturedAsync: map[*types.Var]bool{}, AsyncLits: map[*types.Var][]*ast.FuncLit{}, Modified: map[*types.Var][]ast.Node{}}
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
func (f *Func) LocalsRead(e ast.Expr) []*types.Var {
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

// Stable reports whether e's value depends only on constants, functions and
// local variables that no other code can reach (no calls, no pointer or
// slice/map element reads, no globals, no escaping locals). Evaluating a
// stable expression has no side effects, and its value can only change if
// one of its local variables is assigned.
//
// With sharing, e must also not create a new reference value (a slice or
// map literal, say), so that one shared value is equivalent to evaluating
// e repeatedly.
//
// A stable expression can still panic (a / b, s[i]): moving its
// evaluation also needs MayPanic.
//
// It returns the first offending sub-expression.
func (f *Func) Stable(e ast.Expr, sharing bool) (ast.Node, bool) {
	info := f.Info
	ok := func(es ...ast.Expr) (ast.Node, bool) {
		for _, s := range es {
			if s == nil {
				continue
			}
			if c, good := f.Stable(s, sharing); !good {
				return c, false
			}
		}
		return nil, true
	}
	switch e := e.(type) {
	case *ast.BasicLit:
		return nil, true
	case *ast.Ident:
		switch o := info.Uses[e].(type) {
		case *types.Const, *types.Nil, *types.Func:
			return nil, true
		case *types.Var:
			if f.IsLocal(o) && !f.Escapes(o) {
				return nil, true
			}
		}
		return e, false
	case *ast.ParenExpr:
		return ok(e.X)
	case *ast.UnaryExpr:
		switch e.Op {
		case token.ADD, token.SUB, token.NOT, token.XOR:
			return ok(e.X)
		}
		return e, false
	case *ast.BinaryExpr:
		return ok(e.X, e.Y)
	case *ast.SelectorExpr:
		sel := info.Selections[e]
		if sel == nil { // qualified identifier
			switch info.Uses[e.Sel].(type) {
			case *types.Const, *types.Func:
				return nil, true
			}
			return e, false
		}
		if sel.Kind() == types.FieldVal && !sel.Indirect() && !IsPointer(info.TypeOf(e.X)) {
			return ok(e.X)
		}
		return e, false
	case *ast.IndexExpr:
		if t := info.TypeOf(e.X); IsArray(t) || IsString(t) {
			return ok(e.X, e.Index)
		}
		return e, false
	case *ast.SliceExpr:
		if t := info.TypeOf(e.X); IsString(t) || IsSlice(t) {
			return ok(e.X, e.Low, e.High, e.Max)
		}
		return e, false
	case *ast.TypeAssertExpr:
		if e.Type != nil {
			return ok(e.X)
		}
		return e, false
	case *ast.CallExpr:
		if tv, isConv := info.Types[e.Fun]; isConv && tv.IsType() {
			if sharing && IsReference(info.TypeOf(e)) {
				return e, false
			}
			return ok(e.Args...)
		}
		if b, isBuiltin := info.Uses[IdentOf(e.Fun)].(*types.Builtin); isBuiltin {
			switch b.Name() {
			case "len", "cap":
				if len(e.Args) == 1 && !IsMapOrChan(info.TypeOf(e.Args[0])) {
					return ok(e.Args...)
				}
			case "min", "max", "real", "imag", "complex":
				return ok(e.Args...)
			}
		}
		if known, panics := KnownCall(info, e); known && panics == NoPanic {
			if sharing && IsReference(info.TypeOf(e)) {
				return e, false // a new slice each time, say
			}
			args := e.Args
			if sel, isSel := ast.Unparen(e.Fun).(*ast.SelectorExpr); isSel && info.Selections[sel] != nil {
				args = append([]ast.Expr{sel.X}, args...) // a method's receiver
			}
			return ok(args...)
		}
		return e, false
	case *ast.CompositeLit:
		t := info.TypeOf(e)
		if sharing && !IsStruct(t) && !IsArray(t) {
			return e, false
		}
		for _, el := range e.Elts {
			if kv, isKV := el.(*ast.KeyValueExpr); isKV {
				if !IsStruct(t) {
					if c, good := ok(kv.Key); !good {
						return c, false
					}
				}
				el = kv.Value
			}
			if c, good := ok(el); !good {
				return c, false
			}
		}
		return nil, true
	}
	return e, false
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

// StableAddress is Stable for e's address rather than its value: it
// depends only on operands that are stable.
func (f *Func) StableAddress(e ast.Expr) (ast.Node, bool) {
	ops, culprit := f.AddressOperands(e)
	if culprit != nil {
		return culprit, false
	}
	for _, op := range ops {
		if c, ok := f.Stable(op, false); !ok {
			return c, false
		}
	}
	return nil, true
}

// MayPanic returns the first sub-expression of e whose evaluation can panic,
// or nil: an integer division or remainder by a divisor that is not
// constant, a shift by a signed count that is not constant, an index that
// can be out of range or of a nil pointer, a slice expression with bounds,
// a type assertion, or a conversion of a slice to an array. It does not
// look into calls or function literals; it is meant for expressions Stable
// accepts, whose known calls cannot panic.
func MayPanic(info *types.Info, e ast.Expr) ast.Node {
	var found ast.Node
	ast.Inspect(e, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.BinaryExpr:
			switch n.Op {
			case token.QUO, token.REM:
				if !isFloatOrComplex(info.TypeOf(n)) && info.Types[n.Y].Value == nil {
					found = n
				}
			case token.SHL, token.SHR:
				if !isUnsigned(info.TypeOf(n.Y)) && info.Types[n.Y].Value == nil {
					found = n
				}
			}
		case *ast.IndexExpr:
			switch t := info.TypeOf(n.X); {
			case IsArray(t):
				if info.Types[n.Index].Value == nil {
					found = n
				}
			case IsString(t):
				if info.Types[n.X].Value == nil || info.Types[n.Index].Value == nil {
					found = n
				}
			case IsSlice(t), IsPointer(t):
				found = n
			}
		case *ast.SliceExpr:
			if n.Low != nil || n.High != nil || n.Max != nil || IsPointer(info.TypeOf(n.X)) {
				found = n
			}
		case *ast.TypeAssertExpr:
			if n.Type != nil {
				found = n
			}
		case *ast.CallExpr:
			if tv, isConv := info.Types[n.Fun]; isConv && tv.IsType() && len(n.Args) == 1 && IsSlice(info.TypeOf(n.Args[0])) {
				switch u := tv.Type.Underlying().(type) {
				case *types.Array:
					found = n
				case *types.Pointer:
					if _, ok := u.Elem().Underlying().(*types.Array); ok {
						found = n
					}
				}
			}
		}
		return found == nil
	})
	return found
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

// HasEffect reports whether n itself (not its children) is a call with
// possible side effects or a channel receive.
func HasEffect(info *types.Info, n ast.Node) bool {
	switch n := n.(type) {
	case *ast.CallExpr:
		if tv, ok := info.Types[n.Fun]; ok && tv.IsType() {
			return false
		}
		if b, ok := info.Uses[IdentOf(n.Fun)].(*types.Builtin); ok {
			switch b.Name() {
			case "len", "cap", "min", "max", "real", "imag", "complex", "new", "make":
				return false
			}
		}
		if known, panics := KnownCall(info, n); known && panics == NoPanic {
			return false
		}
		return true
	case *ast.UnaryExpr:
		return n.Op == token.ARROW
	}
	return false
}

// FirstEffectBefore returns the first node with side effects inside root
// that is evaluated before pos (it ends before pos and does not enclose
// it), ignoring the bodies of function literals and any node in skip.
func FirstEffectBefore(info *types.Info, root ast.Node, pos token.Pos, skip ast.Node) ast.Node {
	var first ast.Node
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil || first != nil || n.Pos() >= pos || (skip != nil && n == skip) {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false // defining a closure runs nothing
		}
		if n.End() <= pos && HasEffect(info, n) {
			first = n
			return false
		}
		return true
	})
	return first
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
