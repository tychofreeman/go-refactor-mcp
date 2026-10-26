package analysis

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

const effectsSrc = `package p

import (
	"errors"
	"fmt"
	"go/ast"
	"sort"
)

var counter int

type counterT struct{ n int }

func pureAdd(a, b int) int { return a + b }

func div(a, b int) int { return a / b }

func readGlobal() int { return counter }

func bump() { counter++ }

func first(xs []int) int { return xs[0] }

func setFirst(xs []int) { xs[0] = 1 }

func build(n int) []int {
	out := make([]int, 0, n)
	for i := range n {
		out = append(out, i)
	}
	return out
}

func fill(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

func mapBuild(keys []string) map[string]bool {
	m := map[string]bool{}
	for _, k := range keys {
		m[k] = true
	}
	return m
}

func callsPure() int { return pureAdd(1, 2) }

func callsBump() { bump() }

func loop(n int) int {
	for n > 1 {
		n /= 2
	}
	return n
}

func recursive(n int) int {
	if n == 0 {
		return 0
	}
	return recursive(n - 1)
}

func iface(s fmt.Stringer) string { return s.String() }

func printIt() { fmt.Println() }

func aliasWrite(n int) []int {
	out := make([]int, n)
	alias := out
	alias[0] = 1
	return out
}

func (c *counterT) get() int { return c.n }

func (c *counterT) inc() { c.n++ }

func (c counterT) incCopy() int {
	c.n++
	return c.n
}

func visit(n ast.Node) int {
	k := 0
	ast.Inspect(n, func(ast.Node) bool {
		k++
		return true
	})
	return k
}

func sortFresh(n int) []int {
	xs := make([]int, n)
	sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
	return xs
}

func sortParam(xs []int) {
	sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
}

func newErr() error { return errors.New("x") }

func goIt() { go pureAdd(1, 2) }

func ptrFresh() int {
	p := &counterT{}
	p.n = 3
	return p.n
}

func callsGet(c *counterT) int { return c.get() }

func kind(x any) string {
	switch v := x.(type) {
	case int:
		_ = v
		return "int"
	}
	return "other"
}
`

func TestEffectsFunc(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", effectsSrc, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{
		Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{},
		Types: map[ast.Expr]types.TypeAndValue{}, Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	conf := types.Config{Importer: importer.Default()}
	pkg, err := conf.Check("example.com/p", fset, []*ast.File{f}, info)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEffects()
	e.AddPackage([]*ast.File{f}, info)
	// Which of effect, reads and unsafe each function has.
	want := map[string][3]bool{
		"pureAdd":    {false, false, false},
		"div":        {false, false, true},
		"readGlobal": {false, true, false},
		"bump":       {true, true, false},
		"first":      {false, true, true},
		"setFirst":   {true, true, true},
		"build":      {false, false, true}, // make with a size of n, which may be negative
		"fill":       {false, false, true},
		"mapBuild":   {false, true, false},
		"callsPure":  {false, false, false},
		"callsBump":  {true, true, false},
		"loop":       {false, false, true},
		"recursive":  {true, false, false},
		"iface":      {true, false, true},
		"printIt":    {true, false, false},
		"aliasWrite": {true, true, true},
		"visit":      {false, true, false},
		"sortFresh":  {false, false, true},
		"sortParam":  {true, true, true},
		"newErr":     {false, false, false},
		"goIt":       {true, false, false},
		"ptrFresh":   {false, false, false},
		"callsGet":   {false, true, true},
		"get":        {false, true, true},
		"inc":        {true, true, true},
		"incCopy":    {false, false, false},
		"kind":       {false, false, false},
	}
	counterT := pkg.Scope().Lookup("counterT").Type()
	for name, w := range want {
		obj := pkg.Scope().Lookup(name)
		if obj == nil {
			ms := types.NewMethodSet(types.NewPointer(counterT))
			for i := 0; i < ms.Len(); i++ {
				if ms.At(i).Obj().Name() == name {
					obj = ms.At(i).Obj()
				}
			}
		}
		fn, _ := obj.(*types.Func)
		if fn == nil {
			t.Fatalf("no function %s", name)
		}
		s := e.Func(fn)
		if s == nil {
			t.Errorf("%s: no summary", name)
			continue
		}
		got := [3]bool{s.Effect != "", s.Reads != "", s.Unsafe != ""}
		if got != w {
			t.Errorf("%s: effect, reads, unsafe = %v, want %v (summary %+v)", name, got, w, *s)
		}
	}
}

const stdSrc = `package p

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type named struct{ n int }

func (named) String() string { return "named" }

func f(path string, xs []string, n ast.Node, tf *token.File, fset *token.FileSet, pn *types.PkgName, t types.Type, sb *strings.Builder, err error, v named, o types.Object, fi fs.FileInfo, tt *testing.T) {
	_, _ = os.ReadFile(path)
	_ = strings.Join(xs, ",")
	sort.Strings(xs)
	_ = n.Pos()
	_ = tf.Offset(n.Pos())
	_ = fset.Position(n.Pos())
	_ = pn.Name()
	_ = t.Underlying()
	sb.WriteString("x")
	_ = fmt.Sprintf("%d %s", 1, path)
	_ = fmt.Sprintf("%v", xs)
	_ = fmt.Sprintf("%v", err)
	_ = fmt.Sprintf("%v", v)
	_ = errors.New("x")
	_, _ = filepath.Rel(path, path)
	_ = ast.Unparen(nil)
	_ = token.NewFileSet()
	fmt.Println()
	_ = o.Pos()
	_ = fi.IsDir()
	tt.Helper()
	_ = filepath.Join(xs...)
	_ = ast.NewIdent("x")
}

func builder(names []string) string {
	var sb strings.Builder
	for _, n := range names {
		sb.WriteString(n)
		fmt.Fprintf(&sb, "%d", len(n))
	}
	return sb.String()
}

func sorted(n int) []string {
	out := make([]string, n)
	sort.Strings(out)
	return out
}

func sortsParam(xs []string) { sort.Strings(xs) }
`

// checkStd type-checks stdSrc and returns its package, file and info.
func checkStd(t *testing.T) (*types.Package, *ast.File, *types.Info) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", stdSrc, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{
		Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{},
		Types: map[ast.Expr]types.TypeAndValue{}, Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	conf := types.Config{Importer: importer.Default()}
	pkg, err := conf.Check("example.com/p", fset, []*ast.File{f}, info)
	if err != nil {
		t.Fatal(err)
	}
	return pkg, f, info
}

func TestEffectsCallStandardLibrary(t *testing.T) {
	_, f, info := checkStd(t)
	e := NewEffects()
	e.AddPackage([]*ast.File{f}, info)
	// Which of effect, reads and unsafe each call itself has.
	want := map[string][3]bool{
		"os.ReadFile(path)":             {false, true, false},
		`strings.Join(xs, ",")`:         {false, true, true}, // a result too long
		"sort.Strings(xs)":              {true, true, false},
		"n.Pos()":                       {false, true, true},
		"tf.Offset(n.Pos())":            {false, true, true},
		"fset.Position(n.Pos())":        {false, true, true},
		"pn.Name()":                     {false, false, true},
		"t.Underlying()":                {false, false, true},
		`sb.WriteString("x")`:           {true, false, true},
		`fmt.Sprintf("%d %s", 1, path)`: {false, false, false},
		`fmt.Sprintf("%v", xs)`:         {false, true, false},
		`fmt.Sprintf("%v", err)`:        {true, false, false},
		`fmt.Sprintf("%v", v)`:          {true, false, false},
		`errors.New("x")`:               {false, false, false},
		"filepath.Rel(path, path)":      {false, false, false},
		"ast.Unparen(nil)":              {false, true, false},
		"token.NewFileSet()":            {false, false, false},
		"fmt.Println()":                 {true, false, false},
		"o.Pos()":                       {false, false, true},
		"fi.IsDir()":                    {false, false, true},
		"tt.Helper()":                   {false, false, true},
		"filepath.Join(xs...)":          {false, true, false},
		`ast.NewIdent("x")`:             {false, false, false},
	}
	seen := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		text := types.ExprString(call)
		w, ok := want[text]
		if !ok {
			return true
		}
		seen[text] = true
		s := e.Call(info, call)
		if got := [3]bool{s.Effect != "", s.Reads != "", s.Unsafe != ""}; got != w {
			t.Errorf("%s: effect, reads, unsafe = %v, want %v (summary %+v)", text, got, w, s)
		}
		return true
	})
	for text := range want {
		if !seen[text] {
			t.Errorf("no call %s in the source", text)
		}
	}
}

// Functions that call a function literal during the call: what the call
// itself does with its other arguments, and that the literal runs then.
func TestEffectsCallSyncCallees(t *testing.T) {
	src := `package p

import (
	"bytes"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

func f(s string, b []byte, xs, ys []int, m map[string]int, n int, root string) {
	_ = strings.FieldsFunc(s, func(r rune) bool { return r == 32 })
	_ = bytes.FieldsFunc(b, func(r rune) bool { return false })
	_ = slices.DeleteFunc(xs, func(int) bool { return false })
	_ = slices.EqualFunc(xs, ys, func(a, b int) bool { return a == b })
	_ = slices.MinFunc(xs, func(a, b int) int { return a - b })
	_ = sort.Search(n, func(i int) bool { return i > 2 })
	_ = filepath.WalkDir(root, func(string, fs.DirEntry, error) error { return nil })
	maps.DeleteFunc(m, func(string, int) bool { return false })
	sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{
		Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{},
		Types: map[ast.Expr]types.TypeAndValue{}, Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	if _, err := (&types.Config{Importer: importer.Default()}).Check("example.com/p", fset, []*ast.File{file}, info); err != nil {
		t.Fatal(err)
	}
	e := NewEffects()
	e.AddPackage([]*ast.File{file}, info)
	want := map[string][3]bool{ // effect, reads, unsafe
		"strings.FieldsFunc": {false, false, false}, // strings cannot change
		"bytes.FieldsFunc":   {false, true, false},
		"slices.DeleteFunc":  {true, true, false},
		"slices.EqualFunc":   {false, true, false},
		"slices.MinFunc":     {false, true, true}, // panics on an empty slice
		"sort.Search":        {false, false, false},
		"filepath.WalkDir":   {true, false, false}, // the file system
		"maps.DeleteFunc":    {true, true, false},
		"sort.Slice":         {true, true, false},
	}
	seen := 0
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := types.ExprString(call.Fun)
		w, ok := want[name]
		if !ok {
			return true
		}
		seen++
		s := e.Call(info, call)
		if got := [3]bool{s.Effect != "", s.Reads != "", s.Unsafe != ""}; got != w {
			t.Errorf("%s: effect, reads, unsafe = %v, want %v (summary %+v)", name, got, w, s)
		}
		if lits := SyncLits(info, call); len(lits) != 1 {
			t.Errorf("%s: %d literals run during the call, want 1", name, len(lits))
		}
		return true
	})
	if seen != len(want) {
		t.Errorf("checked %d calls, want %d", seen, len(want))
	}
}

// make panics on a slice length or capacity, or a channel buffer size,
// that is negative or too large; a map's size is only a hint.
func TestEffectsCallMake(t *testing.T) {
	src := `package p

func f(n int, u uint, s []int) {
	_ = make([]int, 4)
	_ = make([]int, n)
	_ = make([]int, 0, u)
	_ = make(map[int]int, n)
	_ = make(chan int, n)
	_ = make(chan int)
	_ = new(int)
	_ = make([]int, len(s), cap(s)+1)
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Types: map[ast.Expr]types.TypeAndValue{}}
	if _, err := (&types.Config{}).Check("example.com/p", fset, []*ast.File{file}, info); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{ // unsafe
		"make([]int, 4)":                false,
		"make([]int, n)":                true,
		"make([]int, 0, u)":             true,
		"make(map[int]int, n)":          false,
		"make(chan int, n)":             true,
		"make(chan int)":                false,
		"new(int)":                      false,
		"make([]int, len(s), cap(s)+1)": false,
	}
	e := NewEffects()
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			text := types.ExprString(call)
			if s := e.Call(info, call); (s.Unsafe != "") != want[text] || s.Effect != "" || s.Reads != "" {
				t.Errorf("%s: %+v, want unsafe %v and nothing else", text, s, want[text])
			}
		}
		return true
	})
}

func TestEffectsFuncStandardLibraryWrites(t *testing.T) {
	pkg, f, info := checkStd(t)
	e := NewEffects()
	e.AddPackage([]*ast.File{f}, info)
	want := map[string][3]bool{
		"builder":    {false, true, false},
		"sorted":     {false, false, true}, // make with a size of n
		"sortsParam": {true, true, false},
	}
	for name, w := range want {
		s := e.Func(pkg.Scope().Lookup(name).(*types.Func))
		if s == nil {
			t.Fatalf("%s: no summary", name)
		}
		if got := [3]bool{s.Effect != "", s.Reads != "", s.Unsafe != ""}; got != w {
			t.Errorf("%s: effect, reads, unsafe = %v, want %v (summary %+v)", name, got, w, *s)
		}
	}
}

const whereSrc = `package p

type T struct{ n int }

func field(p *T) int { return p.n }

func quot(x, y int) int { return x / y }

func both(p *T, x, y int) int { return p.n + x/y }

func viaCall(p *T) int { return field(p) }

func spin() {
	for {
	}
}

func set(p *T) { p.n = 1 }

func first(xs []int) int { return xs[0] }
`

// TestEffectsWhere checks where a summary says its findings are, and how
// it says the code can panic.
func TestEffectsWhere(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", whereSrc, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{
		Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{},
		Types: map[ast.Expr]types.TypeAndValue{}, Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	pkg, err := (&types.Config{}).Check("example.com/p", fset, []*ast.File{f}, info)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEffects()
	e.AddPackage([]*ast.File{f}, info)
	text := func(n ast.Node) string {
		switch n := n.(type) {
		case nil:
			return ""
		case ast.Expr:
			return types.ExprString(n)
		case *ast.ForStmt:
			return "for"
		case *ast.AssignStmt:
			return types.ExprString(n.Lhs[0]) + " = " + types.ExprString(n.Rhs[0])
		}
		return "?"
	}
	type where struct {
		effect, reads, unsafe, panic string
		panics                       PanicKind
	}
	want := map[string]where{
		"field":   {reads: "p.n", unsafe: "p.n", panics: NilPanic},
		"quot":    {unsafe: "x / y", panic: "x / y", panics: OtherPanic},
		"both":    {reads: "p.n", unsafe: "p.n", panic: "x / y", panics: OtherPanic},
		"viaCall": {reads: "field(p)", unsafe: "field(p)", panics: NilPanic},
		"spin":    {unsafe: "for"},
		"set":     {effect: "p.n = 1", reads: "p.n", unsafe: "p.n", panics: NilPanic}, // a write is at its statement
		"first":   {reads: "xs[0]", unsafe: "xs[0]", panic: "xs[0]", panics: OtherPanic},
	}
	for name, w := range want {
		s := e.Func(pkg.Scope().Lookup(name).(*types.Func))
		got := where{text(s.At.Effect), text(s.At.Reads), text(s.At.Unsafe), text(s.At.OtherPanic), s.Panics}
		if got != w {
			t.Errorf("%s: got %+v, want %+v (summary %+v)", name, got, w, *s)
		}
	}
}

const ofSrc = `package p

type T struct{ n int }

var counter int

func bump() int {
	counter++
	return counter
}

func field(p *T) int { return p.n }

func g(p *T, x, y int) {
	local := &T{}
	_ = p.n
	_ = x / y
	_ = x + y
	_ = local.n
	_ = field(p)
	_ = bump()
}
`

// TestEffectsOf checks the summaries of single expressions of a function.
func TestEffectsOf(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", ofSrc, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{
		Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{},
		Types: map[ast.Expr]types.TypeAndValue{}, Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	if _, err := (&types.Config{}).Check("example.com/p", fset, []*ast.File{f}, info); err != nil {
		t.Fatal(err)
	}
	e := NewEffects()
	e.AddPackage([]*ast.File{f}, info)
	type verdict struct {
		effect, reads, unsafe bool
		panics                PanicKind
	}
	want := map[string]verdict{
		"p.n":      {reads: true, unsafe: true, panics: NilPanic},
		"x / y":    {unsafe: true, panics: OtherPanic},
		"x + y":    {},
		"local.n":  {reads: true, unsafe: true, panics: NilPanic}, // memory the rest of g can change
		"field(p)": {reads: true, unsafe: true, panics: NilPanic},
		"bump()":   {effect: true, reads: true},
	}
	g := f.Decls[len(f.Decls)-1].(*ast.FuncDecl)
	fa := AnalyzeFunc(info, g)
	seen := 0
	for _, st := range g.Body.List {
		as, ok := st.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != 1 || types.ExprString(as.Lhs[0]) != "_" {
			continue
		}
		x := as.Rhs[0]
		w, ok := want[types.ExprString(x)]
		if !ok {
			t.Fatalf("no verdict for %s", types.ExprString(x))
		}
		seen++
		s := e.Of(fa, x)
		got := verdict{s.Effect != "", s.Reads != "", s.Unsafe != "", s.Panics}
		if got != w {
			t.Errorf("%s: got %+v, want %+v (summary %+v)", types.ExprString(x), got, w, s)
		}
	}
	if seen != len(want) {
		t.Errorf("checked %d expressions, want %d", seen, len(want))
	}
}
