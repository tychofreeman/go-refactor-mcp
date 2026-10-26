package analysis

import (
	"go/ast"
	"go/types"
)

// PanicKind says how a call of a known function can panic.
type PanicKind int

const (
	NoPanic    PanicKind = iota // never
	NilPanic                    // only through a nil receiver
	OtherPanic                  // also otherwise (a bad argument, say)
)

// KnownCall reports whether call calls a function known to have no side
// effects, whose result depends only on its arguments and on its
// receiver's state that never changes, and how it can panic. A call that
// spreads a slice (f(xs...)) is never known: it reads the slice.
func KnownCall(info *types.Info, call *ast.CallExpr) (known bool, panics PanicKind) {
	if call.Ellipsis.IsValid() {
		return false, OtherPanic
	}
	fn, ok := info.Uses[IdentOf(call.Fun)].(*types.Func)
	if !ok {
		return false, OtherPanic
	}
	se, ok := stdFuncs[fn.FullName()]
	if !ok || se.reads != "" || se.writes != 0 || se.fresh || se.stops {
		return false, OtherPanic
	}
	return true, se.panics
}

// ReadOnlyCall reports whether call calls a function that has no side
// effects and never panics, though its result may depend on memory its
// arguments point to, which other code can change: deleting such a call
// changes nothing, but moving it can.
func ReadOnlyCall(info *types.Info, call *ast.CallExpr) bool {
	fn, ok := info.Uses[IdentOf(call.Fun)].(*types.Func)
	se, known := stdFuncs[fn.FullName()]
	return ok && known && se.writes == 0 && se.panics == NoPanic && !se.stops
}
