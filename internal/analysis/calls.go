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

// knownFuncs lists functions (by types.Func.FullName) that have no side
// effects and whose result depends only on their arguments, which are
// values (strings, numbers, runes), and on their receiver's state that
// never changes, with how each can panic.
//
// Left out on purpose: functions that return a newly allocated error
// (strconv.Atoi: two calls give two errors that are not ==), that call
// methods of their arguments (fmt), that read memory other code can change
// (bytes, strings.Join, anything taking a slice or map), and the methods
// of go/ast nodes, whose fields can be assigned.
var knownFuncs = map[string]PanicKind{
	// strings, on strings.
	"strings.Compare": NoPanic, "strings.Contains": NoPanic, "strings.ContainsAny": NoPanic,
	"strings.ContainsRune": NoPanic, "strings.Count": NoPanic, "strings.Cut": NoPanic,
	"strings.CutPrefix": NoPanic, "strings.CutSuffix": NoPanic, "strings.EqualFold": NoPanic,
	"strings.Fields": NoPanic, "strings.HasPrefix": NoPanic, "strings.HasSuffix": NoPanic,
	"strings.Index": NoPanic, "strings.IndexAny": NoPanic, "strings.IndexByte": NoPanic,
	"strings.IndexRune": NoPanic, "strings.LastIndex": NoPanic, "strings.LastIndexAny": NoPanic,
	"strings.LastIndexByte": NoPanic, "strings.Replace": NoPanic, "strings.ReplaceAll": NoPanic,
	"strings.Split": NoPanic, "strings.SplitAfter": NoPanic, "strings.SplitAfterN": NoPanic,
	"strings.SplitN": NoPanic, "strings.ToLower": NoPanic, "strings.ToTitle": NoPanic,
	"strings.ToUpper": NoPanic, "strings.Trim": NoPanic, "strings.TrimLeft": NoPanic,
	"strings.TrimPrefix": NoPanic, "strings.TrimRight": NoPanic, "strings.TrimSpace": NoPanic,
	"strings.TrimSuffix": NoPanic,
	"strings.Repeat":     OtherPanic, // a negative count

	// unicode and unicode/utf8.
	"unicode.IsControl": NoPanic, "unicode.IsDigit": NoPanic, "unicode.IsGraphic": NoPanic,
	"unicode.IsLetter": NoPanic, "unicode.IsLower": NoPanic, "unicode.IsNumber": NoPanic,
	"unicode.IsPrint": NoPanic, "unicode.IsPunct": NoPanic, "unicode.IsSpace": NoPanic,
	"unicode.IsUpper": NoPanic, "unicode.ToLower": NoPanic, "unicode.ToTitle": NoPanic,
	"unicode.ToUpper":                     NoPanic,
	"unicode/utf8.DecodeLastRuneInString": NoPanic, "unicode/utf8.DecodeRuneInString": NoPanic,
	"unicode/utf8.RuneCountInString": NoPanic, "unicode/utf8.RuneLen": NoPanic,
	"unicode/utf8.ValidRune": NoPanic, "unicode/utf8.ValidString": NoPanic,

	// strconv, formatting only.
	"strconv.FormatBool": NoPanic, "strconv.FormatFloat": NoPanic, "strconv.Itoa": NoPanic,
	"strconv.Quote": NoPanic, "strconv.QuoteRune": NoPanic, "strconv.QuoteToASCII": NoPanic,
	"strconv.FormatInt": OtherPanic, "strconv.FormatUint": OtherPanic, // a bad base

	// math.
	"math.Abs": NoPanic, "math.Ceil": NoPanic, "math.Exp": NoPanic, "math.Floor": NoPanic,
	"math.Inf": NoPanic, "math.IsInf": NoPanic, "math.IsNaN": NoPanic, "math.Log": NoPanic,
	"math.Max": NoPanic, "math.Min": NoPanic, "math.Mod": NoPanic, "math.NaN": NoPanic,
	"math.Pow": NoPanic, "math.Round": NoPanic, "math.Signbit": NoPanic, "math.Sqrt": NoPanic,
	"math.Trunc": NoPanic,

	// path and path/filepath, lexical only (not Abs, which reads the
	// working directory, nor Rel, which returns new errors).
	"path.Base": NoPanic, "path.Clean": NoPanic, "path.Dir": NoPanic, "path.Ext": NoPanic,
	"path.IsAbs": NoPanic, "path.Join": NoPanic,
	"path/filepath.Base": NoPanic, "path/filepath.Clean": NoPanic, "path/filepath.Dir": NoPanic,
	"path/filepath.Ext": NoPanic, "path/filepath.FromSlash": NoPanic, "path/filepath.IsAbs": NoPanic,
	"path/filepath.Join": NoPanic, "path/filepath.ToSlash": NoPanic,

	// go/token.
	"go/token.IsExported": NoPanic, "go/token.IsIdentifier": NoPanic, "go/token.IsKeyword": NoPanic,
	"(go/token.Pos).IsValid": NoPanic, "(go/token.Token).IsKeyword": NoPanic,
	"(go/token.Token).IsLiteral": NoPanic, "(go/token.Token).IsOperator": NoPanic,
	"(go/token.Token).Precedence": NoPanic, "(go/token.Token).String": NoPanic,

	// reflect: a value's dynamic type, which never changes.
	"reflect.TypeOf": NoPanic,

	// go/types: getters of state that never changes once an object exists.
	"(go/types.Object).Exported": NilPanic, "(go/types.Object).Id": NilPanic,
	"(go/types.Object).Name": NilPanic, "(go/types.Object).Pkg": NilPanic,
	"(go/types.Object).Pos": NilPanic, "(*go/types.Package).Path": NilPanic,
	"(*go/types.Var).Anonymous": NilPanic, "(*go/types.Var).Embedded": NilPanic,
	"(*go/types.Var).IsField": NilPanic,
}

// readOnlyFuncs lists functions (by types.Func.FullName) that have no side
// effects and never panic but read memory their arguments point to (a
// slice or what an interface holds), so that two calls can give different
// results: not stable, as knownFuncs are, but safe to delete.
var readOnlyFuncs = map[string]bool{
	"bytes.Compare": true, "bytes.Contains": true, "bytes.Equal": true,
	"bytes.HasPrefix": true, "bytes.HasSuffix": true, "bytes.Index": true,
	"encoding/json.Valid": true, "reflect.DeepEqual": true,
}

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
	p, ok := knownFuncs[fn.FullName()]
	if !ok {
		return false, OtherPanic
	}
	return true, p
}

// ReadOnlyCall reports whether call calls a function that has no side
// effects and never panics, though its result may depend on memory its
// arguments point to, which other code can change: deleting such a call
// changes nothing, but moving it can.
func ReadOnlyCall(info *types.Info, call *ast.CallExpr) bool {
	fn, ok := info.Uses[IdentOf(call.Fun)].(*types.Func)
	return ok && readOnlyFuncs[fn.FullName()]
}
