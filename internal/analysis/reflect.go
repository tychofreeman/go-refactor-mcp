package analysis

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/tools/go/packages"
)

// swapsOnly holds the functions that take a slice as an interface and
// whose reflection only measures it and swaps its elements, never
// looking at what they hold.
var swapsOnly = map[string]bool{
	"sort.Slice":         true,
	"sort.SliceStable":   true,
	"sort.SliceIsSorted": true,
	"reflect.Swapper":    true,
}

// PassedAsInterface returns the first argument in files that is passed to
// an interface parameter, or converted to an interface type, and whose
// type holds a type match accepts, or nil. Reflection there (encoding/json,
// text/template, fmt's %+v) sees what the value holds: its field names
// among others. Arguments of sort.Slice and the like, which only swap
// elements, and assignments to interface variables and returns are not
// looked at.
func PassedAsInterface(info *types.Info, files []*ast.File, match func(types.Type) bool) ast.Expr {
	var found ast.Expr
	passedAsInterface(info, files, match, func(_ *ast.CallExpr, _ int, arg ast.Expr) bool {
		found = arg
		return false
	})
	return found
}

// Sight is what reflection in a callee sees of a value passed to it, from
// least to most.
type Sight int

const (
	// Blind: the callee sees nothing of its fields (fmt's %T, a value that
	// prints itself).
	Blind Sight = iota
	// Prints: the callee prints the fields' values, not their names (fmt's
	// %v, fmt.Println).
	Prints
	// PrintsNames: the callee prints the fields' names and values (fmt's
	// %+v and %#v) and does nothing else with them.
	PrintsNames
	// Compares: the callee compares the fields' values (reflect.DeepEqual).
	Compares
	// SeesNames: the callee may find fields by name (encoding/json,
	// text/template), or is not known.
	SeesNames
)

// Reflected is an argument passed as an interface whose type holds a type
// a match accepts, and what the callee sees of it.
type Reflected struct {
	Arg    ast.Expr
	Type   types.Type // the argument's type
	Callee string     // the call's function, or the type converted to, as written
	Conv   bool       // a conversion, not a call
	Sight  Sight
}

// Passed says how the argument reaches its callee: "passed as an interface
// to json.Marshal", or "converted to any".
func (r Reflected) Passed() string {
	if r.Conv {
		return "converted to " + r.Callee
	}
	return "passed as an interface to " + r.Callee
}

// ReflectedArgs returns every argument PassedAsInterface would find, with
// what the callee sees of it.
func ReflectedArgs(info *types.Info, files []*ast.File, match func(types.Type) bool) []Reflected {
	var refs []Reflected
	passedAsInterface(info, files, match, func(call *ast.CallExpr, i int, arg ast.Expr) bool {
		refs = append(refs, Reflected{Arg: arg, Type: info.TypeOf(arg), Callee: types.ExprString(call.Fun), Conv: info.Types[call.Fun].IsType(), Sight: sight(info, call, i)})
		return true
	})
	return refs
}

// ReflectedIn returns ReflectedArgs over pkgs, once for an argument that
// package variants (a package and its test variant) share.
func ReflectedIn(pkgs []*packages.Package, fset *token.FileSet, match func(types.Type) bool) []Reflected {
	seen := map[token.Position]bool{}
	var refs []Reflected
	for _, p := range pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, ref := range ReflectedArgs(p.TypesInfo, p.Syntax, match) {
			if pos := fset.Position(ref.Arg.Pos()); !seen[pos] {
				seen[pos] = true
				refs = append(refs, ref)
			}
		}
	}
	return refs
}

// passedAsInterface calls visit with each argument PassedAsInterface
// looks for, its call and its index, until visit returns false.
func passedAsInterface(info *types.Info, files []*ast.File, match func(types.Type) bool, visit func(call *ast.CallExpr, i int, arg ast.Expr) bool) {
	stopped := false
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || stopped {
				return !stopped
			}
			if fn := staticCallee(info, call); fn != nil && swapsOnly[fn.FullName()] {
				return true
			}
			tv := info.Types[call.Fun]
			var param func(i int) types.Type
			switch {
			case tv.IsType():
				param = func(int) types.Type { return tv.Type }
			case tv.IsBuiltin():
				return true
			default:
				sig, ok := tv.Type.Underlying().(*types.Signature)
				if !ok {
					return true
				}
				param = func(i int) types.Type {
					ps := sig.Params()
					if sig.Variadic() && i >= ps.Len()-1 {
						last := ps.At(ps.Len() - 1).Type()
						if call.Ellipsis.IsValid() {
							return last
						}
						return last.(*types.Slice).Elem()
					}
					if i < ps.Len() {
						return ps.At(i).Type()
					}
					return nil
				}
			}
			for i, arg := range call.Args {
				if pt := param(i); pt != nil && types.IsInterface(pt) && Holds(info.TypeOf(arg), match) {
					if !visit(call, i, arg) {
						stopped = true
						return false
					}
				}
			}
			return true
		})
		if stopped {
			return
		}
	}
}

// printer describes a function that only prints its arguments from index
// args on, with the format at args-1 when formatted.
type printer struct {
	args      int
	formatted bool
}

// printers are fmt's, log's and testing's printing functions, by full name.
var printers = func() map[string]printer {
	m := map[string]printer{
		"fmt.Print": {0, false}, "fmt.Println": {0, false}, "fmt.Printf": {1, true},
		"fmt.Sprint": {0, false}, "fmt.Sprintln": {0, false}, "fmt.Sprintf": {1, true},
		"fmt.Errorf": {1, true},
		"fmt.Fprint": {1, false}, "fmt.Fprintln": {1, false}, "fmt.Fprintf": {2, true},
		"fmt.Append": {1, false}, "fmt.Appendln": {1, false}, "fmt.Appendf": {2, true},
	}
	for _, recv := range []string{"log.", "(*log.Logger)."} {
		for _, name := range []string{"Print", "Println", "Fatal", "Fatalln", "Panic", "Panicln"} {
			m[recv+name] = printer{0, false}
		}
		for _, name := range []string{"Printf", "Fatalf", "Panicf"} {
			m[recv+name] = printer{1, true}
		}
	}
	for _, recv := range []string{"(*testing.common).", "(testing.TB)."} {
		for _, name := range []string{"Error", "Fatal", "Log", "Skip"} {
			m[recv+name] = printer{0, false}
			m[recv+name+"f"] = printer{1, true}
		}
	}
	return m
}()

// sight says what the callee of call sees of its argument i: a printer
// prints the value for every verb but %T, its field names only for %+v
// and %#v, and neither for a value that prints itself.
func sight(info *types.Info, call *ast.CallExpr, i int) Sight {
	fn, _ := info.Uses[IdentOf(call.Fun)].(*types.Func)
	if fn == nil {
		return SeesNames
	}
	if fn.FullName() == "reflect.DeepEqual" {
		return Compares
	}
	p, ok := printers[fn.FullName()]
	if !ok || i < p.args {
		return SeesNames
	}
	t := info.TypeOf(call.Args[i])
	if !p.formatted {
		return printedBy(t, verb{r: 'v'})
	}
	format := info.Types[call.Args[p.args-1]].Value
	if format == nil || format.Kind() != constant.String || call.Ellipsis.IsValid() {
		return PrintsNames
	}
	verbs, ok := formatVerbs(constant.StringVal(format))
	if !ok {
		return PrintsNames
	}
	vs, ok := verbs[i-p.args]
	if !ok {
		vs = []verb{{r: 'v'}} // an extra argument prints as %v
	}
	s := Blind
	for _, v := range vs {
		s = max(s, printedBy(t, v))
	}
	return s
}

// printedBy says what fmt prints of a value of type t for the verb v.
func printedBy(t types.Type, v verb) Sight {
	switch {
	case v.r == 'T' || printsItself(t, "Format"):
		return Blind
	case v.r == 'v' && v.sharp:
		if printsItself(t, "GoString") {
			return Blind
		}
		return PrintsNames
	case strings.ContainsRune("vsxXq", v.r) && printsItself(t, "Error", "String"):
		return Blind
	case v.r == 'v' && v.plus:
		return PrintsNames
	}
	return Prints
}

// printsItself reports whether fmt prints a value of type t by one of the
// methods names (fmt.Formatter's, fmt.Stringer's, error's, fmt.GoStringer's)
// rather than by reflection.
func printsItself(t types.Type, names ...string) bool {
	ms := types.NewMethodSet(t)
	for _, name := range names {
		sel := ms.Lookup(nil, name)
		if sel == nil {
			continue
		}
		sig := sel.Obj().(*types.Func).Signature()
		if name == "Format" && sig.Params().Len() == 2 || name != "Format" && sig.Params().Len() == 0 && sig.Results().Len() == 1 {
			return true
		}
	}
	return false
}

// verb is a verb of a fmt format, with the flags that make %v print field
// names: + and #.
type verb struct {
	r           rune
	plus, sharp bool
}

// formatVerbs parses a fmt format and returns the verbs that print each
// argument, by index; ok is false when the format is malformed.
func formatVerbs(format string) (verbs map[int][]verb, ok bool) {
	verbs = map[int][]verb{}
	arg := 0
	for i := 0; i < len(format); {
		if format[i] != '%' {
			i++
			continue
		}
		i++
		var v verb
	flags:
		for ; i < len(format); i++ {
			switch format[i] {
			case '+':
				v.plus = true
			case '#':
				v.sharp = true
			case '-', ' ', '0':
			default:
				break flags
			}
		}
		index := func() bool { // [n] chooses the argument
			if i >= len(format) || format[i] != '[' {
				return true
			}
			end := strings.IndexByte(format[i:], ']')
			if end < 0 {
				return false
			}
			n, err := strconv.Atoi(format[i+1 : i+end])
			if err != nil || n < 1 {
				return false
			}
			arg = n - 1
			i += end + 1
			return true
		}
		number := func() { // a width or precision; * takes an argument
			if i < len(format) && format[i] == '*' {
				i++
				arg++
				return
			}
			for i < len(format) && '0' <= format[i] && format[i] <= '9' {
				i++
			}
		}
		if !index() {
			return nil, false
		}
		number()
		if i < len(format) && format[i] == '.' {
			i++
			if !index() {
				return nil, false
			}
			number()
		}
		if !index() || i >= len(format) {
			return nil, false
		}
		r, size := utf8.DecodeRuneInString(format[i:])
		i += size
		if r == '%' {
			continue
		}
		v.r = r
		verbs[arg] = append(verbs[arg], v)
		arg++
	}
	return verbs, true
}

// Holds reports whether a value of type t holds one of a type match
// accepts: t itself, or what it points to, its elements, keys and fields,
// as reflection would walk them. Interfaces, functions and channels are not
// looked into.
func Holds(t types.Type, match func(types.Type) bool) bool {
	seen := map[types.Type]bool{}
	var holds func(t types.Type) bool
	holds = func(t types.Type) bool {
		if t == nil || seen[t] {
			return false
		}
		seen[t] = true
		if match(t) {
			return true
		}
		switch u := t.(type) {
		case *types.Named:
			return holds(u.Underlying())
		case *types.Alias:
			return holds(types.Unalias(u))
		case *types.Pointer:
			return holds(u.Elem())
		case *types.Slice:
			return holds(u.Elem())
		case *types.Array:
			return holds(u.Elem())
		case *types.Map:
			return holds(u.Key()) || holds(u.Elem())
		case *types.Struct:
			for i := 0; i < u.NumFields(); i++ {
				if holds(u.Field(i).Type()) {
					return true
				}
			}
		}
		return false
	}
	return holds(t)
}
