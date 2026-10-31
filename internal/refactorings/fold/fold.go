// Package fold implements "fold": replace code that does what an
// existing function does with calls to it.
package fold

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/constant"
	"go/printer"
	"go/token"
	"go/types"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/pattern"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
	"golang.org/x/tools/go/packages"
)

// Fold is the "fold" refactoring.
type Fold struct{ refactor.Named[foldArgs] }

// New returns the fold refactoring.
func New() *Fold { return &Fold{Named: refactor.Called[foldArgs]("fold")} }

func (*Fold) Description() string {
	return `Replace code that does what an existing function does with calls to it: every match of the function's body in the module, its parameters standing for any expressions of their types, becomes a call. The function stays. extract_function makes a function from one place; fold reuses one, written or extracted, everywhere else.
What a body matches:
- One return: s.Unit becomes s.unit() for func (s Sensor) unit() string { return s.Unit }.
- A struct parameter used only by its fields: Feed(z, ps, ml) becomes Feed2(z, Dose{Plants: ps, Millilitres: ml}) for func Feed2(z string, d Dose) string { return Feed(z, d.Plants, d.Millilitres) }, to move callers to a parameter object.
- A setter, which sets fields of a struct parameter and returns it: Plant{ID: id, Colour: v} becomes withColour(Plant{ID: id}, v).
- Statements, for a function without results: the same statements in any block become one call, so near-duplicate methods become calls of one function you declare. A *p matches a field, a %s of a format matches constant text, and a string parameter matches part of a + concatenation: in func (s *Sprinkler) Run(bed, note string) { log.Printf("sprinkler on %s", bed); s.Log = append(s.Log, bed+": "+note+"!") } the two statements become record(&s.Log, "sprinkler", bed, note+"!") for func record(entries *[]string, device, bed, text string) { log.Printf("%s on %s", device, bed); *entries = append(*entries, bed+": "+text) }.
Identify the function by file and name (Name or Type.Method); with line (and in), only the matches on that line are folded, to move callers over one at a time.
A match where the call would not do the same (an argument evaluated in another order or another number of times, a call that cannot be made from there) is left, and the result says why.`
}

type foldArgs struct {
	File string `json:"file" desc:"Path of the file that declares the function, relative to the workspace root."`
	Name string `json:"name" desc:"The function: Name, or Type.Method for a method."`
	Line int    `json:"line,omitempty" desc:"Optional: fold only the matches that start on this line of file (or of in)."`
	In   string `json:"in,omitempty" desc:"Optional, with line: the file whose line it is, if not file."`
}

type relaxer struct {
	addressOf map[ast.Expr]bool       // arguments &x made for a *p of the body that x matched
	made      map[ast.Expr]bool       // string literals made from a format's text, for a %s of the body
	runs      map[ast.Expr][]ast.Expr // concatenations made of a run of operands, for a string parameter of the body
}

// Call matches a format string whose %s takes text of the call's.
func (r *relaxer) Call(a *pattern.Attempt, p, x *ast.CallExpr) bool {
	return matchFormat(a, p, x, r)
}

// Concat matches a string concatenation whose operands make up the pattern's.
func (r *relaxer) Concat(a *pattern.Attempt, p, x *ast.BinaryExpr) bool {
	return matchConcat(a, p, x, r)
}

// Address matches *p against an addressable expression, passed as &x.
func (r *relaxer) Address(a *pattern.Attempt, p *ast.StarExpr, x ast.Expr) bool {
	return matchAddress(a, p, x, r)
}

// folder finds the code that does what fn does and replaces it with calls.
type folder struct {
	ws *workspace.Workspace
	m  pattern.Matcher
	relaxer
	fn    *types.Func
	decl  *ast.FuncDecl
	body  ast.Expr                // what fn returns: the pattern; or nil, and
	stmts []ast.Stmt              // fn's statements: the pattern, for a function without results
	root  ast.Node                // body or fn's block: where the pattern's names are
	done  [][2]token.Pos          // statements folded, which a later match must not overlap
	uses  map[pattern.HoleKey]int // how often the body uses each field of fields
	// self and sets are a setter's: the struct parameter it sets fields
	// of and returns, and the fields with what it sets them to, in order.
	self types.Object
	sets []set
	rest map[ast.Expr]rest // literals folded through the setter, by what to write for them

	n       int           // occurrences folded
	skipped []string      // why matches were left, "file:line: reason"
	nearest *pattern.Miss // of the matches that failed, the one that got furthest
	by      map[string][]edit.TextEdit
	adders  map[string]*imports.Adder
	order   []string
	removed []string // imports removed because only folded code used them
}

// count is how often the body uses the hole k.
func (f *folder) count(k pattern.HoleKey) int {
	if k.Field != "" {
		return f.uses[k]
	}
	return f.m.Holes[k.Obj]
}

// pattern reads fn's declaration: its body must be one return of one
// value, which uses every parameter.
func (f *folder) pattern(id *ast.Ident) error {
	for _, file := range f.m.Pkg.Syntax {
		for _, d := range file.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Name == id {
				f.decl = fd
			}
		}
	}
	name := f.fn.Name()
	if f.decl == nil || f.decl.Body == nil {
		return refactor.Refusef("cannot find the body of %s", name)
	}
	sig := f.fn.Signature()
	if sig.TypeParams().Len() > 0 || sig.RecvTypeParams().Len() > 0 {
		return refactor.Refusef("%s is generic; folding into generic functions is not supported yet", name)
	}
	if sig.Variadic() {
		return refactor.Refusef("%s is variadic; folding into variadic functions is not supported yet", name)
	}
	list := f.decl.Body.List
	if ret, ok := list[0].(*ast.ReturnStmt); len(list) == 1 && ok && len(ret.Results) == 1 {
		f.body, f.root = ret.Results[0], ret.Results[0]
	} else if sig.Results().Len() == 1 && f.setter(list) {
		f.root = f.decl.Body
	} else if sig.Results().Len() > 0 {
		return refactor.Refusef("fold takes a function whose body is one return statement of one value, sets fields of a struct parameter and returns it, or has no results; %s is none of these. To reuse it where its statements are copied, extract_function each copy of its statements, then merge_duplicate_function the result into %s", name, name)
	} else if why := statementsOnly(f.decl.Body); why != "" {
		return refactor.Refusef("%s %s, which a call of it would not do in place of its statements", name, why)
	} else {
		f.stmts, f.root = list, f.decl.Body
	}
	info := f.m.Pkg.TypesInfo
	for _, fl := range f.decl.Type.Params.List {
		if len(fl.Names) == 0 {
			return refactor.Refusef("the parameters of %s have no names", name)
		}
		for _, n := range fl.Names {
			f.m.Holes[info.Defs[n]] = 0
		}
	}
	if r := f.decl.Recv; r != nil && len(r.List[0].Names) == 1 && r.List[0].Names[0].Name != "_" {
		f.m.Recv = info.Defs[r.List[0].Names[0]]
		f.m.Holes[f.m.Recv] = 0
	}
	f.m.BodyLocals = map[types.Object]types.Object{}
	ast.Inspect(f.root, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if d := info.Defs[id]; d != nil {
				f.m.BodyLocals[d] = d
			}
			if c, hole := f.m.Holes[info.Uses[id]]; hole {
				f.m.Holes[info.Uses[id]] = c + 1
			}
		}
		return true
	})
	if id, ok := ast.Unparen(f.body).(*ast.Ident); ok {
		if _, hole := f.m.Holes[info.Uses[id]]; hole {
			return refactor.Refusef("the body of %s is only its parameter %s, so every value of its type would match", name, id.Name)
		}
	}
	f.m.Fields, f.uses = map[types.Object][]string{}, map[pattern.HoleKey]int{}
	for o := range f.m.Holes {
		if _, named := o.Type().(*types.Named); o == f.m.Recv || !named {
			continue
		}
		if _, isStruct := o.Type().Underlying().(*types.Struct); !isStruct {
			continue
		}
		var order []string
		byField := 0
		ast.Inspect(f.root, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if id := pattern.IdentOf(selX(sel)); !ok || id == nil || info.Uses[id] != o {
				return true
			}
			if s := info.Selections[sel]; s != nil && s.Kind() == types.FieldVal && len(s.Index()) == 1 {
				k := pattern.HoleKey{Obj: o, Field: sel.Sel.Name}
				if f.uses[k] == 0 {
					order = append(order, sel.Sel.Name)
				}
				f.uses[k]++
				byField++
			}
			return true
		})
		// A body that is only one field of o would match every value of
		// the field's type: o stays one hole.
		if byField > 0 && byField == f.m.Holes[o] && !fieldOf(info, f.body, o) {
			f.m.Fields[o] = order
		}
	}
	for o, c := range f.m.Holes {
		if c == 0 && o != f.m.Recv {
			return refactor.Refusef("%s's body does not use its parameter %s, so a call could not be built from what matches", name, o.Name())
		}
	}
	if p := f.assignedHole(); f.stmts != nil && p != "" {
		return refactor.Refusef("%s assigns its parameter %s, which in place of the call would assign what the caller passed", name, p)
	}
	return nil
}

// shape describes the function's body for a message: its expression, or
// how many statements it has.
func (f *folder) shape() string {
	if f.sets != nil {
		var fields []string
		for _, s := range f.sets {
			fields = append(fields, s.field)
		}
		return "literals of " + pattern.BaseName(f.self.Type()).Name() + " giving " + strings.Join(fields, ", ")
	}
	if f.body != nil {
		return types.ExprString(f.body)
	}
	return fmt.Sprintf("%d statement%s", len(f.stmts), textutil.Plural(len(f.stmts)))
}

// statementsOnly is why a body of statements does not do in place what a
// call of it does: it returns, defers or jumps; or "".
func statementsOnly(body *ast.BlockStmt) string {
	why := ""
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			why = "returns from inside its body"
		case *ast.DeferStmt:
			why = "defers a call"
		case *ast.LabeledStmt:
			why = "has a label"
		case *ast.BranchStmt:
			if n.Tok == token.GOTO || n.Label != nil {
				why = "jumps to a label"
			}
		}
		return why == ""
	})
	return why
}

// assignedHole is the name of a parameter (or the receiver) the body
// assigns, increments or takes the address of, or "".
func (f *folder) assignedHole() string {
	info := f.m.Pkg.TypesInfo
	name := ""
	hole := func(e ast.Expr) {
		if id := pattern.IdentOf(e); id != nil && name == "" {
			if _, ok := f.m.Holes[info.Uses[id]]; ok {
				name = id.Name
			}
		}
	}
	ast.Inspect(f.root, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			for _, l := range n.Lhs {
				hole(l)
			}
		case *ast.IncDecStmt:
			hole(n.X)
		case *ast.UnaryExpr:
			if n.Op == token.AND {
				hole(n.X)
			}
		}
		return true
	})
	return name
}

// set is a field a setter sets, and the expression it sets it to.
type set struct {
	field string
	value ast.Expr
}

// rest is what a literal folded through a setter becomes: itself without
// the elements drop, with its type written out when it is elided.
type rest struct {
	drop []int
	typ  string
}

// setter reads a body that sets fields of a struct parameter or value
// receiver to expressions of the other parameters, then returns it; it
// reports whether the body is one.
func (f *folder) setter(list []ast.Stmt) bool {
	info := f.m.Pkg.TypesInfo
	ret, ok := list[len(list)-1].(*ast.ReturnStmt)
	if len(list) < 2 || !ok || len(ret.Results) != 1 || pattern.IdentOf(ret.Results[0]) == nil {
		return false
	}
	self := info.Uses[pattern.IdentOf(ret.Results[0])]
	params := slices.Clone(f.decl.Type.Params.List)
	if f.decl.Recv != nil {
		params = append(params, f.decl.Recv.List...)
	}
	param := false
	for _, fl := range params {
		for _, n := range fl.Names {
			param = param || info.Defs[n] == self
		}
	}
	if !param {
		return false
	}
	if _, named := types.Unalias(self.Type()).(*types.Named); !named {
		return false
	}
	if _, isStruct := self.Type().Underlying().(*types.Struct); !isStruct {
		return false
	}
	var sets []set
	for _, s := range list[:len(list)-1] {
		as, ok := s.(*ast.AssignStmt)
		if !ok || as.Tok != token.ASSIGN || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			return false
		}
		sel, ok := as.Lhs[0].(*ast.SelectorExpr)
		if !ok || pattern.IdentOf(sel.X) == nil || info.Uses[pattern.IdentOf(sel.X)] != self {
			return false
		}
		if s := info.Selections[sel]; s == nil || s.Kind() != types.FieldVal || len(s.Index()) != 1 {
			return false
		}
		uses := false
		ast.Inspect(as.Rhs[0], func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			uses = uses || ok && info.Uses[id] == self
			return !uses
		})
		if uses || slices.ContainsFunc(sets, func(s set) bool { return s.field == sel.Sel.Name }) {
			return false
		}
		sets = append(sets, set{sel.Sel.Name, as.Rhs[0]})
	}
	f.self, f.sets = self, sets
	return true
}

// find folds every match of the pattern in the workspace, or with line
// only those that start on that line of the file in.
func (f *folder) find(in string, line int) error {
	f.by, f.adders, f.rest = map[string][]edit.TextEdit{}, map[string]*imports.Adder{}, map[ast.Expr]rest{}
	f.addressOf, f.made, f.runs = map[ast.Expr]bool{}, map[ast.Expr]bool{}, map[ast.Expr][]ast.Expr{}
	seen := map[token.Position]bool{}
	declFile := f.ws.Fset.Position(f.decl.Pos()).Filename
	for _, p := range f.ws.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, file := range p.Syntax {
			path := f.ws.Fset.Position(file.Pos()).Filename
			if in != "" && path != in {
				continue
			}
			var stack []ast.Node
			var err error
			ast.Inspect(file, func(n ast.Node) bool {
				if n == nil {
					stack = stack[:len(stack)-1]
					return false
				}
				if err != nil {
					return false
				}
				if fd, ok := n.(*ast.FuncDecl); ok && path == declFile && fd.Pos() == f.decl.Pos() {
					return false // fn itself
				}
				if list := stmtList(n); f.stmts != nil && list != nil {
					if err = f.foldStmts(p, file, list, append(stack, n), line); err != nil {
						return false
					}
				}
				x, isExpr := n.(ast.Expr)
				pos := f.ws.Fset.Position(n.Pos())
				if isExpr && f.stmts == nil && !seen[pos] && (line == 0 || pos.Line == line) {
					var folded bool
					if folded, err = f.try(p, file, x, stack); folded {
						seen[pos] = true
						return false
					}
				}
				stack = append(stack, n)
				return true
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// try folds x, an expression of file of package p inside the nodes stack
// (outermost first), if it matches the pattern and the call means the same.
func (f *folder) try(p *packages.Package, file *ast.File, x ast.Expr, stack []ast.Node) (bool, error) {
	info := p.TypesInfo
	if tv, ok := info.Types[x]; !ok || !tv.IsValue() || tv.Value != nil {
		return false, nil // a type, or a constant: nothing to fold
	}
	if f.sets != nil {
		return f.trySetter(p, file, x, stack)
	}
	a := f.m.At(info)
	if !a.Match(f.body, x) {
		f.keepNearest(a.Miss, 0)
		return false, nil
	}
	b := a.Bound
	fa := analysis.FuncAround(info, stack, f.ws.Effects())
	where := f.ws.Position(x.Pos())
	skip := func(why string) (bool, error) {
		f.skipped = append(f.skipped, where+": "+why)
		return false, nil
	}
	if len(stack) > 0 && assigned(stack[len(stack)-1], x) {
		return skip("it is assigned or its address taken")
	}
	if p.PkgPath != f.m.Pkg.PkgPath && !f.fn.Exported() {
		return skip(fmt.Sprintf("%s is not exported, so package %s cannot call it", f.fn.Name(), p.Name))
	}
	// Arguments are evaluated once, before the body: one that has effects
	// or could panic, or takes an address, moves there.
	for k, e := range b {
		if f.impure(fa, e) || f.addressOf[e] {
			if why := f.evaluatedFirst(fa, []ast.Node{x}, b, k); why != "" {
				return skip(why)
			}
		}
	}
	for _, e := range b {
		if why := f.staleRead(fa, []ast.Node{x}, e); why != "" {
			return skip(why)
		}
	}
	call, why, err := f.call(p, file, b, stack)
	if err != nil {
		return false, err
	}
	if why != "" {
		return skip(why)
	}
	f.replace(file, x, call)
	return true, nil
}

// replace records that x, an expression of file, becomes call.
func (f *folder) replace(file *ast.File, x ast.Expr, call string) {
	path := f.ws.Fset.Position(file.Pos()).Filename
	if f.by[path] == nil {
		f.order = append(f.order, path)
	}
	f.by[path] = append(f.by[path], edit.TextEdit{Start: f.ws.Fset.Position(x.Pos()).Offset, End: f.ws.Fset.Position(x.End()).Offset, NewText: call})
	f.n++
}

// trySetter folds x, an expression of file of package p inside stack, if
// it is a literal of the setter's struct giving the fields the setter
// sets as it sets them: the literal without them becomes the call's
// struct argument.
func (f *folder) trySetter(p *packages.Package, file *ast.File, x ast.Expr, stack []ast.Node) (bool, error) {
	info := p.TypesInfo
	lit, ok := x.(*ast.CompositeLit)
	if !ok || !pattern.SameObj(pattern.BaseName(info.TypeOf(lit)), pattern.BaseName(f.self.Type())) {
		return false, nil
	}
	a := f.m.At(info)
	b := a.Bound
	fa := analysis.FuncAround(info, stack, f.ws.Effects())
	b[pattern.HoleKey{Obj: f.self, Field: ""}] = lit
	var drop []int
	for _, s := range f.sets {
		i := slices.IndexFunc(lit.Elts, func(e ast.Expr) bool {
			kv, ok := e.(*ast.KeyValueExpr)
			return ok && pattern.IdentOf(kv.Key) != nil && pattern.IdentOf(kv.Key).Name == s.field
		})
		if i < 0 || !a.Match(s.value, lit.Elts[i].(*ast.KeyValueExpr).Value) {
			return false, nil
		}
		drop = append(drop, i)
	}
	where := f.ws.Position(x.Pos())
	skip := func(why string) (bool, error) {
		f.skipped = append(f.skipped, where+": "+why)
		return false, nil
	}
	if len(stack) > 0 && assigned(stack[len(stack)-1], x) {
		return skip("it has its address taken")
	}
	if p.PkgPath != f.m.Pkg.PkgPath && !f.fn.Exported() {
		return skip(fmt.Sprintf("%s is not exported, so package %s cannot call it", f.fn.Name(), p.Name))
	}
	typ, why := f.elided(p, lit, stack)
	if why != "" {
		return skip(why)
	}
	// The fields set are evaluated after the rest of the literal, in the
	// setter's order: each moves past the elements after it and the other
	// fields set.
	for _, i := range drop {
		var past []ast.Node
		for j, e := range lit.Elts {
			if j > i || j != i && slices.Contains(drop, j) {
				if kv, ok := e.(*ast.KeyValueExpr); ok {
					e = kv.Value
				}
				past = append(past, e)
			}
		}
		v := lit.Elts[i].(*ast.KeyValueExpr).Value
		if o := (analysis.Move{Effects: f.ws.Effects(), Func: fa, Expr: v, Past: past}).Check(); o != nil {
			return skip("the fields set would be evaluated in another order: " + o.Why)
		}
	}
	for k, e := range b {
		if n := f.count(k); k.Obj != f.self && n > 1 {
			if f.sharedMove(fa, e, []ast.Node{lit}).Check() != nil {
				return skip(fmt.Sprintf("%s, used %d times in the body, would be evaluated once", types.ExprString(e), n))
			}
		}
	}
	f.rest[lit] = rest{drop, typ}
	call, why, err := f.call(p, file, b, stack)
	if err != nil {
		return false, err
	}
	if why != "" {
		return skip(why)
	}
	f.replace(file, x, call)
	return true, nil
}

// elided is the type to write for lit, of package p inside stack, when
// an enclosing literal elides it (call qualifies it), or ""; or why it
// cannot be written.
func (f *folder) elided(p *packages.Package, lit *ast.CompositeLit, stack []ast.Node) (string, string) {
	if lit.Type != nil {
		return "", ""
	}
	var outer ast.Expr
	key := false
	switch parent := stack[len(stack)-1].(type) {
	case *ast.CompositeLit:
		outer = parent
	case *ast.KeyValueExpr:
		outer, key = stack[len(stack)-2].(ast.Expr), parent.Key == lit
	}
	t := types.Unalias(p.TypesInfo.TypeOf(outer))
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	var slot types.Type
	switch u := t.Underlying().(type) {
	case *types.Slice:
		slot = u.Elem()
	case *types.Array:
		slot = u.Elem()
	case *types.Map:
		slot = u.Elem()
		if key {
			slot = u.Key()
		}
	}
	if _, ptr := slot.(*types.Pointer); ptr {
		return "", "its & is elided in the enclosing literal"
	}
	if tn := pattern.BaseName(f.self.Type()); tn.Pkg().Path() != f.fn.Pkg().Path() {
		return "", fmt.Sprintf("its type is elided in the enclosing literal, and %s is declared in another package than %s", tn.Name(), f.fn.Name())
	}
	return pattern.BaseName(f.self.Type()).Name(), ""
}

// literal is lit's source in src, at offsets off, as r says to write it.
func (r rest) literal(lit *ast.CompositeLit, src []byte, off func(token.Pos) int) string {
	kept := -1 // the last element kept
	for i := range lit.Elts {
		if !slices.Contains(r.drop, i) {
			kept = i
		}
	}
	if kept < 0 {
		return r.typ + string(src[off(lit.Pos()):off(lit.Lbrace)]) + "{}"
	}
	// Cut each element dropped up to the next one; a run of them at the
	// end, from the end of the last element kept.
	var cuts [][2]int
	for _, i := range r.drop {
		if i < kept {
			cuts = append(cuts, [2]int{off(lit.Elts[i].Pos()), off(lit.Elts[i+1].Pos())})
		}
	}
	if kept < len(lit.Elts)-1 {
		cuts = append(cuts, [2]int{off(lit.Elts[kept].End()), off(lit.Elts[len(lit.Elts)-1].End())})
	}
	slices.SortFunc(cuts, func(a, b [2]int) int { return a[0] - b[0] })
	var sb strings.Builder
	sb.WriteString(r.typ)
	at := off(lit.Pos())
	for _, c := range cuts {
		sb.Write(src[at:c[0]])
		at = c[1]
	}
	sb.Write(src[at:off(lit.End())])
	return sb.String()
}

// call is the call of fn that a match with holes b in file of package p,
// inside stack, becomes; or why there is none.
func (f *folder) call(p *packages.Package, file *ast.File, b map[pattern.HoleKey]ast.Expr, stack []ast.Node) (string, string, error) {
	path := f.ws.Fset.Position(file.Pos()).Filename
	src, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	var text func(e ast.Expr) string
	text = func(e ast.Expr) string {
		if run, ok := f.runs[e]; ok {
			var parts []string
			for _, y := range run {
				parts = append(parts, text(y))
			}
			return strings.Join(parts, "+")
		}
		if l, ok := e.(*ast.BasicLit); ok && f.made[e] {
			return l.Value
		}
		if u, ok := e.(*ast.UnaryExpr); ok && f.addressOf[e] {
			return "&" + text(u.X)
		}
		if r, ok := f.rest[e]; ok {
			return r.literal(e.(*ast.CompositeLit), src, func(p token.Pos) int { return f.ws.Fset.Position(p).Offset })
		}
		return string(src[f.ws.Fset.Position(e.Pos()).Offset:f.ws.Fset.Position(e.End()).Offset])
	}
	// qualify writes name, of fn's package, for code in file.
	qualify := func(name string) (string, error) {
		if p.PkgPath == f.m.Pkg.PkgPath {
			return name, nil
		}
		if f.adders[path] == nil {
			f.adders[path] = imports.New(f.ws, p, file, src)
		}
		q, err := f.adders[path].Name(f.fn.Pkg())
		return q + "." + name, err
	}
	if lit := b[pattern.HoleKey{Obj: f.self, Field: ""}]; lit != nil && f.rest[lit].typ != "" {
		r := f.rest[lit]
		if r.typ, err = qualify(r.typ); err != nil {
			return "", err.Error(), nil
		}
		f.rest[lit] = r
	}
	var args []string
	for _, fl := range f.decl.Type.Params.List {
		for _, n := range fl.Names {
			o := f.m.Pkg.TypesInfo.Defs[n]
			fs, byField := f.m.Fields[o]
			if !byField {
				args = append(args, text(b[pattern.HoleKey{Obj: o, Field: ""}]))
				continue
			}
			t, err := qualify(o.Type().(*types.Named).Obj().Name())
			if err != nil {
				return "", err.Error(), nil
			}
			var elts []string
			for _, fld := range fs {
				elts = append(elts, fld+": "+text(b[pattern.HoleKey{Obj: o, Field: fld}]))
			}
			args = append(args, t+"{"+strings.Join(elts, ", ")+"}")
		}
	}
	call := f.fn.Name() + "(" + strings.Join(args, ", ") + ")"
	if f.fn.Signature().Recv() != nil {
		recv, why := f.receiver(p.TypesInfo, b, stack, text)
		if why != "" {
			return "", why, nil
		}
		return recv + "." + call, "", nil
	}
	if call, err = qualify(call); err != nil {
		return "", err.Error(), nil
	}
	return call, "", nil
}

// receiver is the receiver of the call: what matched the body's receiver,
// or, when the body does not use it, the receiver of the method of the
// same type the occurrence is in; or why there is none.
func (f *folder) receiver(info *types.Info, b map[pattern.HoleKey]ast.Expr, stack []ast.Node, text func(ast.Expr) string) (string, string) {
	recvT := f.fn.Signature().Recv().Type()
	_, ptr := recvT.(*types.Pointer)
	if e, ok := b[pattern.HoleKey{Obj: f.m.Recv, Field: ""}]; ok {
		_, isPtr := info.TypeOf(e).(*types.Pointer)
		switch ast.Unparen(e).(type) {
		case *ast.Ident, *ast.SelectorExpr, *ast.IndexExpr, *ast.CompositeLit:
			return text(e), ""
		case *ast.CallExpr:
			if ptr && !isPtr {
				return "", types.ExprString(e) + " cannot have its address taken for a pointer receiver"
			}
			return text(e), ""
		}
		if ptr && !isPtr {
			return "", types.ExprString(e) + " cannot have its address taken for a pointer receiver"
		}
		return "(" + text(e) + ")", ""
	}
	base := pattern.BaseName(recvT)
	for i := len(stack) - 1; i >= 0; i-- {
		fd, ok := stack[i].(*ast.FuncDecl)
		if !ok || fd.Recv == nil || len(fd.Recv.List[0].Names) != 1 {
			continue
		}
		r := info.Defs[fd.Recv.List[0].Names[0]]
		if r != nil && base != nil && pattern.SameObj(pattern.BaseName(r.Type()), base) && fd.Recv.List[0].Names[0].Name != "_" {
			return fd.Recv.List[0].Names[0].Name, ""
		}
	}
	return "", fmt.Sprintf("it is not in a method of %s, whose receiver the call needs", base.Name())
}

// edits are the folds, with the imports they need, by file.
func (f *folder) edits() []edit.FileEdit {
	var out []edit.FileEdit
	for _, path := range f.order {
		es := f.by[path]
		f.removeImports(path, es)
		if a := f.adders[path]; a != nil {
			es = append(es, a.Edits()...)
		}
		out = append(out, edit.FileEdit{Path: path, Edits: es})
	}
	return out
}

// removeImports removes the imports of the file at path that only the
// code its folds (es) replace used, and that the calls do not name.
func (f *folder) removeImports(path string, es []edit.TextEdit) {
	p, file := f.ws.FileSyntax(path)
	if file == nil {
		return
	}
	var calls []string
	for _, e := range es {
		calls = append(calls, e.NewText)
	}
	code, err := gocode.ParseStatements(strings.Join(calls, "\n"))
	if err != nil {
		return
	}
	if f.adders[path] == nil {
		src, err := os.ReadFile(path)
		if err != nil {
			return
		}
		f.adders[path] = imports.New(f.ws, p, file, src)
	}
	f.removed = append(f.removed, gocode.RemoveImports(p, file, f.adders[path], func(pos token.Pos) bool {
		off := f.ws.Fset.Position(pos).Offset
		return slices.ContainsFunc(es, func(e edit.TextEdit) bool { return e.Start <= off && off < e.End })
	}, code)...)
}

// matchAddress reports whether x, which is not a dereference, is the
// pattern's *p for a pointer parameter p: an addressable expression of
// p's element type, so that the call passes &x.
func matchAddress(a *pattern.Attempt, pp *ast.StarExpr, x ast.Expr, rel *relaxer) bool {
	info, b := a.Info, a.Bound
	id, ok := ast.Unparen(pp.X).(*ast.Ident)
	if !ok {
		return false
	}
	po := a.Pkg.TypesInfo.Uses[id]
	if _, hole := a.Holes[po]; !hole || po == nil || po == a.Recv {
		return false
	}
	ptr, ok := po.Type().Underlying().(*types.Pointer)
	xt := info.TypeOf(x)
	if !ok || xt == nil || !addressable(info, x) || !types.Identical(xt, ptr.Elem()) && types.TypeString(xt, nil) != types.TypeString(ptr.Elem(), nil) {
		return false
	}
	u := &ast.UnaryExpr{OpPos: x.Pos(), Op: token.AND, X: x}
	k := pattern.HoleKey{Obj: po, Field: ""}
	if prev, ok := b[k]; ok {
		return types.ExprString(prev) == types.ExprString(u)
	}
	b[k] = u
	rel.addressOf[u] = true
	return true
}

// addressable reports whether x can have its address taken in place: a
// variable, or a field of one or through a pointer.
func addressable(info *types.Info, x ast.Expr) bool {
	switch x := ast.Unparen(x).(type) {
	case *ast.Ident:
		_, ok := info.Uses[x].(*types.Var)
		return ok
	case *ast.SelectorExpr:
		s := info.Selections[x]
		if s == nil || s.Kind() != types.FieldVal {
			return false
		}
		return s.Indirect() || addressable(info, x.X)
	}
	return false
}

// matchFormat reports whether xc, a call of a printf-style function, is
// the pattern's call pp with text of xc's constant format in place of
// verbs %s or %v of pp whose arguments are string parameters, which
// print them as they are: log.Printf("email to %s", to) is
// log.Printf("%s to %s", channel, to) with channel "email". Text with a
// % in it is not taken.
func matchFormat(a *pattern.Attempt, pp, xc *ast.CallExpr, rel *relaxer) bool {
	info, b := a.Info, a.Bound
	pinfo := a.Pkg.TypesInfo
	fi := formatIndex(pinfo, pp)
	if fi < 0 || pp.Ellipsis.IsValid() || len(xc.Args) <= fi || len(xc.Args) > len(pp.Args) {
		return false
	}
	for i := 0; i < fi; i++ {
		if !a.Match(pp.Args[i], xc.Args[i]) {
			return false
		}
	}
	pv, xv := pinfo.Types[pp.Args[fi]].Value, info.Types[xc.Args[fi]].Value
	if pv == nil || xv == nil || pv.Kind() != constant.String || xv.Kind() != constant.String {
		return false
	}
	parts := formatParts(constant.StringVal(pv))
	if parts == nil {
		return false
	}
	// try matches parts[k:] to rest, the format's text still to match,
	// and the arguments from xi; it returns the holes, or nil.
	var try func(k int, rest string, xi int, b map[pattern.HoleKey]ast.Expr) map[pattern.HoleKey]ast.Expr
	try = func(k int, rest string, xi int, b map[pattern.HoleKey]ast.Expr) map[pattern.HoleKey]ast.Expr {
		if k == len(parts) {
			if rest == "" && xi == len(xc.Args) {
				return b
			}
			return nil
		}
		part := parts[k]
		if k%2 == 0 {
			if !strings.HasPrefix(rest, part) {
				return nil
			}
			return try(k+1, rest[len(part):], xi, b)
		}
		arg := pp.Args[fi+(k+1)/2]
		if strings.HasPrefix(rest, part) && xi < len(xc.Args) {
			c := maps.Clone(b)
			if matchIn(a, c, arg, xc.Args[xi]) {
				if r := try(k+1, rest[len(part):], xi+1, c); r != nil {
					return r
				}
			}
		}
		if part != "%s" && part != "%v" {
			return nil
		}
		for j := 0; j <= len(rest) && !strings.Contains(rest[:j], "%"); j++ {
			c := maps.Clone(b)
			if bindText(a, arg, rest[:j], xc.Args[fi].Pos(), c, rel) {
				if r := try(k+1, rest[j:], xi, c); r != nil {
					return r
				}
			}
		}
		return nil
	}
	r := try(0, constant.StringVal(xv), fi+1, b)
	if r == nil {
		return false
	}
	maps.Copy(b, r)
	return true
}

// bindText binds the pattern's arg, if it is a parameter of type string,
// to a literal of text in b; it reports whether it could.
func bindText(a *pattern.Attempt, arg ast.Expr, text string, pos token.Pos, b map[pattern.HoleKey]ast.Expr, rel *relaxer) bool {
	po := stringParam(a, arg)
	if po == nil {
		return false
	}
	k := pattern.HoleKey{Obj: po, Field: ""}
	if prev, ok := b[k]; ok {
		l, ok := prev.(*ast.BasicLit)
		if !ok || l.Kind != token.STRING {
			return false
		}
		s, err := strconv.Unquote(l.Value)
		return err == nil && s == text
	}
	lit := &ast.BasicLit{ValuePos: pos, Kind: token.STRING, Value: strconv.Quote(text)}
	b[k] = lit
	rel.made[lit] = true
	return true
}

// stringParam is the parameter of type string that arg, of the pattern,
// is, or nil.
func stringParam(a *pattern.Attempt, arg ast.Expr) types.Object {
	id, ok := ast.Unparen(arg).(*ast.Ident)
	if !ok {
		return nil
	}
	po := a.Pkg.TypesInfo.Uses[id]
	if _, hole := a.Holes[po]; !hole || po == nil || po == a.Recv || !types.Identical(po.Type(), types.Typ[types.String]) {
		return nil
	}
	return po
}

// matchConcat reports whether x, a string concatenation, is the
// pattern's concatenation pp, + being associative: each operand of pp
// matches one of x's in order, but a string parameter may match a run of
// them, so to+": "+text is first+" "+last+": "+ref with to first+" "+last.
func matchConcat(a *pattern.Attempt, pp, x *ast.BinaryExpr, rel *relaxer) bool {
	info, b := a.Info, a.Bound
	ps, xs := concatOperands(a.Pkg.TypesInfo, pp), concatOperands(info, x)
	// try matches ps[i:] to xs[j:]; it returns the holes, or nil.
	var try func(i, j int, b map[pattern.HoleKey]ast.Expr) map[pattern.HoleKey]ast.Expr
	try = func(i, j int, b map[pattern.HoleKey]ast.Expr) map[pattern.HoleKey]ast.Expr {
		if i == len(ps) || j == len(xs) {
			if i == len(ps) && j == len(xs) {
				return b
			}
			return nil
		}
		c := maps.Clone(b)
		if matchIn(a, c, ps[i], xs[j]) {
			if r := try(i+1, j+1, c); r != nil {
				return r
			}
		}
		po := stringParam(a, ps[i])
		for k := j + 2; po != nil && k <= len(xs)-(len(ps)-i-1); k++ {
			c := maps.Clone(b)
			if bindRun(po, xs[j:k], c, rel) {
				if r := try(i+1, k, c); r != nil {
					return r
				}
			}
		}
		return nil
	}
	r := try(0, 0, b)
	if r == nil {
		return false
	}
	maps.Copy(b, r)
	return true
}

// bindRun binds the string parameter po to the concatenation of run in
// b; it reports whether it could.
func bindRun(po types.Object, run []ast.Expr, b map[pattern.HoleKey]ast.Expr, rel *relaxer) bool {
	e := run[0]
	for _, y := range run[1:] {
		e = &ast.BinaryExpr{X: e, OpPos: y.Pos(), Op: token.ADD, Y: y}
	}
	k := pattern.HoleKey{Obj: po, Field: ""}
	if prev, ok := b[k]; ok {
		return types.ExprString(prev) == types.ExprString(e)
	}
	b[k] = e
	rel.runs[e] = run
	return true
}

// matchIn reports whether x matches the pattern's p with the holes bound
// as in b, which the match adds to; a's own bindings stay as they were.
func matchIn(a *pattern.Attempt, b map[pattern.HoleKey]ast.Expr, p, x ast.Expr) bool {
	saved := a.Bound
	a.Bound = b
	defer func() { a.Bound = saved }()
	return a.Match(p, x)
}

// concatOperands lists the operands of e as a string concatenation,
// however it is grouped: a+(b+c) is a, b, c.
func concatOperands(info *types.Info, e ast.Expr) []ast.Expr {
	if bin, ok := ast.Unparen(e).(*ast.BinaryExpr); ok && bin.Op == token.ADD && analysis.IsString(info.TypeOf(bin)) {
		return append(concatOperands(info, bin.X), concatOperands(info, bin.Y)...)
	}
	return []ast.Expr{e}
}

// impure reports whether evaluating e could do more than compute a
// value: a call in it has side effects, reads what other code can
// change, or could panic or not finish; or e could panic itself.
func (f *folder) impure(fa *analysis.Func, e ast.Expr) bool {
	if f.ws.Effects().Of(fa, e).At.OtherPanic != nil {
		return true
	}
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			s := f.ws.Effects().Call(fa.Info, n)
			found = s.Effect != "" || s.Reads != "" || s.Unsafe != ""
		case *ast.UnaryExpr:
			found = n.Op == token.ARROW
		}
		return !found
	})
	return found
}

// evaluatedFirst is why the argument for the hole k, which could panic
// or has effects, could not be evaluated once before roots, the code the
// call replaces, or "": a move of it past what roots evaluate before its
// first use, one value for each use the body makes of it; and each
// argument the call evaluates before it that roots evaluated after it,
// a move of that argument past it.
func (f *folder) evaluatedFirst(fa *analysis.Func, roots []ast.Node, b map[pattern.HoleKey]ast.Expr, k pattern.HoleKey) string {
	e := b[k]
	text := types.ExprString(e)
	var past []ast.Node
	for _, r := range roots {
		if r.Pos() >= e.Pos() {
			break
		}
		past = append(past, analysis.EvaluatedBefore(r, e.Pos(), nil)...)
	}
	if o := (analysis.Move{Effects: f.ws.Effects(), Func: fa, Expr: e, Past: past}).Check(); o != nil {
		return text + " would be evaluated before the code it is in: " + o.Why
	}
	if n := f.count(k); n > 1 { // one value for each use the body makes of it
		if o := f.sharedMove(fa, e, roots).Check(); o != nil {
			if o.Past == nil {
				return fmt.Sprintf("%s, used %d times in the body, would be evaluated once", text, n)
			}
			return fmt.Sprintf("%s, used %d times in the body, would be evaluated once, but %s %s between its uses", text, n, types.ExprString(o.Past.(ast.Expr)), o.PastDoes)
		}
	}
	before := func(a ast.Expr) string {
		if a == nil {
			return text + " would be evaluated after an argument the code it is in does not have"
		}
		if a.Pos() < e.Pos() || f.addressOf[a] {
			return "" // an address is judged on its own: movedAddress, or its own move
		}
		if o := (analysis.Move{Effects: f.ws.Effects(), Func: fa, Expr: a, Past: []ast.Node{e}}).Check(); o != nil {
			return types.ExprString(a) + " would be evaluated before " + text + ", which it now follows: " + o.Why
		}
		return ""
	}
	if f.m.Recv != nil && k.Obj != f.m.Recv {
		if a := b[pattern.HoleKey{Obj: f.m.Recv, Field: ""}]; a != nil {
			if why := before(a); why != "" {
				return why
			}
		}
	}
	for _, fl := range f.decl.Type.Params.List {
		for _, n := range fl.Names {
			o := f.m.Pkg.TypesInfo.Defs[n]
			keys := []pattern.HoleKey{{Obj: o, Field: ""}}
			if fs, byField := f.m.Fields[o]; byField {
				keys = nil
				for _, fld := range fs {
					keys = append(keys, pattern.HoleKey{Obj: o, Field: fld})
				}
			}
			for _, ok := range keys {
				if ok == k {
					return ""
				}
				if why := before(b[ok]); why != "" {
					return why
				}
			}
		}
	}
	return ""
}

// sharedMove is the move that makes e one value for each use the body
// makes of it: what e reads must not change in roots, the code the call
// replaces (analysis.Move.Between), unless roots hold a function literal,
// which may run later; then e may read nothing that can change.
func (f *folder) sharedMove(fa *analysis.Func, e ast.Expr, roots []ast.Node) analysis.Move {
	m := analysis.Move{Effects: f.ws.Effects(), Func: fa, Expr: e, Times: analysis.Shared}
	for _, r := range roots {
		lit := false
		ast.Inspect(r, func(n ast.Node) bool {
			_, ok := n.(*ast.FuncLit)
			lit = lit || ok
			return !lit
		})
		if lit {
			return m
		}
	}
	m.Past, m.Between = roots, true
	return m
}

// staleRead is why e, an argument that may read memory other code can
// change (a field through a pointer, an element, a package variable),
// would not read what the code roots, which it replaces, read where it
// read it, or "": a move of it past what roots evaluate before its last
// use, since one value now stands for every use. An argument with
// effects or that could panic is judged by evaluatedFirst instead.
func (f *folder) staleRead(fa *analysis.Func, roots []ast.Node, e ast.Expr) string {
	if f.addressOf[e] || f.made[e] || f.impure(fa, e) {
		return ""
	}
	text := types.ExprString(e)
	last := roots[len(roots)-1].End()
	if _, run := f.runs[e]; !run {
		last = token.NoPos
		for _, r := range roots {
			ast.Inspect(r, func(n ast.Node) bool {
				if x, ok := n.(ast.Expr); ok && x.End() > last && types.ExprString(x) == text {
					last = x.End()
				}
				return true
			})
		}
	}
	var past []ast.Node
	for _, r := range roots {
		past = append(past, analysis.EvaluatedBefore(r, last, nil)...)
	}
	o := (analysis.Move{Effects: f.ws.Effects(), Func: fa, Expr: e, Past: past}).Check()
	switch {
	case o == nil:
		return ""
	case o.Kind == analysis.Reads && o.Past != nil:
		return fmt.Sprintf("%s would be read before the code it is in, and code before its last use could change it before then (it %s, at %s)", text, o.PastDoes, f.ws.Position(o.Past.Pos()))
	}
	return text + " would be read before the code it is in: " + o.Why
}

// stmtNodes is list as nodes.
func stmtNodes(list []ast.Stmt) []ast.Node {
	var out []ast.Node
	for _, s := range list {
		out = append(out, s)
	}
	return out
}

// formatIndex is the index of the format argument of call, a call of a
// printf-style function (its name ends in f, and its last parameters
// are a format string and ...any), or -1.
func formatIndex(info *types.Info, call *ast.CallExpr) int {
	fn, ok := info.Uses[analysis.IdentOf(call.Fun)].(*types.Func)
	if !ok || !strings.HasSuffix(fn.Name(), "f") {
		return -1
	}
	sig := fn.Signature()
	n := sig.Params().Len()
	if !sig.Variadic() || n < 2 || !types.Identical(sig.Params().At(n-2).Type(), types.Typ[types.String]) {
		return -1
	}
	last, ok := sig.Params().At(n - 1).Type().(*types.Slice)
	if !ok {
		return -1
	}
	if it, ok := last.Elem().Underlying().(*types.Interface); !ok || !it.Empty() {
		return -1
	}
	return n - 2
}

// formatParts splits a printf format into its text and verbs, text first
// and last: "%s to %s" is "", "%s", " to ", "%s", "". It returns nil
// for a verb that takes its width or precision from an argument (*) or
// names its argument ([1]).
func formatParts(s string) []string {
	parts := []string{""}
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			parts[len(parts)-1] += s[i : i+1]
			continue
		}
		if i+1 < len(s) && s[i+1] == '%' {
			parts[len(parts)-1] += "%%"
			i++
			continue
		}
		j := i + 1
		for j < len(s) && strings.IndexByte("+-# 0123456789.", s[j]) >= 0 {
			j++
		}
		if j >= len(s) || s[j] == '*' || s[j] == '[' {
			return nil
		}
		_, size := utf8.DecodeRuneInString(s[j:])
		parts = append(parts, s[i:j+size], "")
		i = j + size - 1
	}
	return parts
}

// movedAddress is why an argument &x for a *p of the body, evaluated
// before the statements seq, would not do what they did, or "". Taking
// x's address can panic only when a pointer it goes through is nil. That
// panic may come before the code that ran before x's first use when that
// code returns and panics no other way, and the pointer is a local
// variable that nothing else changes.
func (f *folder) movedAddress(info *types.Info, seq []ast.Stmt, b map[pattern.HoleKey]ast.Expr, stack []ast.Node) string {
	for _, e := range b {
		u, ok := e.(*ast.UnaryExpr)
		if !ok || !f.addressOf[e] {
			continue
		}
		for _, p := range analysis.Addressed(info, u) {
			if p == nil {
				continue
			}
			if why := pointerStays(info, p, seq, stack); why != "" {
				return types.ExprString(u) + " " + why
			}
			for _, s := range seq {
				if s.Pos() > u.X.Pos() {
					break
				}
				if why := f.mayStop(info, s, u.X.Pos()); why != "" {
					return fmt.Sprintf("%s could panic on a nil %s, and would be evaluated before the statement at %s, which %s", types.ExprString(u), types.ExprString(p), f.ws.Position(s.Pos()), why)
				}
			}
		}
	}
	return ""
}

// pointerStays is why the pointer p, read before the statements seq of
// the function at the top of stack, might not be what they read, or "".
func pointerStays(info *types.Info, p ast.Expr, seq []ast.Stmt, stack []ast.Node) string {
	id, ok := ast.Unparen(p).(*ast.Ident)
	if !ok {
		return "reads " + types.ExprString(p) + " before the statements, which could change it"
	}
	v, ok := info.Uses[id].(*types.Var)
	if !ok || v.Pkg() == nil || v.Parent() == v.Pkg().Scope() {
		return "reads the package variable " + id.Name + " before the statements, which could change it"
	}
	for i := len(stack) - 1; i >= 0; i-- {
		switch fn := stack[i].(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			fa := analysis.AnalyzeFunc(info, fn, nil)
			if fa.Addressed[v] || fa.Captured[v] {
				return "reads " + v.Name() + ", which a closure or pointer can change"
			}
			for _, n := range fa.Modified[v] {
				if seq[0].Pos() <= n.Pos() && n.Pos() < seq[len(seq)-1].End() {
					return "reads " + v.Name() + " before the statements, which assign it"
				}
			}
			return ""
		}
	}
	return ""
}

// mayStop is how the code of the statement s before pos might not finish
// or return, or "". Code that could panic is not counted: a nil
// dereference moved ahead of it panics either way, only with another
// message.
func (f *folder) mayStop(info *types.Info, s ast.Stmt, pos token.Pos) string {
	why := ""
	ast.Inspect(s, func(n ast.Node) bool {
		if _, lit := n.(*ast.FuncLit); lit || n == nil || why != "" || n.Pos() >= pos {
			return false
		}
		switch n := n.(type) {
		case *ast.SendStmt, *ast.SelectStmt:
			why = "uses a channel, which might block"
		case *ast.UnaryExpr:
			if n.Op == token.ARROW {
				why = "receives from a channel, which might block"
			}
		case *ast.RangeStmt:
			switch info.TypeOf(n.X).Underlying().(type) {
			case *types.Chan, *types.Signature:
				why = "ranges over a channel or function, which might not finish"
			}
		}
		e, ok := n.(ast.Expr)
		if why != "" || !ok || e.End() > pos {
			return why == ""
		}
		if c, ok := e.(*ast.CallExpr); ok {
			why = f.ws.Effects().Call(info, c).Stops
		}
		return why == ""
	})
	return why
}

// foldStmts folds the runs of list, statements of a block inside stack,
// that match the pattern's statements; with line, only one starting on
// that line.
func (f *folder) foldStmts(p *packages.Package, file *ast.File, list []ast.Stmt, stack []ast.Node, line int) error {
	info := p.TypesInfo
	n := len(f.stmts)
	for i := 0; i+n <= len(list); i++ {
		pos := f.ws.Fset.Position(list[i].Pos())
		if f.inDone(list[i].Pos()) || line != 0 && pos.Line != line {
			continue
		}
		a := f.m.At(info)
		a.Locals = map[types.Object]types.Object{}
		matched := true
		for k := range f.stmts {
			if !a.MatchStmt(f.stmts[k], list[i+k]) {
				f.keepNearest(a.Miss, k)
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		b := a.Bound
		fa := analysis.FuncAround(info, stack, f.ws.Effects())
		seq := list[i : i+n]
		where := f.ws.Position(seq[0].Pos())
		skip := ""
		for k, e := range b {
			if skip == "" && f.impure(fa, e) {
				skip = f.evaluatedFirst(fa, stmtNodes(seq), b, k)
			}
		}
		if skip == "" {
			if v := usedAfter(info, seq, stack); v != "" {
				skip = "they declare " + v + ", which is used after them"
			}
		}
		if skip == "" {
			skip = f.movedAddress(info, seq, b, stack)
		}
		for _, e := range b {
			if skip == "" {
				skip = f.staleRead(fa, stmtNodes(seq), e)
			}
		}
		call := ""
		if skip == "" {
			var err error
			if call, skip, err = f.call(p, file, b, stack); err != nil {
				return err
			}
		}
		if skip != "" {
			f.skipped = append(f.skipped, where+": "+skip)
			continue
		}
		path := f.ws.Fset.Position(file.Pos()).Filename
		if f.by[path] == nil {
			f.order = append(f.order, path)
		}
		f.by[path] = append(f.by[path], edit.TextEdit{Start: f.ws.Fset.Position(seq[0].Pos()).Offset, End: f.ws.Fset.Position(seq[n-1].End()).Offset, NewText: call})
		f.done = append(f.done, [2]token.Pos{seq[0].Pos(), seq[n-1].End()})
		f.n++
		i += n - 1
	}
	return nil
}

// keepNearest keeps miss, an attempt's, as the nearest if it got further
// than any before: past k of the pattern's statements, then deeper. A
// miss at the top of the pattern is no near miss.
func (f *folder) keepNearest(miss *pattern.Miss, k int) {
	if miss == nil {
		return
	}
	n := *miss
	n.Depth += k
	if n.Depth > 0 && (f.nearest == nil || n.Depth > f.nearest.Depth) {
		f.nearest = &n
	}
}

// short is n's source on one line: its first, with " ..." if there are more.
func short(n ast.Node) string {
	var b strings.Builder
	printer.Fprint(&b, token.NewFileSet(), n)
	s, _, more := strings.Cut(b.String(), "\n")
	if more {
		s += " ..."
	}
	return s
}

// inDone reports whether pos is in statements already folded.
func (f *folder) inDone(pos token.Pos) bool {
	for _, d := range f.done {
		if d[0] <= pos && pos < d[1] {
			return true
		}
	}
	return false
}

// usedAfter is the name of a variable seq declares at its top level that
// the function it is in uses after it, or "".
func usedAfter(info *types.Info, seq []ast.Stmt, stack []ast.Node) string {
	declared := map[types.Object]bool{}
	for _, s := range seq {
		switch s := s.(type) {
		case *ast.AssignStmt:
			for _, l := range s.Lhs {
				if id := pattern.IdentOf(l); id != nil && info.Defs[id] != nil {
					declared[info.Defs[id]] = true
				}
			}
		case *ast.DeclStmt:
			ast.Inspect(s, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && info.Defs[id] != nil {
					declared[info.Defs[id]] = true
				}
				return true
			})
		}
	}
	end := seq[len(seq)-1].End()
	var fn ast.Node
	for i := len(stack) - 1; i >= 0 && fn == nil; i-- {
		switch stack[i].(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			fn = stack[i]
		}
	}
	name := ""
	if fn != nil {
		ast.Inspect(fn, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Pos() >= end && declared[info.Uses[id]] && name == "" {
				name = id.Name
			}
			return name == ""
		})
	}
	return name
}

// stmtList is the statements a block, case or select clause holds, or
// nil.
func stmtList(n ast.Node) []ast.Stmt {
	switch n := n.(type) {
	case *ast.BlockStmt:
		return n.List
	case *ast.CaseClause:
		return n.Body
	case *ast.CommClause:
		return n.Body
	}
	return nil
}

// selX is sel's operand, or nil for a nil sel.
func selX(sel *ast.SelectorExpr) ast.Expr {
	if sel == nil {
		return nil
	}
	return sel.X
}

// fieldOf reports whether e is a field of the variable o.
func fieldOf(info *types.Info, e ast.Expr, o types.Object) bool {
	sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
	return ok && info.Uses[pattern.IdentOf(selX(sel))] == o
}

// assigned reports whether x is written by parent: the left of an
// assignment, ++ or --, or the operand of &.
func assigned(parent ast.Node, x ast.Expr) bool {
	switch p := parent.(type) {
	case *ast.AssignStmt:
		return slices.Contains(p.Lhs, x)
	case *ast.IncDecStmt:
		return p.X == x
	case *ast.UnaryExpr:
		return p.Op == token.AND && p.X == x
	case *ast.RangeStmt:
		return p.Key == x || p.Value == x
	}
	return false
}

// limit keeps the first n of s, noting how many more there are.
func limit(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return append(s[:n:n], fmt.Sprintf("and %d more", len(s)-n))
}

// Plan checks preconditions and computes the edits.
func (*Fold) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a foldArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	pkg, id, err := locate.Ident(ws, locate.IdentQuery{File: a.File, Name: a.Name})
	if err != nil {
		return nil, err
	}
	fn, ok := pkg.TypesInfo.Defs[id].(*types.Func)
	if !ok {
		return nil, refactor.Refusef("%s is not a function or method", a.Name)
	}
	f := &folder{ws: ws, m: pattern.Matcher{Pkg: pkg, Holes: map[types.Object]int{}}, fn: fn}
	f.m.Relax = &f.relaxer
	if err := f.pattern(id); err != nil {
		return nil, err
	}
	in := ""
	if a.Line > 0 {
		in = ws.Abs(a.File)
		if a.In != "" {
			in = ws.Abs(a.In)
		}
	}
	if err := f.find(in, a.Line); err != nil {
		return nil, err
	}
	if f.n == 0 {
		why := ""
		if len(f.skipped) > 0 {
			why = "; what matched was left: " + strings.Join(limit(f.skipped, 5), "; ")
		}
		if f.nearest != nil {
			why = fmt.Sprintf("; the nearest, %s, has %s where the body has %s", ws.Position(f.nearest.X.Pos()), short(f.nearest.X), short(f.nearest.P)) + why
		}
		return nil, refactor.Refusef("nothing in the module matches the body of %s (%s)%s", fn.Name(), f.shape(), why)
	}
	plan := &refactor.Plan{
		Summary: fmt.Sprintf("folded %d occurrence%s of %s into calls of %s", f.n, textutil.Plural(f.n), f.shape(), fn.Name()),
		Edits:   f.edits(),
	}
	if len(f.skipped) > 0 {
		plan.Notes = append(plan.Notes, "left as they are: "+strings.Join(limit(f.skipped, 5), "; "))
	}
	if len(f.removed) > 0 {
		plan.Notes = append(plan.Notes, gocode.ImportNote("removed", f.removed)+", which only the folded code used")
	}
	return plan, nil
}
