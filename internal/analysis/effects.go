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
	Stops  string // of Unsafe, something that might not finish or return: a loop, a channel operation, os.Exit, unknown code
	Makes  string // a new reference value: a pointer, slice, map, channel or function value

	At     Where     // where each of the above was found
	Panics PanicKind // how it can panic: NilPanic when only by dereferencing nil
}

// Where says where, in the code summarized, each finding of a Summary
// is: the node that does it, or the call through which it happens.
type Where struct {
	Effect, Reads, Unsafe, Stops, Makes ast.Node
	Panic                               ast.Node // the first that can panic
	OtherPanic                          ast.Node // the first that can panic other than by dereferencing nil
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
	return e.call(info, call, owned{})
}

// Of summarizes what evaluating x, an expression in the body of f, can
// do, as if it were evaluated anywhere in f: memory f made itself is not
// its own, since the rest of f can change it, and neither are the locals
// a pointer or closure can change. x is taken out of its context: a type
// assertion x itself is not one with a comma, ok.
func (e *Effects) Of(f *Func, x ast.Expr) Summary {
	b := e.walker(f, x)
	ast.Inspect(x, b.visit)
	return b.s
}

// walker returns a body that summarizes code of f evaluated out of its
// context (root, for the part summarized): only memory nothing else in f
// reaches is its own, since the rest of f can change the rest.
func (e *Effects) walker(f *Func, root ast.Expr) *body {
	return &body{e: e, owned: owned{f: f, own: ThisCode}, sync: map[*ast.FuncLit]bool{}, callees: map[ast.Expr]bool{}, root: root, addressed: map[ast.Expr]ast.Expr{}}
}

// StableAddress is Stable for the address of x, an expression in the
// body of f, rather than its value: it depends only on operands that are
// stable.
func (e *Effects) StableAddress(f *Func, x ast.Expr) (ast.Node, bool) {
	ops, culprit := f.AddressOperands(x)
	if culprit != nil {
		return culprit, false
	}
	for _, op := range ops {
		if c, ok := e.Of(f, op).Stable(false); !ok {
			return c, false
		}
	}
	return nil, true
}

// Acts reports whether n itself, apart from its operands, can do more
// than compute a value: a call with side effects, that reads what other
// code can change or that could panic or not finish, or a receive.
func (e *Effects) Acts(info *types.Info, n ast.Node) bool {
	switch n := n.(type) {
	case *ast.CallExpr:
		s := e.Call(info, n)
		return s.Effect != "" || s.Reads != "" || s.Unsafe != ""
	case *ast.UnaryExpr:
		return n.Op == token.ARROW
	}
	return false
}

// FirstActBefore returns the first node inside root that Acts and is
// evaluated before pos (it ends before pos and does not enclose it),
// ignoring the bodies of function literals and the node skip.
func (e *Effects) FirstActBefore(info *types.Info, root ast.Node, pos token.Pos, skip ast.Node) ast.Node {
	var first ast.Node
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil || first != nil || n.Pos() >= pos || (skip != nil && n == skip) {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false // defining a closure runs nothing
		}
		if n.End() <= pos && e.Acts(info, n) {
			first = n
			return false
		}
		return true
	})
	return first
}

func (e *Effects) call(info *types.Info, call *ast.CallExpr, o owned) Summary {
	name := types.ExprString(call.Fun)
	if tv, ok := info.Types[call.Fun]; ok && tv.IsType() {
		return Summary{} // a conversion
	}
	if b, ok := info.Uses[IdentOf(call.Fun)].(*types.Builtin); ok {
		return builtinCall(info, call, b.Name())
	}
	if fn, ok := info.Uses[IdentOf(call.Fun)].(*types.Func); ok {
		if s, ok := stdCall(info, fn, call, o); ok {
			return s
		}
		if s, ok := syncCallSummary(info, fn, call, o); ok {
			return s
		}
	}
	fn := staticCallee(info, call)
	if fn == nil {
		return Summary{Effect: "calls " + name, Unsafe: "calls " + name + ", which could be any function", Stops: "calls " + name + ", which could be any function", Panics: OtherPanic}
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
	s.add(Summary{Effect: which(fs.Effect), Reads: which(fs.Reads), Unsafe: which(fs.Unsafe), Stops: which(fs.Stops), Makes: which(fs.Makes), Panics: fs.Panics})
	return s
}

// add fills in the fields of s that are still empty from o, with where
// o found them, and keeps the worse of the two ways to panic.
func (s *Summary) add(o Summary) {
	if s.Effect == "" {
		s.Effect, s.At.Effect = o.Effect, o.At.Effect
	}
	if s.Reads == "" {
		s.Reads, s.At.Reads = o.Reads, o.At.Reads
	}
	if s.Unsafe == "" {
		s.Unsafe, s.At.Unsafe = o.Unsafe, o.At.Unsafe
	}
	if s.Stops == "" {
		s.Stops, s.At.Stops = o.Stops, o.At.Stops
	}
	if s.Makes == "" {
		s.Makes, s.At.Makes = o.Makes, o.At.Makes
	}
	if s.At.Panic == nil {
		s.At.Panic = o.At.Panic
	}
	if s.At.OtherPanic == nil {
		s.At.OtherPanic = o.At.OtherPanic
	}
	s.Panics = max(s.Panics, o.Panics)
}

// at returns s with n as where each of its findings is: for a call, the
// call, through which the callee's findings happen.
func (s Summary) at(n ast.Node) Summary {
	where := func(what string) ast.Node {
		if what == "" {
			return nil
		}
		return n
	}
	s.At = Where{Effect: where(s.Effect), Reads: where(s.Reads), Unsafe: where(s.Unsafe), Stops: where(s.Stops), Makes: where(s.Makes)}
	if s.Panics != NoPanic {
		s.At.Panic = n
	}
	if s.Panics == OtherPanic {
		s.At.OtherPanic = n
	}
	return s
}

// Stable reports whether the value s summarizes depends only on
// constants, functions and local variables no other code can reach, so
// that evaluating it again, or elsewhere in the function, gives the same
// value: it has no side effects, reads nothing other code can change and
// finishes. With sharing it must also make no new reference value, so
// that one value can stand for evaluating it each time. It can still
// panic (At.OtherPanic and Panics say how). It returns the first finding in
// the way.
func (s Summary) Stable(sharing bool) (ast.Node, bool) {
	var first ast.Node
	note := func(what string, at ast.Node) {
		if what != "" && (first == nil || at != nil && at.Pos() < first.Pos()) {
			first = at
		}
	}
	note(s.Effect, s.At.Effect)
	note(s.Reads, s.At.Reads)
	note(s.Stops, s.At.Stops)
	if sharing {
		note(s.Makes, s.At.Makes)
	}
	return first, s.Effect == "" && s.Reads == "" && s.Stops == "" && (!sharing || s.Makes == "")
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
	// The TypeScript compiler's walk, which this module's tsapi uses. A
	// method: CallsBeforeReturning consults it, a summary of the call
	// does not (see SyncLits).
	"(*github.com/buke/typescript-go-internal/v7/pkg/ast.Node).ForEachChild": reads,
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
func syncCallSummary(info *types.Info, fn *types.Func, call *ast.CallExpr, o owned) (Summary, bool) {
	if fn.Pkg() == nil || fn.Signature().Recv() != nil {
		return Summary{}, false
	}
	sc, ok := syncCalls[fn.Pkg().Path()+"."+fn.Name()]
	if !ok {
		return Summary{}, false
	}
	s := Summary{Effect: sc.effect, Unsafe: sc.unsafe}
	if sc.unsafe != "" {
		s.Panics = OtherPanic
	}
	for i, a := range call.Args {
		_, isLit := ast.Unparen(a).(*ast.FuncLit)
		_, isFunc := info.TypeOf(a).Underlying().(*types.Signature)
		switch {
		case isLit:
		case isFunc:
			s.Effect = "passes " + types.ExprString(a) + " to " + types.ExprString(call.Fun)
			s.Stops = s.Effect + ", which could be any function"
			s.Panics = OtherPanic
		case (i == 0 || sc.readsAll) && !o.writes(a, false):
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
					return Summary{Unsafe: "makes " + types.ExprString(call.Args[0]) + " of a size that may be out of range", Panics: OtherPanic}
				}
			}
		}
		return Summary{}
	case "len", "cap", "min", "max", "real", "imag", "complex", "new":
		return Summary{}
	case "panic":
		return Summary{Unsafe: "panics", Panics: OtherPanic}
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

// freshLocals returns the local variables of the function f, whose body
// is body, that only ever hold memory the function made itself (make,
// new, a composite literal or its address, nil, an append to the
// variable or a slice of it) and whose address is not taken: writing
// through them is not seen outside the call.
func freshLocals(f *Func, body *ast.BlockStmt) map[*types.Var]bool {
	info := f.Info
	fresh, bad := map[*types.Var]bool{}, map[*types.Var]bool{}
	note := func(v *types.Var, rhs ast.Expr, ok bool) {
		switch {
		case v == nil || !f.IsLocal(v):
		case !ok || f.Addressed[v] || v.Pos() < body.Pos() || (rhs != nil && !isFreshValue(info, v, rhs)):
			bad[v] = true
		default:
			fresh[v] = true
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
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

// owned is the memory a summary counts as its code's own: what f's
// Owner ranks at least own. The zero owned has none, for a call
// summarized outside any body.
type owned struct {
	f           *Func
	own         Owner      // whose memory counts as the summarized code's own: ThisFunc for a whole function, ThisCode for part of one out of its context
	recv, cache *types.Var // for a getter that makes its result on first use, its receiver and the field it caches it in (cacheOf)
}

// body walks one function body for summarize.
type body struct {
	e *Effects
	owned
	sync map[*ast.FuncLit]bool // literals whose bodies run during the call
	s    Summary

	callees   map[ast.Expr]bool     // the functions of the calls seen
	root      ast.Expr              // for Of, the expression summarized, evaluated out of its context
	addressed map[ast.Expr]ast.Expr // operands whose address is taken: reads of the pointer they go through only (addressedIn)
}

// summarize computes what calling the function declared by src can do,
// from its body: what its own code does, and what the functions it calls
// do. Writes to its own local variables, and through those that hold
// memory it made itself, are not side effects.
func (e *Effects) summarize(src funcSource) *Summary {
	f := AnalyzeFunc(src.info, src.decl)
	b := &body{e: e, owned: owned{f: f, own: ThisFunc}, sync: map[*ast.FuncLit]bool{}, callees: map[ast.Expr]bool{}, addressed: addressedIn(src.info, src.decl.Body)}
	var fill *ast.BlockStmt
	b.recv, b.cache, fill = cacheOf(src.info, src.decl)
	ast.Inspect(src.decl.Body, b.visit)
	if m := b.s.At.Makes; fill != nil && m != nil && fill.Pos() <= m.Pos() && m.End() <= fill.End() {
		b.s.Makes, b.s.At.Makes = "", nil // later calls return what the first made
	}
	return &b.s
}

// cacheOf returns the receiver of decl and the field it caches its result
// in, and the block that makes the result, when decl is a getter that
// makes it on first use: its body is if r.f == nil { ... } then return
// r.f. Its writes to r.f, and to what r.f holds, only fill the cache:
// whenever the block runs, later calls return what it made, so they
// count as the code's own, and r.f is read.
func cacheOf(info *types.Info, decl *ast.FuncDecl) (recv, field *types.Var, fill *ast.BlockStmt) {
	if decl.Recv == nil || len(decl.Recv.List) != 1 || len(decl.Recv.List[0].Names) != 1 || decl.Body == nil || len(decl.Body.List) != 2 {
		return nil, nil, nil
	}
	recv, _ = info.Defs[decl.Recv.List[0].Names[0]].(*types.Var)
	is := func(x ast.Expr) *types.Var {
		sel, ok := ast.Unparen(x).(*ast.SelectorExpr)
		if !ok {
			return nil
		}
		if id, ok := ast.Unparen(sel.X).(*ast.Ident); !ok || recv == nil || info.Uses[id] != recv {
			return nil
		}
		v, _ := info.Uses[sel.Sel].(*types.Var)
		return v
	}
	ifs, ok := decl.Body.List[0].(*ast.IfStmt)
	ret, ok2 := decl.Body.List[1].(*ast.ReturnStmt)
	if !ok || !ok2 || ifs.Init != nil || ifs.Else != nil || len(ret.Results) != 1 {
		return nil, nil, nil
	}
	cond, ok := ast.Unparen(ifs.Cond).(*ast.BinaryExpr)
	if !ok || cond.Op != token.EQL || !info.Types[cond.Y].IsNil() {
		return nil, nil, nil
	}
	if field = is(cond.X); field == nil || !field.IsField() || is(ret.Results[0]) != field {
		return nil, nil, nil
	}
	return recv, field, ifs.Body
}

// caches reports whether x is the cache field of a getter that makes its
// result on first use (see cacheOf), or part of what it holds.
func (o owned) caches(x ast.Expr) bool {
	if o.cache == nil {
		return false
	}
	for {
		switch e := ast.Unparen(x).(type) {
		case *ast.SelectorExpr:
			if id, ok := ast.Unparen(e.X).(*ast.Ident); ok && o.f.Info.Uses[id] == o.recv && o.f.Info.Uses[e.Sel] == o.cache {
				return true
			}
			x = e.X
		case *ast.IndexExpr:
			x = e.X
		case *ast.StarExpr:
			x = e.X
		default:
			return false
		}
	}
}

// pointsPrivate reports whether the memory x (a pointer, slice or map)
// points to belongs to the summarized code.
func (o owned) pointsPrivate(x ast.Expr) bool {
	return o.f != nil && (o.f.Pointee(x) >= o.own || o.caches(x))
}

// private reports whether writing to the assignable expression x changes
// only memory the summarized code owns: a local variable or part of one,
// or memory a fresh local points to.
func (o owned) private(x ast.Expr) bool { return o.f != nil && (o.f.Owner(x) >= o.own || o.caches(x)) }

// effect, read, unsafe and stops record what n does, where it is the
// first of its kind: a side effect, a read of what other code can change,
// something that can panic (how, kind says), and something that might
// not finish (and can also panic as kind says).
func (b *body) effect(n ast.Node, what string) { b.s.add(Summary{Effect: what}.at(n)) }

func (b *body) read(n ast.Node, what string) { b.s.add(Summary{Reads: what}.at(n)) }

func (b *body) unsafe(n ast.Node, kind PanicKind, what string) {
	b.s.add(Summary{Unsafe: what, Panics: kind}.at(n))
}

func (b *body) stops(n ast.Node, kind PanicKind, what string) {
	b.s.add(Summary{Unsafe: what, Stops: what, Panics: kind}.at(n))
}

func (b *body) makes(n ast.Node, what string) { b.s.add(Summary{Makes: what}.at(n)) }

// assigns records the write to x by the statement at, when other code
// could see it.
func (b *body) assigns(x ast.Expr, at ast.Node) {
	if id, ok := x.(*ast.Ident); ok && id.Name == "_" {
		return
	}
	if !b.private(x) {
		b.effect(at, "assigns to "+types.ExprString(x))
	}
}

// visit records what node n itself does; ast.Inspect walks its children.
func (b *body) visit(n ast.Node) bool {
	info := b.f.Info
	s := &b.s
	switch n := n.(type) {
	case *ast.FuncLit:
		b.makes(n, "makes a function value")
		return b.sync[n] // defining a closure runs nothing
	case *ast.GoStmt:
		b.effect(n, "starts a goroutine")
	case *ast.DeferStmt:
		b.effect(n, "defers a call")
	case *ast.SendStmt:
		b.effect(n, "sends on a channel")
		b.stops(n, OtherPanic, "sends on a channel, which might block")
	case *ast.SelectStmt:
		b.effect(n, "selects on channels")
		b.stops(n, OtherPanic, "selects on channels, which might block")
	case *ast.UnaryExpr:
		switch n.Op {
		case token.ARROW:
			b.effect(n, "receives from a channel")
			b.stops(n, NoPanic, "receives from a channel, which might block")
		case token.AND:
			for x, p := range addressedIn(info, n) {
				b.addressed[x] = p
			}
			if _, lit := ast.Unparen(n.X).(*ast.CompositeLit); lit { // &v is the same pointer each time
				b.makes(n, "allocates "+types.ExprString(n.X))
			}
		}
	case *ast.ForStmt:
		b.stops(n, NoPanic, "has a loop that might not finish")
	case *ast.RangeStmt:
		switch info.TypeOf(n.X).Underlying().(type) {
		case *types.Chan:
			b.effect(n, "receives from a channel")
			b.stops(n, NoPanic, "receives from a channel, which might block")
		case *types.Signature:
			b.effect(n, "ranges over a function")
			b.stops(n, OtherPanic, "ranges over a function, which could be any function")
		case *types.Slice, *types.Map, *types.Pointer:
			if !b.pointsPrivate(n.X) {
				b.read(n, "reads the elements of "+types.ExprString(n.X))
			}
		}
		if n.Tok == token.ASSIGN {
			for _, x := range []ast.Expr{n.Key, n.Value} {
				if x != nil {
					b.assigns(x, n)
				}
			}
		}
	case *ast.AssignStmt:
		for _, l := range n.Lhs {
			if id, ok := l.(*ast.Ident); ok && n.Tok == token.DEFINE && Defines(info, id) {
				continue
			}
			b.assigns(l, n)
		}
		if (n.Tok == token.QUO_ASSIGN || n.Tok == token.REM_ASSIGN) && !isFloatOrComplex(info.TypeOf(n.Lhs[0])) && info.Types[n.Rhs[0]].Value == nil {
			b.unsafe(n, OtherPanic, "divides by "+types.ExprString(n.Rhs[0]))
		}
	case *ast.IncDecStmt:
		b.assigns(n.X, n)
	case *ast.CallExpr:
		b.callees[ast.Unparen(n.Fun)] = true
		if tv := info.Types[n.Fun]; tv.IsType() && len(n.Args) == 1 && toArray(tv.Type, info.TypeOf(n.Args[0])) {
			b.unsafe(n, OtherPanic, "converts "+types.ExprString(n.Args[0])+" to an array")
		}
		if IsReference(info.TypeOf(n)) {
			b.makes(n, "makes a new "+info.TypeOf(n).String())
		}
		if bi, ok := info.Uses[IdentOf(n.Fun)].(*types.Builtin); ok && (bi.Name() == "len" || bi.Name() == "cap") && len(n.Args) == 1 && IsMapOrChan(info.TypeOf(n.Args[0])) && !b.pointsPrivate(n.Args[0]) {
			b.read(n, "reads the size of "+types.ExprString(n.Args[0]))
		}
		s.add(b.call(n).at(n))
	case *ast.StarExpr:
		if !info.Types[n].IsType() && !b.pointsPrivate(n.X) {
			b.read(n, "reads through the pointer "+types.ExprString(n.X))
			b.unsafe(n, NilPanic, "dereferences "+types.ExprString(n.X))
		}
	case *ast.SelectorExpr:
		sel := info.Selections[n]
		if sel == nil {
			break
		}
		if p, ok := b.addressed[n]; ok { // &x.f reads only the pointer it goes through
			if p != nil && !b.pointsPrivate(p) {
				b.unsafe(n, NilPanic, "dereferences "+types.ExprString(p))
			}
			break
		}
		if sel.Indirect() && !(IsPointer(info.TypeOf(n.X)) && b.pointsPrivate(n.X)) && (sel.Kind() == types.FieldVal || valueReceiver(sel)) {
			b.read(n, "reads "+types.ExprString(n)+" through a pointer")
			b.unsafe(n, NilPanic, "reads "+types.ExprString(n)+" through a pointer")
		}
		if sel.Kind() == types.MethodVal && !b.callees[n] {
			b.makes(n, "makes the method value "+types.ExprString(n))
		}
	case *ast.IndexExpr:
		tv := info.Types[n.X]
		if tv.IsType() {
			break
		}
		switch tv.Type.Underlying().(type) {
		case *types.Signature: // instantiating a generic function
		case *types.Map:
			if _, addressed := b.addressed[n]; !addressed && !b.pointsPrivate(n.X) {
				b.read(n, "reads the map element "+types.ExprString(n))
			}
		case *types.Array:
			if info.Types[n.Index].Value == nil {
				b.unsafe(n, OtherPanic, "indexes "+types.ExprString(n.X))
			}
		case *types.Basic: // a string
			if info.Types[n.X].Value == nil || info.Types[n.Index].Value == nil {
				b.unsafe(n, OtherPanic, "indexes "+types.ExprString(n.X))
			}
		default:
			if _, addressed := b.addressed[n]; !addressed && !b.pointsPrivate(n.X) {
				b.read(n, "reads the element "+types.ExprString(n))
			}
			b.unsafe(n, OtherPanic, "indexes "+types.ExprString(n.X))
		}
	case *ast.SliceExpr:
		switch {
		case n.Low != nil || n.High != nil || n.Max != nil:
			b.unsafe(n, OtherPanic, "slices "+types.ExprString(n.X))
		case IsPointer(info.TypeOf(n.X)):
			b.unsafe(n, NilPanic, "slices "+types.ExprString(n.X)+", a pointer")
		}
		if IsArray(info.TypeOf(n.X)) || IsPointer(info.TypeOf(n.X)) {
			b.makes(n, "makes a slice of "+types.ExprString(n.X))
		}
	case *ast.TypeAssertExpr:
		if n.Type != nil && (n == b.root || b.f.Node == nil || !commaOK(n, b.f.Node)) {
			b.unsafe(n, OtherPanic, "asserts the type of "+types.ExprString(n.X))
		}
	case *ast.BinaryExpr:
		switch {
		case (n.Op == token.QUO || n.Op == token.REM) && !isFloatOrComplex(info.TypeOf(n)) && info.Types[n.Y].Value == nil:
			b.unsafe(n, OtherPanic, "divides by "+types.ExprString(n.Y))
		case (n.Op == token.SHL || n.Op == token.SHR) && !isUnsigned(info.TypeOf(n.Y)) && info.Types[n.Y].Value == nil:
			b.unsafe(n, OtherPanic, "shifts by "+types.ExprString(n.Y))
		}
	case *ast.CompositeLit:
		if t := info.TypeOf(n); !IsStruct(t) && !IsArray(t) {
			b.makes(n, "makes a new "+t.String())
		}
	case *ast.Ident:
		v, ok := info.Uses[n].(*types.Var)
		switch {
		case !ok:
		case !v.IsField() && v.Pkg() != nil && v.Parent() == v.Pkg().Scope():
			b.read(n, "reads the package variable "+v.Name())
		case b.root != nil && b.f.Owns(v) == ThisFunc:
			b.read(n, "reads "+v.Name()+", which a pointer or closure can change")
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
	return b.e.call(info, call, b.owned)
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

// toArray reports whether converting a value of type from to type to
// makes an array of a slice, or a pointer to one: a conversion that
// panics when the slice is too short.
func toArray(to, from types.Type) bool {
	if !IsSlice(from) {
		return false
	}
	switch u := to.Underlying().(type) {
	case *types.Array:
		return true
	case *types.Pointer:
		_, ok := u.Elem().Underlying().(*types.Array)
		return ok
	}
	return false
}

// valueReceiver reports whether sel selects a method with a value
// receiver, which copies what it is called on.
func valueReceiver(sel *types.Selection) bool {
	if sel.Kind() != types.MethodVal {
		return false
	}
	recv := sel.Obj().(*types.Func).Signature().Recv()
	return recv != nil && !IsPointer(recv.Type())
}

func isInteger(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsInteger != 0
}

// writes reports whether a call's write goes to memory of its own:
// target in place (a local variable or part of one), or what target
// points to (a fresh local, or the address of a local).
func (o owned) writes(target ast.Expr, inPlace bool) bool {
	if inPlace {
		return o.private(target)
	}
	if u, ok := ast.Unparen(target).(*ast.UnaryExpr); ok && u.Op == token.AND {
		return o.private(u.X)
	}
	return o.pointsPrivate(target)
}
