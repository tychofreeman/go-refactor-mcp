package analysis

import (
	"go/ast"
	"go/token"
	"go/types"
)

// Summary says what evaluating something can do besides computing its
// value. Each field describes the first such thing found ("calls
// os.Exit"), or is "".
type Summary struct {
	Effect string // a side effect: an assignment others can see, I/O, a channel operation
	Reads  string // a read of what other code can change: a package variable, memory through a pointer
	Unsafe string // something that could panic or not finish
}

// Effects summarizes calls: of functions known to have no side effects,
// and of the functions of the packages added to it, from their bodies.
type Effects struct {
	decls  map[*types.Func]funcSource
	memo   map[*types.Func]*Summary
	active map[*types.Func]bool // being summarized: a call back to one is recursion
}

// funcSource is a function's declaration and the type information of
// the package it was checked in.
type funcSource struct {
	decl *ast.FuncDecl
	info *types.Info
}

// NewEffects returns an Effects that knows no packages yet.
func NewEffects() *Effects {
	return &Effects{decls: map[*types.Func]funcSource{}, memo: map[*types.Func]*Summary{}, active: map[*types.Func]bool{}}
}

// AddPackage lets e summarize the functions declared in files, which
// were type-checked into info.
func (e *Effects) AddPackage(files []*ast.File, info *types.Info) {
	for _, f := range files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				if fn, ok := info.Defs[fd.Name].(*types.Func); ok {
					e.decls[fn] = funcSource{fd, info}
				}
			}
		}
	}
}

// Func summarizes what calling fn can do, or returns nil when that is not
// known (its body is not available).
func (e *Effects) Func(fn *types.Func) *Summary {
	fn = fn.Origin()
	if s, ok := e.memo[fn]; ok {
		return s
	}
	src, ok := e.decls[fn]
	if !ok || e.active[fn] {
		return nil // no body, or recursion: not known
	}
	e.active[fn] = true
	s := e.summarize(src)
	delete(e.active, fn)
	e.memo[fn] = s
	return s
}

// Call summarizes what call itself does, apart from evaluating its
// function and arguments.
func (e *Effects) Call(info *types.Info, call *ast.CallExpr) Summary {
	return e.call(info, call, nil)
}

func (e *Effects) call(info *types.Info, call *ast.CallExpr, private func(target ast.Expr, inPlace bool) bool) Summary {
	name := types.ExprString(call.Fun)
	if tv, ok := info.Types[call.Fun]; ok && tv.IsType() {
		return Summary{} // a conversion
	}
	if b, ok := info.Uses[IdentOf(call.Fun)].(*types.Builtin); ok {
		return builtinCall(info, call, b.Name())
	}
	if fn, ok := info.Uses[IdentOf(call.Fun)].(*types.Func); ok {
		if stopsFuncs[fn.FullName()] {
			return Summary{Effect: "calls " + name, Unsafe: "calls " + name + ", which does not return"}
		}
		if s, ok := stdCall(info, fn, call, private); ok {
			return s
		}
		if s, ok := syncCallSummary(info, fn, call, private); ok {
			return s
		}
	}
	fn := staticCallee(info, call)
	if fn == nil {
		return Summary{Effect: "calls " + name, Unsafe: "calls " + name + ", which could be any function"}
	}
	var s Summary
	if call.Ellipsis.IsValid() {
		s.Reads = "spreads " + types.ExprString(call.Args[len(call.Args)-1])
	}
	fs := e.Func(fn)
	if fs == nil {
		return Summary{Effect: "calls " + name}
	}
	which := func(what string) string {
		if what == "" {
			return ""
		}
		return "calls " + name + ", which " + what
	}
	s.add(Summary{Effect: which(fs.Effect), Reads: which(fs.Reads), Unsafe: which(fs.Unsafe)})
	return s
}

// add fills in the fields of s that are still empty from o.
func (s *Summary) add(o Summary) {
	if s.Effect == "" {
		s.Effect = o.Effect
	}
	if s.Reads == "" {
		s.Reads = o.Reads
	}
	if s.Unsafe == "" {
		s.Unsafe = o.Unsafe
	}
}

// effectFreeFuncs lists functions without side effects that knownFuncs
// leaves out, because their results are new values that do not compare
// equal (errors), with how each can panic.
var effectFreeFuncs = map[string]PanicKind{
	"errors.New":       NoPanic,
	"strconv.Atoi":     NoPanic,
	"strconv.ParseInt": NoPanic, "strconv.ParseUint": NoPanic,
	"strconv.ParseFloat": NoPanic, "strconv.ParseBool": NoPanic,
}

// stopsFuncs are functions that end the program or the goroutine rather
// than return.
var stopsFuncs = map[string]bool{
	"os.Exit": true, "runtime.Goexit": true,
	"log.Fatal": true, "log.Fatalf": true, "log.Fatalln": true,
	"log.Panic": true, "log.Panicf": true, "log.Panicln": true,
	"(*log.Logger).Fatal": true, "(*log.Logger).Fatalf": true, "(*log.Logger).Fatalln": true,
	"(*log.Logger).Panic": true, "(*log.Logger).Panicf": true, "(*log.Logger).Panicln": true,
	"(*testing.common).FailNow": true, "(*testing.common).Fatal": true, "(*testing.common).Fatalf": true,
	"(*testing.common).Skip": true, "(*testing.common).Skipf": true, "(*testing.common).SkipNow": true,
}

// syncCall is how a function that calls its function arguments only
// before it returns, and never keeps them, uses its other arguments.
type syncCall struct {
	reads, writes bool   // its first argument: it reads it, or changes it in place
	readsAll      bool   // it reads every argument that is not a function
	effect        string // what else it does, if anything
	unsafe        string // how it can panic, if it can
}

var (
	reads      = syncCall{reads: true}
	readWrites = syncCall{reads: true, writes: true}
)

// syncCalls lists the functions whose literal function arguments run
// during the call, so they are summarized as part of the caller.
var syncCalls = map[string]syncCall{
	"go/ast.Inspect": reads,
	"sort.Slice":     readWrites, "sort.SliceStable": readWrites, "sort.SliceIsSorted": reads, "sort.Search": {},
	"slices.SortFunc": readWrites, "slices.SortStableFunc": readWrites, "slices.IsSortedFunc": reads,
	"slices.IndexFunc": reads, "slices.ContainsFunc": reads, "slices.DeleteFunc": readWrites,
	"slices.CompactFunc": readWrites, "slices.BinarySearchFunc": reads,
	"slices.EqualFunc": {readsAll: true}, "slices.CompareFunc": {readsAll: true},
	"slices.MaxFunc":  {reads: true, unsafe: "panics on an empty slice"},
	"slices.MinFunc":  {reads: true, unsafe: "panics on an empty slice"},
	"maps.DeleteFunc": readWrites,
	// Strings cannot change; byte slices can.
	"strings.Map": {}, "strings.IndexFunc": {}, "strings.LastIndexFunc": {}, "strings.ContainsFunc": {},
	"strings.FieldsFunc": {}, "strings.TrimFunc": {}, "strings.TrimLeftFunc": {}, "strings.TrimRightFunc": {},
	"bytes.Map": reads, "bytes.IndexFunc": reads, "bytes.LastIndexFunc": reads, "bytes.ContainsFunc": reads,
	"bytes.FieldsFunc": reads, "bytes.TrimFunc": reads, "bytes.TrimLeftFunc": reads, "bytes.TrimRightFunc": reads,
	"path/filepath.Walk":    {effect: "walks the file system"},
	"path/filepath.WalkDir": {effect: "walks the file system"},
	"io/fs.WalkDir":         {effect: "walks a file system"},
}

// SyncLits returns the function literals passed to call that run during
// it, when it calls a function that calls them only before it returns
// (ast.Inspect, sort.Slice, ...). Their bodies are part of what the call
// does, which Call leaves to the caller to walk.
func SyncLits(info *types.Info, call *ast.CallExpr) []*ast.FuncLit {
	fn := staticCallee(info, call)
	if fn == nil || fn.Pkg() == nil || fn.Signature().Recv() != nil {
		return nil
	}
	if _, ok := syncCalls[fn.Pkg().Path()+"."+fn.Name()]; !ok {
		return nil
	}
	var lits []*ast.FuncLit
	for _, a := range call.Args {
		if lit, ok := ast.Unparen(a).(*ast.FuncLit); ok {
			lits = append(lits, lit)
		}
	}
	return lits
}

// syncCallSummary summarizes a call of a function in syncCalls, apart
// from the literal functions passed to it (see SyncLits): its reads and
// writes of its first argument, and a function value passed to it, which
// could do anything.
func syncCallSummary(info *types.Info, fn *types.Func, call *ast.CallExpr, private func(target ast.Expr, inPlace bool) bool) (Summary, bool) {
	if fn.Pkg() == nil || fn.Signature().Recv() != nil {
		return Summary{}, false
	}
	sc, ok := syncCalls[fn.Pkg().Path()+"."+fn.Name()]
	if !ok {
		return Summary{}, false
	}
	s := Summary{Effect: sc.effect, Unsafe: sc.unsafe}
	for i, a := range call.Args {
		_, isLit := ast.Unparen(a).(*ast.FuncLit)
		_, isFunc := info.TypeOf(a).Underlying().(*types.Signature)
		switch {
		case isLit:
		case isFunc:
			s.Effect = "passes " + types.ExprString(a) + " to " + types.ExprString(call.Fun)
		case (i == 0 || sc.readsAll) && (private == nil || !private(a, false)):
			if sc.writes && s.Effect == "" {
				s.Effect = "changes " + types.ExprString(a)
			}
			if (sc.reads || sc.readsAll) && s.Reads == "" {
				s.Reads = "reads " + types.ExprString(a)
			}
		}
	}
	return s, true
}

// staticCallee returns the function call calls, when that is known
// before it runs: not a function value or an interface method.
func staticCallee(info *types.Info, call *ast.CallExpr) *types.Func {
	fn, ok := info.Uses[IdentOf(call.Fun)].(*types.Func)
	if !ok {
		return nil
	}
	if recv := fn.Signature().Recv(); recv != nil && types.IsInterface(recv.Type()) {
		return nil
	}
	return fn
}

// builtinCall summarizes a call of the built-in function name, as far as
// the call shows: writes through its arguments count as side effects.
func builtinCall(info *types.Info, call *ast.CallExpr, name string) Summary {
	switch name {
	case "make":
		// A slice's length or capacity, or a channel's buffer size, that is
		// negative or too large panics; a map's size is only a hint.
		if _, isMap := under(info.TypeOf(call)).(*types.Map); !isMap {
			for _, a := range call.Args[1:] {
				if !nonNegative(info, a) {
					return Summary{Unsafe: "makes " + types.ExprString(call.Args[0]) + " of a size that may be out of range"}
				}
			}
		}
		return Summary{}
	case "len", "cap", "min", "max", "real", "imag", "complex", "new":
		return Summary{}
	case "panic":
		return Summary{Unsafe: "panics"}
	}
	return Summary{Effect: "calls " + name}
}

// nonNegative reports whether size, an integer, cannot be negative as far
// as it shows: a constant, a length or capacity, or a sum of them.
func nonNegative(info *types.Info, size ast.Expr) bool {
	switch x := ast.Unparen(size).(type) {
	case *ast.CallExpr:
		b, ok := info.Uses[IdentOf(x.Fun)].(*types.Builtin)
		return ok && (b.Name() == "len" || b.Name() == "cap")
	case *ast.BinaryExpr:
		return x.Op == token.ADD && nonNegative(info, x.X) && nonNegative(info, x.Y)
	}
	return info.Types[size].Value != nil
}

// freshLocals returns the local variables of fd that only ever hold
// memory fd made itself (make, new, a composite literal or its address,
// nil, an append to the variable or a slice of it) and whose address is
// not taken: writing through them is not seen outside the call.
func freshLocals(f *Func, fd *ast.FuncDecl) map[*types.Var]bool {
	info := f.Info
	fresh, bad := map[*types.Var]bool{}, map[*types.Var]bool{}
	note := func(v *types.Var, rhs ast.Expr, ok bool) {
		switch {
		case v == nil || !f.IsLocal(v):
		case !ok || f.Addressed[v] || v.Pos() < fd.Body.Pos() || (rhs != nil && !isFreshValue(info, v, rhs)):
			bad[v] = true
		default:
			fresh[v] = true
		}
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			for i, l := range n.Lhs {
				id, ok := ast.Unparen(l).(*ast.Ident)
				if !ok {
					continue // a write into what a variable holds
				}
				v, _ := info.ObjectOf(id).(*types.Var)
				simple := len(n.Lhs) == len(n.Rhs) && (n.Tok == token.ASSIGN || n.Tok == token.DEFINE)
				var rhs ast.Expr
				if simple {
					rhs = n.Rhs[i]
				}
				note(v, rhs, simple)
			}
		case *ast.ValueSpec:
			for i, id := range n.Names {
				v, _ := info.Defs[id].(*types.Var)
				var rhs ast.Expr
				if i < len(n.Values) {
					rhs = n.Values[i]
				}
				note(v, rhs, len(n.Values) == 0 || len(n.Values) == len(n.Names))
			}
		case *ast.RangeStmt:
			for _, x := range []ast.Expr{n.Key, n.Value} {
				if id, ok := x.(*ast.Ident); ok {
					v, _ := info.ObjectOf(id).(*types.Var)
					note(v, nil, false)
				}
			}
		}
		return true
	})
	for v := range bad {
		delete(fresh, v)
	}
	return fresh
}

// isFreshValue reports whether assigning rhs to v keeps v holding only
// memory the function made itself.
func isFreshValue(info *types.Info, v *types.Var, rhs ast.Expr) bool {
	self := func(e ast.Expr) bool {
		id, ok := ast.Unparen(e).(*ast.Ident)
		return ok && info.Uses[id] == v
	}
	switch r := ast.Unparen(rhs).(type) {
	case *ast.CompositeLit:
		return true
	case *ast.UnaryExpr:
		_, lit := ast.Unparen(r.X).(*ast.CompositeLit)
		return r.Op == token.AND && lit
	case *ast.Ident:
		return info.Types[r].IsNil()
	case *ast.SliceExpr:
		return self(r.X)
	case *ast.CallExpr:
		b, ok := info.Uses[IdentOf(r.Fun)].(*types.Builtin)
		switch {
		case !ok:
		case b.Name() == "make" || b.Name() == "new":
			return true
		case b.Name() == "append":
			return len(r.Args) > 0 && self(r.Args[0])
		}
	}
	return false
}

// body walks one function body for summarize.
type body struct {
	e     *Effects
	f     *Func
	fresh map[*types.Var]bool
	sync  map[*ast.FuncLit]bool // literals whose bodies run during the call
	s     Summary
}

// summarize computes what calling the function declared by src can do,
// from its body: what its own code does, and what the functions it calls
// do. Writes to its own local variables, and through those that hold
// memory it made itself, are not side effects.
func (e *Effects) summarize(src funcSource) *Summary {
	f := AnalyzeFunc(src.info, src.decl)
	b := &body{e: e, f: f, fresh: freshLocals(f, src.decl), sync: map[*ast.FuncLit]bool{}}
	ast.Inspect(src.decl.Body, b.visit)
	return &b.s
}

// pointsPrivate reports whether x (a pointer, slice or map) is a variable
// that only holds memory this call made.
func (b *body) pointsPrivate(x ast.Expr) bool {
	id, ok := ast.Unparen(x).(*ast.Ident)
	if !ok {
		return false
	}
	v, ok := b.f.Info.Uses[id].(*types.Var)
	return ok && b.fresh[v]
}

// private reports whether writing to the assignable expression x changes
// only memory of this call: a local variable or part of one, or memory a
// fresh local points to.
func (b *body) private(x ast.Expr) bool {
	if v := b.f.RootVar(x); v != nil {
		return true
	}
	info := b.f.Info
	switch x := ast.Unparen(x).(type) {
	case *ast.IndexExpr:
		return b.pointsPrivate(x.X)
	case *ast.SelectorExpr:
		return IsPointer(info.TypeOf(x.X)) && b.pointsPrivate(x.X)
	case *ast.StarExpr:
		return b.pointsPrivate(x.X)
	}
	return false
}

func (b *body) set(field *string, what string) {
	if *field == "" {
		*field = what
	}
}

func (b *body) assigns(x ast.Expr) {
	if id, ok := x.(*ast.Ident); ok && id.Name == "_" {
		return
	}
	if !b.private(x) {
		b.set(&b.s.Effect, "assigns to "+types.ExprString(x))
	}
}

// visit records what node n itself does; ast.Inspect walks its children.
func (b *body) visit(n ast.Node) bool {
	info := b.f.Info
	s := &b.s
	switch n := n.(type) {
	case *ast.FuncLit:
		return b.sync[n] // defining a closure runs nothing
	case *ast.GoStmt:
		b.set(&s.Effect, "starts a goroutine")
	case *ast.SendStmt:
		b.set(&s.Effect, "sends on a channel")
		b.set(&s.Unsafe, "sends on a channel, which might block")
	case *ast.SelectStmt:
		b.set(&s.Effect, "selects on channels")
		b.set(&s.Unsafe, "selects on channels, which might block")
	case *ast.UnaryExpr:
		if n.Op == token.ARROW {
			b.set(&s.Effect, "receives from a channel")
			b.set(&s.Unsafe, "receives from a channel, which might block")
		}
	case *ast.ForStmt:
		b.set(&s.Unsafe, "has a loop that might not finish")
	case *ast.RangeStmt:
		switch info.TypeOf(n.X).Underlying().(type) {
		case *types.Chan:
			b.set(&s.Effect, "receives from a channel")
			b.set(&s.Unsafe, "receives from a channel, which might block")
		case *types.Signature:
			b.set(&s.Effect, "ranges over a function")
			b.set(&s.Unsafe, "ranges over a function, which could be any function")
		case *types.Slice, *types.Map, *types.Pointer:
			if !b.pointsPrivate(n.X) {
				b.set(&s.Reads, "reads the elements of "+types.ExprString(n.X))
			}
		}
		if n.Tok == token.ASSIGN {
			for _, x := range []ast.Expr{n.Key, n.Value} {
				if x != nil {
					b.assigns(x)
				}
			}
		}
	case *ast.AssignStmt:
		for _, l := range n.Lhs {
			if id, ok := l.(*ast.Ident); ok && n.Tok == token.DEFINE && Defines(info, id) {
				continue
			}
			b.assigns(l)
		}
		if (n.Tok == token.QUO_ASSIGN || n.Tok == token.REM_ASSIGN) && isInteger(info.TypeOf(n.Lhs[0])) && info.Types[n.Rhs[0]].Value == nil {
			b.set(&s.Unsafe, "divides by "+types.ExprString(n.Rhs[0]))
		}
	case *ast.IncDecStmt:
		b.assigns(n.X)
	case *ast.CallExpr:
		s.add(b.call(n))
	case *ast.StarExpr:
		if !info.Types[n].IsType() && !b.pointsPrivate(n.X) {
			b.set(&s.Reads, "reads through the pointer "+types.ExprString(n.X))
			b.set(&s.Unsafe, "dereferences "+types.ExprString(n.X))
		}
	case *ast.SelectorExpr:
		sel := info.Selections[n]
		if sel != nil && sel.Kind() == types.FieldVal && sel.Indirect() && !(IsPointer(info.TypeOf(n.X)) && b.pointsPrivate(n.X)) {
			b.set(&s.Reads, "reads "+types.ExprString(n)+" through a pointer")
			b.set(&s.Unsafe, "reads "+types.ExprString(n)+" through a pointer")
		}
	case *ast.IndexExpr:
		tv := info.Types[n.X]
		if tv.IsType() {
			break
		}
		switch tv.Type.Underlying().(type) {
		case *types.Signature: // instantiating a generic function
		case *types.Map:
			if !b.pointsPrivate(n.X) {
				b.set(&s.Reads, "reads the map element "+types.ExprString(n))
			}
		case *types.Array:
			if info.Types[n.Index].Value == nil {
				b.set(&s.Unsafe, "indexes "+types.ExprString(n.X))
			}
		case *types.Basic: // a string
			b.set(&s.Unsafe, "indexes "+types.ExprString(n.X))
		default:
			if !b.pointsPrivate(n.X) {
				b.set(&s.Reads, "reads the element "+types.ExprString(n))
			}
			b.set(&s.Unsafe, "indexes "+types.ExprString(n.X))
		}
	case *ast.SliceExpr:
		b.set(&s.Unsafe, "slices "+types.ExprString(n.X))
	case *ast.TypeAssertExpr:
		if n.Type != nil && !commaOK(n, b.f.Node) {
			b.set(&s.Unsafe, "asserts the type of "+types.ExprString(n.X))
		}
	case *ast.BinaryExpr:
		if (n.Op == token.QUO || n.Op == token.REM) && isInteger(info.TypeOf(n)) && info.Types[n.Y].Value == nil {
			b.set(&s.Unsafe, "divides by "+types.ExprString(n.Y))
		}
	case *ast.Ident:
		if v, ok := info.Uses[n].(*types.Var); ok && !v.IsField() && v.Pkg() != nil && v.Parent() == v.Pkg().Scope() {
			b.set(&s.Reads, "reads the package variable "+v.Name())
		}
	}
	return true
}

// call summarizes what call itself does. Unlike Effects.Call, it knows
// which variables hold memory this call made, and it walks the bodies of
// function literals passed to functions that call them only before they
// return.
func (b *body) call(call *ast.CallExpr) Summary {
	info := b.f.Info
	if bi, ok := info.Uses[IdentOf(call.Fun)].(*types.Builtin); ok && len(call.Args) > 0 {
		arg := call.Args[0]
		switch bi.Name() {
		case "append", "delete", "clear":
			if b.pointsPrivate(arg) {
				return Summary{}
			}
			return Summary{Effect: map[string]string{"append": "appends to ", "delete": "deletes from ", "clear": "clears "}[bi.Name()] + types.ExprString(arg)}
		case "copy":
			var s Summary
			if !b.pointsPrivate(arg) {
				s.Effect = "copies into " + types.ExprString(arg)
			}
			if len(call.Args) > 1 && !b.pointsPrivate(call.Args[1]) {
				s.Reads = "copies from " + types.ExprString(call.Args[1])
			}
			return s
		}
	}
	for _, lit := range SyncLits(info, call) {
		b.sync[lit] = true
	}
	return b.e.call(info, call, b.writesPrivate)
}

// commaOK reports whether the type assertion ta in fn is the value of a
// two-variable assignment or declaration (v, ok := x.(T)), which cannot
// panic.
func commaOK(ta *ast.TypeAssertExpr, fn ast.Node) bool {
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			found = found || (len(n.Lhs) == 2 && len(n.Rhs) == 1 && ast.Unparen(n.Rhs[0]) == ta)
		case *ast.ValueSpec:
			found = found || (len(n.Names) == 2 && len(n.Values) == 1 && ast.Unparen(n.Values[0]) == ta)
		}
		return !found
	})
	return found
}

func isInteger(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsInteger != 0
}

// writesPrivate reports, for Effects.call, whether a call's write goes to
// memory of this call: target in place (a local variable or part of one),
// or what target points to (a fresh local, or the address of a local).
func (b *body) writesPrivate(target ast.Expr, inPlace bool) bool {
	if inPlace {
		return b.private(target)
	}
	if u, ok := ast.Unparen(target).(*ast.UnaryExpr); ok && u.Op == token.AND {
		return b.private(u.X)
	}
	return b.pointsPrivate(target)
}
