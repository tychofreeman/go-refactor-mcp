// Package testfor writes a red test from what it tests: the call of a
// function or method with placeholder arguments, its results kept, and a
// failure that shows them until the assertion is written.
package testfor

import (
	"fmt"
	"go/types"
	"strings"
	"unicode"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
)

// Source returns a test function for target, a function or Type.Method
// of pkg, named Test and the target's name, with a number when pkg
// declares that name already. A method's receiver comes from the type's
// constructor with the fewest parameters (a function of pkg whose first
// result is the type or a pointer to it), else is the type's zero value.
func Source(pkg *types.Package, target string) (string, error) {
	qualify := types.RelativeTo(pkg)
	typeName, method, isMethod := strings.Cut(target, ".")
	var b strings.Builder
	var sig *types.Signature
	var call string
	if isMethod {
		tn, ok := pkg.Scope().Lookup(typeName).(*types.TypeName)
		if !ok {
			return "", refactor.Refusef("test_for names %s, but the package declares no type %s; give a function, or Type.Method", target, typeName)
		}
		obj, _, _ := types.LookupFieldOrMethod(types.NewPointer(tn.Type()), true, pkg, method)
		fn, ok := obj.(*types.Func)
		if !ok {
			return "", refactor.Refusef("%s has no method %s", typeName, method)
		}
		sig = fn.Type().(*types.Signature)
		recv := lowerFirst(typeName)
		if ctor := constructor(pkg, tn); ctor != nil {
			fmt.Fprintf(&b, "\t%s := %s(%s)\n", recv, ctor.Name(), zeros(ctor.Type().(*types.Signature), qualify))
		} else {
			fmt.Fprintf(&b, "\tvar %s %s\n", recv, types.TypeString(tn.Type(), qualify))
		}
		call = fmt.Sprintf("%s.%s(%s)", recv, method, zeros(sig, qualify))
	} else {
		fn, ok := pkg.Scope().Lookup(target).(*types.Func)
		if !ok {
			return "", refactor.Refusef("test_for names %s, but the package declares no function %s; give a function, or Type.Method", target, target)
		}
		sig = fn.Type().(*types.Signature)
		method = target
		call = fmt.Sprintf("%s(%s)", target, zeros(sig, qualify))
	}
	var names []string
	for i := range sig.Results().Len() {
		v := sig.Results().At(i)
		name := v.Name()
		switch {
		case name != "" && name != "_" && name != "t":
		case types.Identical(v.Type(), types.Universe.Lookup("error").Type()):
			name = "err"
		case i == 0:
			name = lowerFirst(method)
		default:
			name = fmt.Sprintf("r%d", i+1)
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		fmt.Fprintf(&b, "\t%s\n\tt.Fatal(\"write the assertion\")\n", call)
	} else {
		verbs := strings.TrimSuffix(strings.Repeat("%v, ", len(names)), ", ")
		fmt.Fprintf(&b, "\t%s := %s\n\tt.Fatalf(\"write the assertion: %s returned %s\", %s)\n", strings.Join(names, ", "), call, method, verbs, strings.Join(names, ", "))
	}
	name := "Test" + typeName + upperFirst(method)
	if !isMethod {
		name = "Test" + upperFirst(target)
	}
	for i, base := 2, name; pkg.Scope().Lookup(name) != nil; i++ {
		name = fmt.Sprintf("%s%d", base, i)
	}
	return fmt.Sprintf("func %s(t *testing.T) {\n%s}", name, b.String()), nil
}

// constructor returns the function of pkg with the fewest parameters whose
// first result is tn's type or a pointer to it, or nil.
func constructor(pkg *types.Package, tn *types.TypeName) *types.Func {
	var best *types.Func
	for _, n := range pkg.Scope().Names() {
		fn, ok := pkg.Scope().Lookup(n).(*types.Func)
		if !ok || !strings.HasPrefix(n, "New") && !strings.HasPrefix(n, "new") {
			continue
		}
		sig := fn.Type().(*types.Signature)
		if sig.Results().Len() == 0 || sig.Variadic() {
			continue
		}
		r := sig.Results().At(0).Type()
		if p, ok := r.(*types.Pointer); ok {
			r = p.Elem()
		}
		if r != tn.Type() {
			continue
		}
		if best == nil || sig.Params().Len() < best.Type().(*types.Signature).Params().Len() {
			best = fn
		}
	}
	return best
}

// zeros returns placeholder arguments for sig's parameters: each type's
// zero value, and none for a variadic parameter.
func zeros(sig *types.Signature, qualify types.Qualifier) string {
	var out []string
	for i := range sig.Params().Len() {
		if sig.Variadic() && i == sig.Params().Len()-1 {
			break
		}
		out = append(out, zero(sig.Params().At(i).Type(), qualify))
	}
	return strings.Join(out, ", ")
}

// zero returns the zero value of t, as Go source.
func zero(t types.Type, qualify types.Qualifier) string {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch {
		case u.Info()&types.IsBoolean != 0:
			return "false"
		case u.Info()&types.IsString != 0:
			if _, named := t.(*types.Named); named {
				return types.TypeString(t, qualify) + "(\"\")"
			}
			return `""`
		case u.Info()&types.IsNumeric != 0:
			return "0"
		}
	case *types.Struct, *types.Array:
		return types.TypeString(t, qualify) + "{}"
	}
	return "nil"
}

func lowerFirst(s string) string {
	for i, r := range s {
		return string(unicode.ToLower(r)) + s[i+len(string(r)):]
	}
	return s
}

func upperFirst(s string) string {
	for i, r := range s {
		return string(unicode.ToUpper(r)) + s[i+len(string(r)):]
	}
	return s
}
