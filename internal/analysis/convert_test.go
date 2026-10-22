package analysis

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

// TestBecomesInterface: a value of T reaches an interface by being
// passed, assigned, returned, stored, compared, sent or panicked with
// where an interface or a type parameter is wanted; used as a receiver,
// copied, indexed or pointed to, it does not.
func TestBecomesInterface(t *testing.T) {
	tests := []struct {
		name, body string
		want       bool
	}{
		{"receiver only", `func f() { T{}.m(); var t T; t.m(); u := t; u.m(); ts := []T{t}; ts[0].m(); p := &t; p.m(); (*p).m() }`, false},
		{"field of a struct", `type S struct{ t T }; func f() S { s := S{t: T{}}; s.t.m(); return s }`, false},
		{"passed as any", `func g(v any) {}; func f() { g(T{}) }`, true},
		{"assigned to any", `func f() { var v any = T{}; _ = v }`, true},
		{"returned as any", `func f() any { return T{} }`, true},
		{"stored in an any field", `type S struct{ v any }; func f() S { return S{v: T{}} }`, true},
		{"pointer passed", `func g(v any) {}; func f() { g(&T{}) }`, true},
		{"slice passed", `func g(v any) {}; func f() { g([]T{}) }`, true},
		{"struct holding it passed", `type S struct{ t T }; func g(v any) {}; func f() { g(S{}) }`, true},
		{"type parameter", `func h[X any](x X) {}; func f() { h(T{}) }`, true},
		{"panic", `func f() { panic(T{}) }`, true},
		{"compared with any", `func f(v any) bool { return v == T{} }`, true},
		{"appended to []any", `func f() []any { return append([]any{}, T{}) }`, true},
		{"map key any", `func f() { m := map[any]int{}; m[T{}] = 1 }`, true},
		{"converted", `func f() { _ = any(T{}) }`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := "package p\n\ntype T struct{}\n\nfunc (T) m() {}\n\n" + tt.body + "\n"
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "p.go", src, 0)
			if err != nil {
				t.Fatal(err)
			}
			info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}, Instances: map[*ast.Ident]types.Instance{}, Types: map[ast.Expr]types.TypeAndValue{}}
			pkg, err := (&types.Config{}).Check("example.com/p", fset, []*ast.File{f}, info)
			if err != nil {
				t.Fatal(err)
			}
			T := pkg.Scope().Lookup("T").Type()
			got := BecomesInterface(info, []*ast.File{f}, func(t types.Type) bool { return types.Identical(t, T) })
			if (got != nil) != tt.want {
				t.Errorf("BecomesInterface = %v, want found %v", got, tt.want)
			}
		})
	}
}
