// Package fold implements "fold": replace code that does what an
// existing function does with calls to it.
package fold

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/constant"
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
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
	"golang.org/x/tools/go/packages"
)

// Fold is the "fold" refactoring.
type Fold struct{}

// New returns the fold refactoring.
func New() *Fold { return &Fold{} }

func (*Fold) Name() string { return "fold" }

func (*Fold) Description() string {
	return `Replace code that does what an existing function does with calls to it: every match of the function's body in the module, its parameters standing for any expressions of their types, becomes a call. The function stays. extract_function makes a function from one place; fold reuses one, written or extracted, everywhere else.
What a body matches:
- One return: u.Email becomes u.email() for func (u User) email() string { return u.Email }.
- A struct parameter used only by its fields: Send(p, us, m) becomes Send2(p, Batch{Users: us, Message: m}) for func Send2(p string, b Batch) string { return Send(p, b.Users, b.Message) }, to move callers to a parameter object.
- A setter, which sets fields of a struct parameter and returns it: User{ID: id, Email: v} becomes withEmail(User{ID: id}, v).
- Statements, for a function without results: the same statements in any block become one call, so near-duplicate methods become calls of one function you declare. A *p matches a field, a %s of a format matches constant text, and a string parameter matches part of a + concatenation: in func (e *Email) Send(to, subject string) { log.Printf("email to %s", to); e.Sent = append(e.Sent, to+": "+subject+"!") } the two statements become record(&e.Sent, "email", to, subject+"!") for func record(sent *[]string, channel, to, text string) { log.Printf("%s to %s", channel, to); *sent = append(*sent, to+": "+text) }.
Identify the function by file and name (Name or Type.Method); with line (and in), only the matches on that line are folded, to move callers over one at a time.
A match where the call would not do the same (an argument evaluated in another order or another number of times, a call that cannot be made from there) is left, and the result says why.`
}

func (*Fold) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file": {"type": "string",  "description": "Path of the file that declares the function, relative to the workspace root."},
    "name": {"type": "string",  "description": "The function: Name, or Type.Method for a method."},
    "line": {"type": "integer", "description": "Optional: fold only the matches that start on this line of file (or of in)."},
    "in":   {"type": "string",  "description": "Optional, with line: the file whose line it is, if not file."}
  },
  "required": ["file", "name"],
  "additionalProperties": false
}`)
}

type foldArgs struct {
	File string `json:"file"`
	Name string `json:"name"`
	Line int    `json:"line,omitempty"`
	In   string `json:"in,omitempty"`
}

// folder finds the code that does what fn does and replaces it with calls.
type folder struct {
	ws    *workspace.Workspace
	pkg   *packages.Package // fn's
	fn    *types.Func
	decl  *ast.FuncDecl
	body  ast.Expr   // what fn returns: the pattern; or nil, and
	stmts []ast.Stmt // fn's statements: the pattern, for a function without results
	root  ast.Node   // body or fn's block: where the pattern's names are
	// locals are, while matching statements, the occurrence's variables
	// standing for fn's locals; bodyLocals are fn's locals.
	locals, bodyLocals map[types.Object]types.Object
	done               [][2]token.Pos       // statements folded, which a later match must not overlap
	holes              map[types.Object]int // fn's parameters (and receiver), and how often the body uses each
	// fields are the struct parameters the body uses only by their fields:
	// the fields, in the order the body first uses them. Each field is a
	// hole of its own, and the call passes a literal of the struct.
	fields map[types.Object][]string
	uses   map[holeKey]int // how often the body uses each of those fields
	recv   types.Object    // fn's receiver, or nil
	// self and sets are a setter's: the struct parameter it sets fields
	// of and returns, and the fields with what it sets them to, in order.
	self      types.Object
	sets      []set
	rest      map[ast.Expr]rest       // literals folded through the setter, by what to write for them
	addressOf map[ast.Expr]bool       // arguments &x made for a *p of the body that x matched
	made      map[ast.Expr]bool       // string literals made from a format's text, for a %s of the body
	runs      map[ast.Expr][]ast.Expr // concatenations made of a run of operands, for a string parameter of the body
	effs      *analysis.Effects       // what calls do, built when needed

	n       int      // occurrences folded
	skipped []string // why matches were left, "file:line: reason"
	by      map[string][]edit.TextEdit
	adders  map[string]*imports.Adder
	order   []string
	removed []string // imports removed because only folded code used them
}

// holeKey is a hole: a parameter, or with field one field of a struct
// parameter the body uses only by its fields.
type holeKey struct {
	obj   types.Object
	field string
}

// count is how often the body uses the hole k.
func (f *folder) count(k holeKey) int {
	if k.field != "" {
		return f.uses[k]
	}
	return f.holes[k.obj]
}

// pattern reads fn's declaration: its body must be one return of one
// value, which uses every parameter.
func (f *folder) pattern(id *ast.Ident) error {
	for _, file := range f.pkg.Syntax {
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
		return refactor.Refusef("fold takes a function whose body is one return statement of one value, sets fields of a struct parameter and returns it, or has no results; %s is none of these", name)
	} else if why := statementsOnly(f.decl.Body); why != "" {
		return refactor.Refusef("%s %s, which a call of it would not do in place of its statements", name, why)
	} else {
		f.stmts, f.root = list, f.decl.Body
	}
	info := f.pkg.TypesInfo
	for _, fl := range f.decl.Type.Params.List {
		if len(fl.Names) == 0 {
			return refactor.Refusef("the parameters of %s have no names", name)
		}
		for _, n := range fl.Names {
			f.holes[info.Defs[n]] = 0
		}
	}
	if r := f.decl.Recv; r != nil && len(r.List[0].Names) == 1 && r.List[0].Names[0].Name != "_" {
		f.recv = info.Defs[r.List[0].Names[0]]
		f.holes[f.recv] = 0
	}
	f.bodyLocals = map[types.Object]types.Object{}
	ast.Inspect(f.root, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if d := info.Defs[id]; d != nil {
				f.bodyLocals[d] = d
			}
			if c, hole := f.holes[info.Uses[id]]; hole {
				f.holes[info.Uses[id]] = c + 1
			}
		}
		return true
	})
	if id, ok := ast.Unparen(f.body).(*ast.Ident); ok {
		if _, hole := f.holes[info.Uses[id]]; hole {
			return refactor.Refusef("the body of %s is only its parameter %s, so every value of its type would match", name, id.Name)
		}
	}
	f.fields, f.uses = map[types.Object][]string{}, map[holeKey]int{}
	for o := range f.holes {
		if _, named := o.Type().(*types.Named); o == f.recv || !named {
			continue
		}
		if _, isStruct := o.Type().Underlying().(*types.Struct); !isStruct {
			continue
		}
		var order []string
		byField := 0
		ast.Inspect(f.root, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if id := identOf(selX(sel)); !ok || id == nil || info.Uses[id] != o {
				return true
			}
			if s := info.Selections[sel]; s != nil && s.Kind() == types.FieldVal && len(s.Index()) == 1 {
				k := holeKey{o, sel.Sel.Name}
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
		if byField > 0 && byField == f.holes[o] && !fieldOf(info, f.body, o) {
			f.fields[o] = order
		}
	}
	for o, c := range f.holes {
		if c == 0 && o != f.recv {
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
		return "literals of " + baseName(f.self.Type()).Name() + " giving " + strings.Join(fields, ", ")
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
	info := f.pkg.TypesInfo
	name := ""
	hole := func(e ast.Expr) {
		if id := identOf(e); id != nil && name == "" {
			if _, ok := f.holes[info.Uses[id]]; ok {
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
	info := f.pkg.TypesInfo
	ret, ok := list[len(list)-1].(*ast.ReturnStmt)
	if len(list) < 2 || !ok || len(ret.Results) != 1 || identOf(ret.Results[0]) == nil {
		return false
	}
	self := info.Uses[identOf(ret.Results[0])]
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
		if !ok || identOf(sel.X) == nil || info.Uses[identOf(sel.X)] != self {
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
	b := map[holeKey]ast.Expr{}
	if !f.match(f.body, x, info, b) {
		return false, nil
	}
	where := f.ws.Position(x.Pos())
	skip := func(why string) (bool, error) {
		f.skipped = append(f.skipped, where+": "+why)
		return false, nil
	}
	if len(stack) > 0 && assigned(stack[len(stack)-1], x) {
		return skip("it is assigned or its address taken")
	}
	if p.PkgPath != f.pkg.PkgPath && !f.fn.Exported() {
		return skip(fmt.Sprintf("%s is not exported, so package %s cannot call it", f.fn.Name(), p.Name))
	}
	// Arguments are evaluated once, before the body: one that has effects
	// or could panic must be used once, and be what the body evaluates
	// first.
	impure := 0
	for k, e := range b {
		if f.impure(info, e) || f.nilAddress(info, e) {
			impure++
			if n := f.count(k); n > 1 {
				return skip(fmt.Sprintf("%s, used %d times in the body, would be evaluated once", types.ExprString(e), n))
			}
			if !f.first(k) {
				return skip(fmt.Sprintf("%s would be evaluated before the rest of the body, not in its order", types.ExprString(e)))
			}
		}
	}
	if impure > 1 {
		return skip("more than one argument has effects or could panic")
	}
	for _, e := range b {
		if why := f.staleRead(info, []ast.Node{x}, e); why != "" {
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
	if !ok || !sameObj(baseName(info.TypeOf(lit)), baseName(f.self.Type())) {
		return false, nil
	}
	b := map[holeKey]ast.Expr{{f.self, ""}: lit}
	var drop []int
	for _, s := range f.sets {
		i := slices.IndexFunc(lit.Elts, func(e ast.Expr) bool {
			kv, ok := e.(*ast.KeyValueExpr)
			return ok && identOf(kv.Key) != nil && identOf(kv.Key).Name == s.field
		})
		if i < 0 || !f.match(s.value, lit.Elts[i].(*ast.KeyValueExpr).Value, info, b) {
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
	if p.PkgPath != f.pkg.PkgPath && !f.fn.Exported() {
		return skip(fmt.Sprintf("%s is not exported, so package %s cannot call it", f.fn.Name(), p.Name))
	}
	typ, why := f.elided(p, lit, stack)
	if why != "" {
		return skip(why)
	}
	// The fields set are evaluated after the rest of the literal, in the
	// setter's order.
	moved, others := 0, 0
	for i, e := range lit.Elts {
		if kv, ok := e.(*ast.KeyValueExpr); ok && (analysis.HasEffect(info, kv.Value) || analysis.MayPanic(info, kv.Value) != nil) {
			if slices.Contains(drop, i) {
				moved++
			} else {
				others++
			}
		}
	}
	if moved > 0 && moved+others > 1 {
		return skip("fields with effects or that could panic would be evaluated in another order")
	}
	for k, e := range b {
		if n := f.count(k); k.obj != f.self && n > 1 && (analysis.HasEffect(info, e) || analysis.MayPanic(info, e) != nil) {
			return skip(fmt.Sprintf("%s, used %d times in the body, would be evaluated once", types.ExprString(e), n))
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
	if tn := baseName(f.self.Type()); tn.Pkg().Path() != f.fn.Pkg().Path() {
		return "", fmt.Sprintf("its type is elided in the enclosing literal, and %s is declared in another package than %s", tn.Name(), f.fn.Name())
	}
	return baseName(f.self.Type()).Name(), ""
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
func (f *folder) call(p *packages.Package, file *ast.File, b map[holeKey]ast.Expr, stack []ast.Node) (string, string, error) {
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
		if p.PkgPath == f.pkg.PkgPath {
			return name, nil
		}
		if f.adders[path] == nil {
			f.adders[path] = imports.New(f.ws, p, file, src)
		}
		q, err := f.adders[path].Name(f.fn.Pkg())
		return q + "." + name, err
	}
	if lit := b[holeKey{f.self, ""}]; lit != nil && f.rest[lit].typ != "" {
		r := f.rest[lit]
		if r.typ, err = qualify(r.typ); err != nil {
			return "", err.Error(), nil
		}
		f.rest[lit] = r
	}
	var args []string
	for _, fl := range f.decl.Type.Params.List {
		for _, n := range fl.Names {
			o := f.pkg.TypesInfo.Defs[n]
			fs, byField := f.fields[o]
			if !byField {
				args = append(args, text(b[holeKey{o, ""}]))
				continue
			}
			t, err := qualify(o.Type().(*types.Named).Obj().Name())
			if err != nil {
				return "", err.Error(), nil
			}
			var elts []string
			for _, fld := range fs {
				elts = append(elts, fld+": "+text(b[holeKey{o, fld}]))
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

// first reports whether the hole k is the first thing the body evaluates
// that matters: the body has neither effects nor panics but in its
// holes, or k is an argument of the body's outermost call whose other
// arguments have neither.
func (f *folder) first(k holeKey) bool {
	info := f.pkg.TypesInfo
	if !analysis.HasEffect(info, f.body) && analysis.MayPanic(info, f.body) == nil {
		return true
	}
	call, ok := ast.Unparen(f.body).(*ast.CallExpr)
	if !ok {
		return false
	}
	is := func(a ast.Expr) bool {
		switch a := ast.Unparen(a).(type) {
		case *ast.Ident:
			return k.field == "" && info.Uses[a] == k.obj
		case *ast.SelectorExpr:
			id := identOf(a.X)
			return k.field == a.Sel.Name && id != nil && info.Uses[id] == k.obj
		}
		return false
	}
	for _, a := range call.Args {
		if !is(a) && (analysis.HasEffect(info, a) || analysis.MayPanic(info, a) != nil) {
			return false
		}
	}
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		return true
	case *ast.SelectorExpr:
		_, pkgName := info.Uses[identOf(fun.X)].(*types.PkgName)
		return pkgName
	}
	return false
}

// receiver is the receiver of the call: what matched the body's receiver,
// or, when the body does not use it, the receiver of the method of the
// same type the occurrence is in; or why there is none.
func (f *folder) receiver(info *types.Info, b map[holeKey]ast.Expr, stack []ast.Node, text func(ast.Expr) string) (string, string) {
	recvT := f.fn.Signature().Recv().Type()
	_, ptr := recvT.(*types.Pointer)
	if e, ok := b[holeKey{f.recv, ""}]; ok {
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
	base := baseName(recvT)
	for i := len(stack) - 1; i >= 0; i-- {
		fd, ok := stack[i].(*ast.FuncDecl)
		if !ok || fd.Recv == nil || len(fd.Recv.List[0].Names) != 1 {
			continue
		}
		r := info.Defs[fd.Recv.List[0].Names[0]]
		if r != nil && base != nil && sameObj(baseName(r.Type()), base) && fd.Recv.List[0].Names[0].Name != "_" {
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

// match reports whether x, of the package whose info is info, is the
// pattern p (of fn's package) with its holes filled in b.
func (f *folder) match(p, x ast.Expr, info *types.Info, b map[holeKey]ast.Expr) bool {
	p, x = ast.Unparen(p), ast.Unparen(x)
	pinfo := f.pkg.TypesInfo
	sub := func(p, x ast.Expr) bool {
		if p == nil || x == nil {
			return p == nil && x == nil
		}
		return f.match(p, x, info, b)
	}
	switch pp := p.(type) {
	case *ast.Ident:
		if pd := pinfo.Defs[pp]; pd != nil || pp.Name == "_" {
			// A local fn declares: the occurrence's own name for it.
			xi, ok := x.(*ast.Ident)
			if !ok || pp.Name == "_" {
				return ok && xi.Name == "_"
			}
			xd := info.Defs[xi]
			if xd == nil || f.locals == nil {
				return false
			}
			f.locals[pd] = xd
			return true
		}
		if xo, local := f.locals[pinfo.Uses[pp]]; local {
			xi, ok := x.(*ast.Ident)
			return ok && info.Uses[xi] == xo
		}
		if _, local := f.bodyLocals[pinfo.Uses[pp]]; local {
			return false
		}
		po := pinfo.Uses[pp]
		if _, hole := f.holes[po]; hole && po != nil {
			if prev, ok := b[holeKey{po, ""}]; ok {
				return types.ExprString(prev) == types.ExprString(x)
			}
			xt := info.TypeOf(x)
			if xt == nil || !info.Types[x].IsValue() {
				return false
			}
			if po == f.recv {
				if !sameObj(baseName(xt), baseName(po.Type())) {
					return false
				}
			} else if !types.AssignableTo(xt, po.Type()) && types.TypeString(xt, nil) != types.TypeString(po.Type(), nil) {
				return false
			}
			b[holeKey{po, ""}] = x
			return true
		}
		switch x := x.(type) {
		case *ast.Ident:
			return x.Name == pp.Name && sameObj(po, info.Uses[x])
		case *ast.SelectorExpr: // pkg.Name in another package
			_, pkgName := info.Uses[identOf(x.X)].(*types.PkgName)
			return pkgName && x.Sel.Name == pp.Name && sameObj(po, info.Uses[x.Sel])
		}
		return false
	case *ast.BasicLit:
		xb, ok := x.(*ast.BasicLit)
		return ok && xb.Kind == pp.Kind && xb.Value == pp.Value
	case *ast.SelectorExpr:
		if id := identOf(pp.X); id != nil {
			po := pinfo.Uses[id]
			if _, byField := f.fields[po]; byField {
				k := holeKey{po, pp.Sel.Name}
				if prev, ok := b[k]; ok {
					return types.ExprString(prev) == types.ExprString(x)
				}
				xt := info.TypeOf(x)
				if xt == nil || !info.Types[x].IsValue() || !types.AssignableTo(xt, pinfo.TypeOf(pp)) && types.TypeString(xt, nil) != types.TypeString(pinfo.TypeOf(pp), nil) {
					return false
				}
				b[k] = x
				return true
			}
			if _, pkgName := po.(*types.PkgName); pkgName {
				if xi, ok := x.(*ast.Ident); ok { // Name in the package itself
					return xi.Name == pp.Sel.Name && sameObj(pinfo.Uses[pp.Sel], info.Uses[xi])
				}
			}
		}
		xs, ok := x.(*ast.SelectorExpr)
		return ok && xs.Sel.Name == pp.Sel.Name && sub(pp.X, xs.X) && sameObj(pinfo.Uses[pp.Sel], info.Uses[xs.Sel])
	case *ast.CallExpr:
		xc, ok := x.(*ast.CallExpr)
		if !ok || xc.Ellipsis.IsValid() != pp.Ellipsis.IsValid() || !sub(pp.Fun, xc.Fun) {
			return false
		}
		if len(xc.Args) != len(pp.Args) {
			return f.matchFormat(pp, xc, info, b)
		}
		for i := range pp.Args {
			if !sub(pp.Args[i], xc.Args[i]) {
				return false
			}
		}
		return true
	case *ast.BinaryExpr:
		if pp.Op == token.ADD && analysis.IsString(pinfo.TypeOf(pp)) {
			xb, ok := x.(*ast.BinaryExpr)
			return ok && xb.Op == token.ADD && f.matchConcat(pp, xb, info, b)
		}
		xb, ok := x.(*ast.BinaryExpr)
		return ok && xb.Op == pp.Op && sub(pp.X, xb.X) && sub(pp.Y, xb.Y)
	case *ast.UnaryExpr:
		xu, ok := x.(*ast.UnaryExpr)
		return ok && xu.Op == pp.Op && sub(pp.X, xu.X)
	case *ast.StarExpr:
		if xs, ok := x.(*ast.StarExpr); ok {
			return sub(pp.X, xs.X)
		}
		return f.matchAddress(pp, x, info, b)
	case *ast.IndexExpr:
		xi, ok := x.(*ast.IndexExpr)
		return ok && sub(pp.X, xi.X) && sub(pp.Index, xi.Index)
	case *ast.SliceExpr:
		xs, ok := x.(*ast.SliceExpr)
		return ok && xs.Slice3 == pp.Slice3 && sub(pp.X, xs.X) && sub(pp.Low, xs.Low) && sub(pp.High, xs.High) && sub(pp.Max, xs.Max)
	case *ast.TypeAssertExpr:
		xt, ok := x.(*ast.TypeAssertExpr)
		return ok && pp.Type != nil && xt.Type != nil && sub(pp.X, xt.X) && types.Identical(pinfo.TypeOf(pp.Type), info.TypeOf(xt.Type))
	case *ast.CompositeLit:
		xc, ok := x.(*ast.CompositeLit)
		if !ok || len(xc.Elts) != len(pp.Elts) || types.TypeString(pinfo.TypeOf(pp), nil) != types.TypeString(info.TypeOf(xc), nil) {
			return false
		}
		for i := range pp.Elts {
			if !sub(pp.Elts[i], xc.Elts[i]) {
				return false
			}
		}
		return true
	case *ast.KeyValueExpr:
		xk, ok := x.(*ast.KeyValueExpr)
		return ok && sub(pp.Key, xk.Key) && sub(pp.Value, xk.Value)
	}
	return false
}

// matchAddress reports whether x, which is not a dereference, is the
// pattern's *p for a pointer parameter p: an addressable expression of
// p's element type, so that the call passes &x.
func (f *folder) matchAddress(pp *ast.StarExpr, x ast.Expr, info *types.Info, b map[holeKey]ast.Expr) bool {
	id, ok := ast.Unparen(pp.X).(*ast.Ident)
	if !ok {
		return false
	}
	po := f.pkg.TypesInfo.Uses[id]
	if _, hole := f.holes[po]; !hole || po == nil || po == f.recv {
		return false
	}
	ptr, ok := po.Type().Underlying().(*types.Pointer)
	xt := info.TypeOf(x)
	if !ok || xt == nil || !addressable(info, x) || !types.Identical(xt, ptr.Elem()) && types.TypeString(xt, nil) != types.TypeString(ptr.Elem(), nil) {
		return false
	}
	u := &ast.UnaryExpr{OpPos: x.Pos(), Op: token.AND, X: x}
	k := holeKey{po, ""}
	if prev, ok := b[k]; ok {
		return types.ExprString(prev) == types.ExprString(u)
	}
	b[k] = u
	f.addressOf[u] = true
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
func (f *folder) matchFormat(pp, xc *ast.CallExpr, info *types.Info, b map[holeKey]ast.Expr) bool {
	pinfo := f.pkg.TypesInfo
	fi := formatIndex(pinfo, pp)
	if fi < 0 || pp.Ellipsis.IsValid() || len(xc.Args) <= fi || len(xc.Args) > len(pp.Args) {
		return false
	}
	for i := 0; i < fi; i++ {
		if !f.match(pp.Args[i], xc.Args[i], info, b) {
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
	var try func(k int, rest string, xi int, b map[holeKey]ast.Expr) map[holeKey]ast.Expr
	try = func(k int, rest string, xi int, b map[holeKey]ast.Expr) map[holeKey]ast.Expr {
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
			if f.match(arg, xc.Args[xi], info, c) {
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
			if f.bindText(arg, rest[:j], xc.Args[fi].Pos(), c) {
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
func (f *folder) bindText(arg ast.Expr, text string, pos token.Pos, b map[holeKey]ast.Expr) bool {
	po := f.stringParam(arg)
	if po == nil {
		return false
	}
	k := holeKey{po, ""}
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
	f.made[lit] = true
	return true
}

// stringParam is the parameter of type string that arg, of the pattern,
// is, or nil.
func (f *folder) stringParam(arg ast.Expr) types.Object {
	id, ok := ast.Unparen(arg).(*ast.Ident)
	if !ok {
		return nil
	}
	po := f.pkg.TypesInfo.Uses[id]
	if _, hole := f.holes[po]; !hole || po == nil || po == f.recv || !types.Identical(po.Type(), types.Typ[types.String]) {
		return nil
	}
	return po
}

// matchConcat reports whether x, a string concatenation, is the
// pattern's concatenation pp, + being associative: each operand of pp
// matches one of x's in order, but a string parameter may match a run of
// them, so to+": "+text is first+" "+last+": "+ref with to first+" "+last.
func (f *folder) matchConcat(pp, x *ast.BinaryExpr, info *types.Info, b map[holeKey]ast.Expr) bool {
	ps, xs := concatOperands(f.pkg.TypesInfo, pp), concatOperands(info, x)
	// try matches ps[i:] to xs[j:]; it returns the holes, or nil.
	var try func(i, j int, b map[holeKey]ast.Expr) map[holeKey]ast.Expr
	try = func(i, j int, b map[holeKey]ast.Expr) map[holeKey]ast.Expr {
		if i == len(ps) || j == len(xs) {
			if i == len(ps) && j == len(xs) {
				return b
			}
			return nil
		}
		c := maps.Clone(b)
		if f.match(ps[i], xs[j], info, c) {
			if r := try(i+1, j+1, c); r != nil {
				return r
			}
		}
		po := f.stringParam(ps[i])
		for k := j + 2; po != nil && k <= len(xs)-(len(ps)-i-1); k++ {
			c := maps.Clone(b)
			if f.bindRun(po, xs[j:k], c) {
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
func (f *folder) bindRun(po types.Object, run []ast.Expr, b map[holeKey]ast.Expr) bool {
	e := run[0]
	for _, y := range run[1:] {
		e = &ast.BinaryExpr{X: e, OpPos: y.Pos(), Op: token.ADD, Y: y}
	}
	k := holeKey{po, ""}
	if prev, ok := b[k]; ok {
		return types.ExprString(prev) == types.ExprString(e)
	}
	b[k] = e
	f.runs[e] = run
	return true
}

// concatOperands lists the operands of e as a string concatenation,
// however it is grouped: a+(b+c) is a, b, c.
func concatOperands(info *types.Info, e ast.Expr) []ast.Expr {
	if bin, ok := ast.Unparen(e).(*ast.BinaryExpr); ok && bin.Op == token.ADD && analysis.IsString(info.TypeOf(bin)) {
		return append(concatOperands(info, bin.X), concatOperands(info, bin.Y)...)
	}
	return []ast.Expr{e}
}

// effects is what calls in the workspace do.
func (f *folder) effects() *analysis.Effects {
	if f.effs == nil {
		f.effs = analysis.NewEffects()
		for _, p := range f.ws.Pkgs {
			f.effs.AddPackage(p.Syntax, p.TypesInfo)
		}
	}
	return f.effs
}

// impure reports whether evaluating e could do more than compute a
// value: a call in it has side effects, reads what other code can
// change, or could panic or not finish; or e could panic itself.
func (f *folder) impure(info *types.Info, e ast.Expr) bool {
	if analysis.MayPanic(info, e) != nil {
		return true
	}
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			s := f.effects().Call(info, n)
			found = s.Effect != "" || s.Reads != "" || s.Unsafe != ""
		case *ast.UnaryExpr:
			found = n.Op == token.ARROW
		}
		return !found
	})
	return found
}

// evaluatedFirst reports whether the argument for the hole k, which
// could panic, may still be evaluated once before the statements seq:
// it has no side effects, and if the body uses it more than once it
// reads nothing other code can change, so each use has its value; in seq
// nothing that matters (a side effect, an assignment, a possible panic)
// comes before its first use; and no argument the call evaluates before
// it could panic.
func (f *folder) evaluatedFirst(info *types.Info, seq []ast.Stmt, b map[holeKey]ast.Expr, k holeKey) bool {
	e := b[k]
	if k.field != "" || f.addressOf[e] || f.effectful(info, e, f.count(k) > 1) {
		return false
	}
	for _, s := range seq {
		if s.Pos() >= e.Pos() {
			break
		}
		if f.matters(info, s, e.Pos()) {
			return false
		}
	}
	if f.recv != nil && k.obj != f.recv {
		if a := b[holeKey{f.recv, ""}]; a != nil && f.impure(info, a) {
			return false
		}
	}
	for _, fl := range f.decl.Type.Params.List {
		for _, n := range fl.Names {
			o := f.pkg.TypesInfo.Defs[n]
			if o == k.obj {
				return true
			}
			keys := []holeKey{{o, ""}}
			if fs, byField := f.fields[o]; byField {
				keys = nil
				for _, fld := range fs {
					keys = append(keys, holeKey{o, fld})
				}
			}
			for _, ok := range keys {
				if a := b[ok]; a == nil || f.impure(info, a) {
					return false
				}
			}
		}
	}
	return true
}

// effectful reports whether evaluating e has side effects or, with
// shared, reads memory other code can change: a package variable, or
// through a pointer, a slice or a map.
func (f *folder) effectful(info *types.Info, e ast.Expr, shared bool) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			s := f.effects().Call(info, n)
			found = s.Effect != "" || shared && s.Reads != ""
		case *ast.UnaryExpr:
			found = n.Op == token.ARROW
		case *ast.StarExpr:
			found = shared && !info.Types[n].IsType()
		case *ast.SelectorExpr:
			s := info.Selections[n]
			found = shared && s != nil && s.Kind() == types.FieldVal && s.Indirect()
		case *ast.IndexExpr:
			if tv := info.Types[n.X]; shared && !tv.IsType() {
				switch tv.Type.Underlying().(type) {
				case *types.Slice, *types.Map, *types.Pointer:
					found = true
				}
			}
		case *ast.Ident:
			v, ok := info.Uses[n].(*types.Var)
			found = shared && ok && !v.IsField() && v.Pkg() != nil && v.Parent() == v.Pkg().Scope()
		}
		return !found
	})
	return found
}

// matters reports whether code of the statement s that is evaluated
// before pos does something an argument evaluated ahead of it could
// change or see: a side effect, an assignment, or a possible panic.
func (f *folder) matters(info *types.Info, s ast.Stmt, pos token.Pos) bool {
	found := false
	ast.Inspect(s, func(n ast.Node) bool {
		if _, lit := n.(*ast.FuncLit); lit || n == nil || found || n.Pos() >= pos {
			return false
		}
		if n.End() > pos {
			return true
		}
		switch n := n.(type) {
		case *ast.AssignStmt, *ast.IncDecStmt, *ast.SendStmt, *ast.GoStmt, *ast.DeferStmt:
			found = true
		case *ast.CallExpr:
			s := f.effects().Call(info, n)
			found = s.Effect != "" || s.Unsafe != ""
		case *ast.UnaryExpr:
			found = n.Op == token.ARROW
		}
		if e, ok := n.(ast.Expr); ok && !found {
			found = analysis.MayPanic(info, e) != nil
		}
		return !found
	})
	return found
}

// staleRead is why e, an argument that reads memory other code can
// change (a field through a pointer, an element, a package variable),
// would not read what the code roots, which it replaces, read where it
// read it: code there that ends before e's last use could write that
// memory; or "". An argument with effects or that could panic is judged
// by impure and evaluatedFirst instead.
func (f *folder) staleRead(info *types.Info, roots []ast.Node, e ast.Expr) string {
	if f.addressOf[e] || f.made[e] || f.impure(info, e) || !f.effectful(info, e, true) {
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
	for _, r := range roots {
		why, at := "", token.NoPos
		ast.Inspect(r, func(n ast.Node) bool {
			if _, lit := n.(*ast.FuncLit); lit || n == nil || why != "" || n.Pos() >= last {
				return false
			}
			if n.End() > last {
				return true // it encloses the last use, which it reads first
			}
			switch n := n.(type) {
			case *ast.AssignStmt:
				if n.Tok != token.DEFINE {
					why = "assigns " + types.ExprString(n.Lhs[0])
				}
			case *ast.IncDecStmt:
				why = "changes " + types.ExprString(n.X)
			case *ast.CallExpr:
				why = f.effects().Call(info, n).Effect
			}
			at = n.Pos()
			return why == ""
		})
		if why != "" {
			return fmt.Sprintf("%s would be read before the code it is in, and code before its last use could change it before then (it %s, at %s)", text, why, f.ws.Position(at))
		}
	}
	return ""
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

// nilAddress reports whether e is an argument &x made for a *p of the
// body that can panic: x goes through a pointer that could be nil.
func (f *folder) nilAddress(info *types.Info, e ast.Expr) bool {
	if !f.addressOf[e] {
		return false
	}
	for _, p := range analysis.Addressed(info, e) {
		if p != nil {
			return true
		}
	}
	return false
}

// movedAddress is why an argument &x for a *p of the body, evaluated
// before the statements seq, would not do what they did, or "". Taking
// x's address can panic only when a pointer it goes through is nil. That
// panic may come before the code that ran before x's first use when that
// code returns and panics no other way, and the pointer is a local
// variable that nothing else changes.
func (f *folder) movedAddress(info *types.Info, seq []ast.Stmt, b map[holeKey]ast.Expr, stack []ast.Node) string {
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
			fa := analysis.AnalyzeFunc(info, fn)
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
			why = f.effects().Call(info, c).Stops
		}
		return why == ""
	})
	return why
}

// matchStmt reports whether x is the pattern's statement p.
func (f *folder) matchStmt(p, x ast.Stmt, info *types.Info, b map[holeKey]ast.Expr) bool {
	if p == nil || x == nil {
		return p == nil && x == nil
	}
	expr := func(p, x ast.Expr) bool {
		if p == nil || x == nil {
			return p == nil && x == nil
		}
		return f.match(p, x, info, b)
	}
	exprs := func(p, x []ast.Expr) bool {
		if len(p) != len(x) {
			return false
		}
		for i := range p {
			if !expr(p[i], x[i]) {
				return false
			}
		}
		return true
	}
	block := func(p, x *ast.BlockStmt) bool {
		if p == nil || x == nil {
			return p == nil && x == nil
		}
		if len(p.List) != len(x.List) {
			return false
		}
		for i := range p.List {
			if !f.matchStmt(p.List[i], x.List[i], info, b) {
				return false
			}
		}
		return true
	}
	switch pp := p.(type) {
	case *ast.ExprStmt:
		xs, ok := x.(*ast.ExprStmt)
		return ok && expr(pp.X, xs.X)
	case *ast.AssignStmt:
		xs, ok := x.(*ast.AssignStmt)
		return ok && xs.Tok == pp.Tok && exprs(pp.Rhs, xs.Rhs) && exprs(pp.Lhs, xs.Lhs)
	case *ast.IncDecStmt:
		xs, ok := x.(*ast.IncDecStmt)
		return ok && xs.Tok == pp.Tok && expr(pp.X, xs.X)
	case *ast.BlockStmt:
		xs, ok := x.(*ast.BlockStmt)
		return ok && block(pp, xs)
	case *ast.IfStmt:
		xs, ok := x.(*ast.IfStmt)
		return ok && f.matchStmt(pp.Init, xs.Init, info, b) && expr(pp.Cond, xs.Cond) && block(pp.Body, xs.Body) && f.matchStmt(pp.Else, xs.Else, info, b)
	case *ast.ForStmt:
		xs, ok := x.(*ast.ForStmt)
		return ok && f.matchStmt(pp.Init, xs.Init, info, b) && expr(pp.Cond, xs.Cond) && f.matchStmt(pp.Post, xs.Post, info, b) && block(pp.Body, xs.Body)
	case *ast.RangeStmt:
		xs, ok := x.(*ast.RangeStmt)
		return ok && xs.Tok == pp.Tok && expr(pp.X, xs.X) && expr(pp.Key, xs.Key) && expr(pp.Value, xs.Value) && block(pp.Body, xs.Body)
	case *ast.DeclStmt:
		xs, ok := x.(*ast.DeclStmt)
		pg, _ := pp.Decl.(*ast.GenDecl)
		xg, _ := xs.Decl.(*ast.GenDecl)
		if !ok || pg == nil || xg == nil || pg.Tok != xg.Tok || len(pg.Specs) != len(xg.Specs) {
			return false
		}
		for i := range pg.Specs {
			ps, _ := pg.Specs[i].(*ast.ValueSpec)
			vs, _ := xg.Specs[i].(*ast.ValueSpec)
			if ps == nil || vs == nil || len(ps.Names) != len(vs.Names) || (ps.Type == nil) != (vs.Type == nil) || !exprs(ps.Values, vs.Values) {
				return false
			}
			if ps.Type != nil && !types.Identical(f.pkg.TypesInfo.TypeOf(ps.Type), info.TypeOf(vs.Type)) {
				return false
			}
			for j := range ps.Names {
				if !expr(ps.Names[j], vs.Names[j]) {
					return false
				}
			}
		}
		return true
	case *ast.EmptyStmt:
		_, ok := x.(*ast.EmptyStmt)
		return ok
	}
	return false
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
		b := map[holeKey]ast.Expr{}
		f.locals = map[types.Object]types.Object{}
		matched := true
		for k := range f.stmts {
			if !f.matchStmt(f.stmts[k], list[i+k], info, b) {
				matched = false
				break
			}
		}
		f.locals = nil
		if !matched {
			continue
		}
		seq := list[i : i+n]
		where := f.ws.Position(seq[0].Pos())
		skip := ""
		for k, e := range b {
			if f.impure(info, e) && !f.evaluatedFirst(info, seq, b, k) {
				skip = types.ExprString(e) + " would be evaluated before the statements, not where it is"
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
				skip = f.staleRead(info, stmtNodes(seq), e)
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
				if id := identOf(l); id != nil && info.Defs[id] != nil {
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

// sameObj reports whether a and b, from any variants of their packages,
// are the same object.
func sameObj(a, b types.Object) bool {
	switch {
	case a == nil || b == nil:
		return false
	case a.Name() != b.Name():
		return false
	case a.Pkg() == nil || b.Pkg() == nil:
		return a.Pkg() == nil && b.Pkg() == nil
	}
	if pa, ok := a.(*types.PkgName); ok {
		pb, ok := b.(*types.PkgName)
		return ok && pa.Imported().Path() == pb.Imported().Path()
	}
	return a.Pkg().Path() == b.Pkg().Path() && a.Pos() == b.Pos()
}

// baseName is the type name of t, T or *T, or nil.
func baseName(t types.Type) types.Object {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := t.(*types.Named); ok {
		return n.Obj()
	}
	return nil
}

// identOf is e's identifier, if it is one.
func identOf(e ast.Expr) *ast.Ident {
	id, _ := ast.Unparen(e).(*ast.Ident)
	return id
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
	return ok && info.Uses[identOf(selX(sel))] == o
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
	f := &folder{ws: ws, pkg: pkg, fn: fn, holes: map[types.Object]int{}}
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
