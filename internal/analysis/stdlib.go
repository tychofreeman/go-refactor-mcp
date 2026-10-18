package analysis

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

// stdEffect is what a standard library function does besides computing
// its result, for functions whose bodies Effects does not read. In reads,
// %r stands for the receiver and %1, %2, ... for the arguments.
type stdEffect struct {
	reads  string    // what it reads that other code can change
	writes int       // the argument (1, 2, ...) whose memory it writes, -1 for the receiver, 0 for none
	panics PanicKind // how it can panic
}

var (
	fileSystem  = stdEffect{reads: "the file system"}
	environment = stdEffect{reads: "the environment"}
	elements    = stdEffect{reads: "the elements of %1"}
	sorts       = stdEffect{reads: "the elements of %1", writes: 1}
	appends     = stdEffect{writes: -1, panics: NilPanic}
	contents    = stdEffect{reads: "%r", panics: NilPanic}
	positions   = stdEffect{reads: "%r", panics: OtherPanic}
	getter      = stdEffect{panics: NilPanic}
)

// stdFuncs lists standard library functions by types.Func.FullName.
// The file system and the environment count as state other code can
// change: a side effect could. The go/types getters read state that is
// complete once type checking is done, and go/token line tables grow only
// while a file is parsed, so they are reads of their receiver at most.
var stdFuncs = map[string]stdEffect{
	"os.ReadFile": fileSystem, "os.Stat": fileSystem, "os.Lstat": fileSystem,
	"os.ReadDir": fileSystem, "os.Getwd": fileSystem, "path/filepath.Abs": fileSystem,
	"path/filepath.Glob": fileSystem, "path/filepath.EvalSymlinks": fileSystem,
	"os.Getenv": environment, "os.LookupEnv": environment,
	"path/filepath.Rel": {},

	"strings.Join": elements, "slices.Contains": elements, "slices.Index": elements,
	"slices.Clone": elements, "slices.Equal": {reads: "the elements of %1 and %2"},
	"sort.Strings": sorts, "sort.Ints": sorts, "sort.Float64s": sorts,
	"slices.Sort": sorts, "slices.Reverse": sorts,

	"(*strings.Builder).WriteString": appends, "(*strings.Builder).WriteByte": appends,
	"(*strings.Builder).WriteRune": appends, "(*strings.Builder).Write": appends,
	"(*strings.Builder).Reset": appends, "(*strings.Builder).Grow": {writes: -1, panics: OtherPanic},
	"(*strings.Builder).String": contents, "(*strings.Builder).Len": contents,
	"(*bytes.Buffer).WriteString": appends, "(*bytes.Buffer).WriteByte": appends,
	"(*bytes.Buffer).WriteRune": appends, "(*bytes.Buffer).Write": appends,
	"(*bytes.Buffer).Reset": appends, "(*bytes.Buffer).String": contents,
	"(*bytes.Buffer).Bytes": contents, "(*bytes.Buffer).Len": contents,

	"(*go/token.File).Offset": positions, "(*go/token.File).Line": positions,
	"(*go/token.File).Position": positions, "(*go/token.File).PositionFor": positions,
	"(*go/token.File).LineStart": positions, "(*go/token.File).Pos": positions,
	"(*go/token.File).LineCount": positions,
	"(*go/token.File).Name":      getter, "(*go/token.File).Base": getter, "(*go/token.File).Size": getter,
	"(*go/token.FileSet).Position": contents, "(*go/token.FileSet).PositionFor": contents,
	"(*go/token.FileSet).File": contents,
	"go/token.NewFileSet":      {}, "(go/token.Position).String": {}, "(go/token.Position).IsValid": {},

	"go/ast.Unparen": {reads: "%1"}, "go/ast.NewIdent": {}, "go/types.ExprString": {reads: "%1"},

	"(*go/types.object).Name": getter, "(*go/types.object).Pkg": getter,
	"(*go/types.object).Pos": getter, "(*go/types.object).Type": getter,
	"(*go/types.object).Exported": getter, "(*go/types.object).Id": getter,
	"(*go/types.object).Parent": getter, "(go/types.Object).Type": getter,
	"(go/types.Type).Underlying": getter, "(go/types.Type).String": getter,
	"(*go/types.Named).Obj": getter, "(*go/types.Named).Underlying": getter,
	"(*go/types.Named).Origin": getter, "(*go/types.Named).TypeArgs": getter,
	"(*go/types.Pointer).Elem": getter, "(*go/types.Slice).Elem": getter,
	"(*go/types.Array).Elem": getter, "(*go/types.Chan).Elem": getter,
	"(*go/types.Map).Key": getter, "(*go/types.Map).Elem": getter,
	"(*go/types.Signature).Recv": getter, "(*go/types.Signature).Params": getter,
	"(*go/types.Signature).Results": getter, "(*go/types.Signature).Variadic": getter,
	"(*go/types.Signature).TypeParams": getter,
	"(*go/types.Tuple).Len":            {}, "(*go/types.Tuple).At": {panics: OtherPanic},
	"(*go/types.Struct).NumFields": getter, "(*go/types.Struct).Field": {panics: OtherPanic},
	"(*go/types.Basic).Kind": getter, "(*go/types.Basic).Info": getter, "(*go/types.Basic).Name": getter,
	"(*go/types.Func).Signature": getter, "(*go/types.Func).FullName": getter,
	"(*go/types.Func).Origin": getter, "(*go/types.Var).Origin": getter,
	"(*go/types.TypeName).IsAlias": getter, "(*go/types.Package).Scope": getter,
	"(*go/types.Package).Name":   getter,
	"(*go/types.Selection).Kind": getter, "(*go/types.Selection).Obj": getter,
	"(*go/types.Selection).Recv": getter, "(*go/types.Selection).Indirect": getter,
	"(*go/types.Selection).Type": getter,
	"(*go/types.Info).ObjectOf":  contents, "(*go/types.Info).TypeOf": contents,
	"(*go/types.Scope).Lookup": contents,
	"go/types.IsInterface":     {}, "go/types.Identical": {}, "go/types.Unalias": {},

	"(io/fs.FileInfo).Name": getter, "(io/fs.FileInfo).IsDir": getter,
	"(io/fs.FileInfo).Size": getter, "(io/fs.FileInfo).Mode": getter,
	"(io/fs.FileInfo).ModTime": getter, "(io/fs.DirEntry).Name": getter,
	"(io/fs.DirEntry).IsDir": getter, "(io/fs.DirEntry).Type": getter,
	"(io/fs.DirEntry).Info":    {reads: "the file system", panics: NilPanic},
	"(*testing.common).Helper": getter,
}

// stdEffectOf returns what fn does, when it is a standard library
// function Effects knows; call is a call of it.
func stdEffectOf(info *types.Info, fn *types.Func, call *ast.CallExpr) (stdEffect, bool) {
	if se, ok := stdFuncs[fn.FullName()]; ok {
		return se, true
	}
	if p, ok := knownFuncs[fn.FullName()]; ok {
		return stdEffect{panics: p}, true // interface methods too (Object.Name)
	}
	if p, ok := effectFreeFuncs[fn.FullName()]; ok {
		return stdEffect{panics: p}, true
	}
	if fn.Pkg() != nil && fn.Pkg().Path() == "go/ast" && (fn.Name() == "Pos" || fn.Name() == "End") {
		return contents, true // every node's Pos and End read its fields
	}
	return stdEffect{}, false
}

// stdCall summarizes a call of a standard library function Effects
// knows. private reports whether the memory a write goes to belongs to
// the caller alone: target is written in place when inPlace (a variable,
// or the x of &x), else it points to what is written.
func stdCall(info *types.Info, fn *types.Func, call *ast.CallExpr, private func(target ast.Expr, inPlace bool) bool) (Summary, bool) {
	if s, ok := fmtCall(info, fn, call, private); ok {
		return s, true
	}
	se, ok := stdEffectOf(info, fn, call)
	if !ok {
		return Summary{}, false
	}
	isPrivate := func(target ast.Expr, inPlace bool) bool { return private != nil && private(target, inPlace) }
	// what returns the expression %r or %N names, and whether that memory
	// is the caller's own.
	what := func(ref string) (ast.Expr, bool) {
		if ref == "r" {
			x := ast.Unparen(call.Fun).(*ast.SelectorExpr).X
			inPlace := !IsPointer(info.TypeOf(x)) && !types.IsInterface(info.TypeOf(x))
			return x, isPrivate(x, inPlace)
		}
		i, _ := strconv.Atoi(ref)
		if i < 1 || i > len(call.Args) {
			return nil, false
		}
		arg := call.Args[i-1]
		if u, ok := ast.Unparen(arg).(*ast.UnaryExpr); ok && u.Op == token.AND {
			return arg, isPrivate(u.X, true)
		}
		return arg, isPrivate(arg, false)
	}
	var s Summary
	if se.reads != "" {
		text, allPrivate := se.reads, true
		for _, ref := range []string{"r", "1", "2", "3"} {
			if !strings.Contains(text, "%"+ref) {
				continue
			}
			x, p := what(ref)
			if x == nil {
				continue
			}
			allPrivate = allPrivate && p
			text = strings.ReplaceAll(text, "%"+ref, types.ExprString(x))
		}
		if !allPrivate || !strings.Contains(se.reads, "%") {
			s.Reads = "reads " + text
		}
	}
	inPlaceRecv := false
	if se.writes != 0 {
		ref := "r"
		if se.writes > 0 {
			ref = strconv.Itoa(se.writes)
		}
		if x, p := what(ref); x != nil && !p {
			s.Effect = "writes to " + types.ExprString(x)
		}
	}
	if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok && fn.Signature().Recv() != nil {
		t := info.TypeOf(sel.X)
		inPlaceRecv = !IsPointer(t) && !types.IsInterface(t)
	}
	if se.panics == OtherPanic || (se.panics == NilPanic && !inPlaceRecv) {
		s.Unsafe = "calls " + types.ExprString(call.Fun)
	}
	if call.Ellipsis.IsValid() && s.Reads == "" {
		s.Reads = "spreads " + types.ExprString(call.Args[len(call.Args)-1])
	}
	return s, true
}

// fmtCall summarizes a call of fmt's Sprint and Fprint functions and
// Errorf: no side effect unless it calls a method of an argument
// (String, Error, Format, GoString, or any method of an interface value)
// or, for Fprint, writes to a writer that is not the caller's own.
func fmtCall(info *types.Info, fn *types.Func, call *ast.CallExpr, private func(target ast.Expr, inPlace bool) bool) (Summary, bool) {
	if fn.Pkg() == nil || fn.Pkg().Path() != "fmt" || fn.Signature().Recv() != nil {
		return Summary{}, false
	}
	args := call.Args
	var s Summary
	switch fn.Name() {
	case "Sprint", "Sprintf", "Sprintln", "Errorf":
	case "Fprint", "Fprintf", "Fprintln":
		if len(args) == 0 {
			return Summary{}, false
		}
		w := args[0]
		u, ok := ast.Unparen(w).(*ast.UnaryExpr)
		if !ok || u.Op != token.AND || private == nil || !private(u.X, true) || !isBuilder(info.TypeOf(u.X)) {
			s.Effect = "writes to " + types.ExprString(w)
		}
		args = args[1:]
	default:
		return Summary{}, false
	}
	if call.Ellipsis.IsValid() {
		s.Effect = "formats the elements of " + types.ExprString(args[len(args)-1]) + ", which could have methods"
		return s, true
	}
	for _, a := range args {
		t := info.TypeOf(a)
		switch {
		case t == nil:
		case types.IsInterface(t) || hasFormatMethod(t):
			if s.Effect == "" {
				s.Effect = "can call a method of " + types.ExprString(a)
			}
		case !isBasic(t) && s.Reads == "":
			s.Reads = "formats what " + types.ExprString(a) + " holds"
		}
	}
	return s, true
}

// hasFormatMethod reports whether fmt would call a method of a value of
// type t: String, Error, Format or GoString.
func hasFormatMethod(t types.Type) bool {
	for _, mt := range []types.Type{t, types.NewPointer(t)} {
		ms := types.NewMethodSet(mt)
		for i := 0; i < ms.Len(); i++ {
			switch ms.At(i).Obj().Name() {
			case "String", "Error", "Format", "GoString":
				return true
			}
		}
	}
	return false
}

func isBasic(t types.Type) bool {
	_, ok := t.Underlying().(*types.Basic)
	return ok
}

// isBuilder reports whether t is strings.Builder or bytes.Buffer, whose
// Write methods only append to the value.
func isBuilder(t types.Type) bool {
	n, ok := types.Unalias(t).(*types.Named)
	if !ok || n.Obj().Pkg() == nil {
		return false
	}
	name := n.Obj().Pkg().Path() + "." + n.Obj().Name()
	return name == "strings.Builder" || name == "bytes.Buffer"
}
