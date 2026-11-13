// Package convertloop implements "convert_loop": a loop rewritten as
// another statement form that runs the same code in the same order.
//
//	for i := 0; i < len(names); i++ {        for _, name := range names {
//		fmt.Println(names[i])          <=>        fmt.Println(name)
//	}                                        }
//
// The pairs are those java-refactor-mcp's convert_loop has where Go has
// both forms: an index loop and a range loop (to=for_each and to=for).
package convertloop

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"go/version"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/site"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// ConvertLoop is the "convert_loop" refactoring.
type ConvertLoop struct{ refactor.Named[args] }

// New returns the convert_loop refactoring.
func New() *ConvertLoop { return &ConvertLoop{Named: refactor.Called[args]("convert_loop")} }

func (*ConvertLoop) Description() string {
	return `Rewrite a loop as another statement form that runs the same code in the same order. to=for_each turns an index loop over a slice or an array, for i := 0; i < len(names); i++ { fmt.Println(names[i]) }, into for _, name := range names { fmt.Println(name) }, keeping i as the key when the body still uses it; to=for turns a range over a slice or an array back into an index loop, the element assigned first in the body.
Identify the loop by file and the line of its for keyword (column if the line has two).`
}

type args struct {
	File   string `json:"file" desc:"Path of the file, relative to the workspace root."`
	Line   int    `json:"line" desc:"1-based line of the for keyword."`
	Column int    `json:"column,omitempty" desc:"Optional 1-based column of the for keyword, only needed if the line has more than one loop."`
	To     string `json:"to" desc:"The form to write: for_each (a range loop) or for (an index loop)."`
}

type conversion struct {
	site.Site
	path string
	line int
}

// Plan checks preconditions and computes the edits.
func (*ConvertLoop) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	switch {
	case a.File == "":
		return nil, refactor.Refusef("file is required")
	case a.Line < 1:
		return nil, refactor.Refusef("line is required (1-based)")
	case a.To != "for_each" && a.To != "for":
		return nil, refactor.Refusef("to is for_each or for; to=%q is not built for Go", a.To)
	}
	path := ws.Abs(a.File)
	at, err := site.At(ws, path)
	if err != nil {
		return nil, err
	}
	x := &conversion{Site: *at, path: path, line: a.Line}
	loop, err := x.find(a)
	if err != nil {
		return nil, err
	}
	var edits []edit.TextEdit
	var summary string
	switch l := loop.(type) {
	case *ast.ForStmt:
		if a.To != "for_each" {
			return nil, refactor.Refusef("the loop at line %d is already a for with an index; to=for_each turns it into a range", a.Line)
		}
		summary, edits, err = x.indexToRange(l)
	case *ast.RangeStmt:
		if a.To != "for" {
			return nil, refactor.Refusef("the loop at line %d is already a range loop; to=for turns it into an index loop", a.Line)
		}
		summary, edits, err = x.rangeToIndex(l)
	}
	if err != nil {
		return nil, err
	}
	return &refactor.Plan{
		Summary: fmt.Sprintf("rewrote the loop at line %d as %s", a.Line, summary),
		Edits:   []edit.FileEdit{{Path: x.path, Edits: edits}},
	}, nil
}

// find returns the for or range statement whose for keyword is at the
// line (and column, if given).
func (x *conversion) find(a args) (ast.Stmt, error) {
	if a.Line > x.TF.LineCount() {
		return nil, refactor.Refusef("line %d is past the end of %s, which has %d lines", a.Line, a.File, x.TF.LineCount())
	}
	var found []ast.Stmt
	at := func(p token.Pos) bool {
		return x.TF.Line(p) == a.Line && (a.Column == 0 || x.TF.Position(p).Column == a.Column)
	}
	ast.Inspect(x.File, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.ForStmt:
			if at(s.For) {
				found = append(found, s)
			}
		case *ast.RangeStmt:
			if at(s.For) {
				found = append(found, s)
			}
		}
		return true
	})
	switch {
	case len(found) == 1:
		return found[0], nil
	case len(found) > 1:
		var cols []string
		for _, s := range found {
			cols = append(cols, fmt.Sprint(x.TF.Position(s.Pos()).Column))
		}
		return nil, refactor.Refusef("line %d has %d loops (columns %s); pass column to choose one", a.Line, len(found), strings.Join(cols, ", "))
	case a.Column != 0:
		return nil, refactor.Refusef("no loop starts at %s:%d:%d", a.File, a.Line, a.Column)
	}
	return nil, refactor.Refusef("no loop starts on line %d of %s; give the line of the for keyword", a.Line, a.File)
}

// indexed is an index loop, for i := 0; i < len(xs); i++.
type indexed struct {
	loop  *ast.ForStmt
	index *types.Var
	over  *ast.Ident
}

// indexLoop reads l as an index loop over a slice or an array (i++ and
// i += 1 for the post statement), or refuses.
func (x *conversion) indexLoop(l *ast.ForStmt) (*indexed, error) {
	shape := refactor.Refusef("the loop at line %d is not an index loop, for i := 0; i < len(xs); i++, over a slice or an array", x.line)
	init, ok := l.Init.(*ast.AssignStmt)
	if !ok || init.Tok != token.DEFINE || len(init.Lhs) != 1 || len(init.Rhs) != 1 {
		return nil, shape
	}
	id, _ := init.Lhs[0].(*ast.Ident)
	zero, _ := init.Rhs[0].(*ast.BasicLit)
	if id == nil || zero == nil || zero.Value != "0" {
		return nil, shape
	}
	i, _ := x.Info.Defs[id].(*types.Var)
	cond, _ := l.Cond.(*ast.BinaryExpr)
	if i == nil || cond == nil || cond.Op != token.LSS || !x.is(cond.X, i) {
		return nil, shape
	}
	call, _ := cond.Y.(*ast.CallExpr)
	if call == nil || len(call.Args) != 1 || !x.builtin(call.Fun, "len") {
		return nil, refactor.Refusef("the loop at line %d is bounded by %s, not by len( of what it reads; to=for_each needs i < len(xs)", x.line, x.Text(cond.Y))
	}
	over, _ := call.Args[0].(*ast.Ident)
	if over == nil || !x.walkable(x.Info.TypeOf(over)) {
		return nil, refactor.Refusef("the loop at line %d is bounded by %s; to=for_each takes a local slice or array, len(xs). Put it in a local first (extract_variable)", x.line, x.Text(call))
	}
	if !x.increments(l.Post, i) {
		return nil, shape
	}
	return &indexed{loop: l, index: i, over: over}, nil
}

// increments reports whether s is i++ or i += 1.
func (x *conversion) increments(s ast.Stmt, i *types.Var) bool {
	switch s := s.(type) {
	case *ast.IncDecStmt:
		return s.Tok == token.INC && x.is(s.X, i)
	case *ast.AssignStmt:
		one, _ := s.Rhs[0].(*ast.BasicLit)
		return s.Tok == token.ADD_ASSIGN && len(s.Lhs) == 1 && x.is(s.Lhs[0], i) && one != nil && one.Value == "1"
	}
	return false
}

// indexToRange turns an index loop into a range: each xs[i] becomes the
// element, and i stays as the key when the body uses it otherwise.
func (x *conversion) indexToRange(l *ast.ForStmt) (string, []edit.TextEdit, error) {
	ix, err := x.indexLoop(l)
	if err != nil {
		return "", nil, err
	}
	over := x.Info.Uses[ix.over]
	if err := x.stable(ix.over, l.Body); err != nil {
		return "", nil, err
	}
	var reads []*ast.IndexExpr
	keyUsed := false
	var bad error
	parents := map[ast.Node]ast.Node{}
	stack := []ast.Node{}
	ast.Inspect(l.Body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if len(stack) > 0 {
			parents[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})
	ast.Inspect(l.Body, func(n ast.Node) bool {
		if bad != nil {
			return false
		}
		switch n := n.(type) {
		case *ast.IndexExpr:
			if root, _ := n.X.(*ast.Ident); root != nil && x.Info.Uses[root] == over {
				if write := x.written(n, parents); write != "" {
					bad = refactor.Refusef("the loop at line %d writes %s (%s); a range's element is a copy, so the write would not reach the next read of it", x.line, x.Text(n), write)
					return false
				}
				if x.is(n.Index, ix.index) {
					reads = append(reads, n)
					return false
				}
			}
		case *ast.Ident:
			if x.Info.Uses[n] == ix.index {
				keyUsed = true
			}
		}
		return true
	})
	if bad != nil {
		return "", nil, bad
	}
	if err := x.notAssigned(l.Body, ix.index); err != nil {
		return "", nil, err
	}
	if err := x.notShared(l, ix.index); err != nil {
		return "", nil, err
	}
	var edits []edit.TextEdit
	value := ""
	if len(reads) > 0 {
		if first, name := x.firstCopy(l.Body, reads); first != nil {
			value = name
			edits = append(edits, edit.TextEdit{Start: x.lineStart(first.Pos()), End: x.lineEnd(first.End()), NewText: ""})
			reads = remove(reads, first.Rhs[0].(*ast.IndexExpr))
		} else {
			value = x.unused(singular(ix.over.Name), l)
		}
		for _, r := range reads {
			edits = append(edits, edit.TextEdit{Start: x.Off(r.Pos()), End: x.Off(r.End()), NewText: value})
		}
	}
	key := "_"
	if keyUsed {
		key = ix.index.Name()
	}
	var head string
	switch {
	case value != "":
		head = fmt.Sprintf("for %s, %s := range %s", key, value, ix.over.Name)
	case keyUsed:
		head = fmt.Sprintf("for %s := range %s", key, ix.over.Name)
	default:
		head = fmt.Sprintf("for range %s", ix.over.Name)
	}
	edits = append(edits, edit.TextEdit{Start: x.Off(l.For), End: x.Off(l.Body.Lbrace), NewText: head + " "})
	return head, edits, nil
}

// rangeToIndex turns a range over a slice or an array into an index
// loop, the element assigned first in the body.
func (x *conversion) rangeToIndex(l *ast.RangeStmt) (string, []edit.TextEdit, error) {
	t := x.Info.TypeOf(l.X)
	switch u := t.Underlying().(type) {
	case *types.Map:
		return "", nil, refactor.Refusef("the loop at line %d ranges over a map, which has no index", x.line)
	case *types.Chan:
		return "", nil, refactor.Refusef("the loop at line %d ranges over a channel, which has no index", x.line)
	case *types.Basic:
		if u.Info()&types.IsString != 0 {
			return "", nil, refactor.Refusef("the loop at line %d ranges over a string, which yields runes at byte offsets; an index loop would read bytes", x.line)
		}
		return "", nil, refactor.Refusef("the loop at line %d ranges over an integer; it is already counted", x.line)
	case *types.Signature:
		return "", nil, refactor.Refusef("the loop at line %d ranges over a function iterator, which has no index", x.line)
	}
	if !x.walkable(t) {
		return "", nil, refactor.Refusef("the loop at line %d ranges over %s, which is not a slice or an array", x.line, t)
	}
	if l.Tok == token.ASSIGN {
		return "", nil, refactor.Refusef("the loop at line %d assigns variables declared outside it (=, not :=); declare them in the loop first", x.line)
	}
	over, _ := l.X.(*ast.Ident)
	if over == nil {
		return "", nil, refactor.Refusef("the loop at line %d ranges over %s, which a range evaluates once and an index loop on each pass. Put it in a local first (extract_variable)", x.line, x.Text(l.X))
	}
	if err := x.stable(over, l.Body); err != nil {
		return "", nil, err
	}
	if analysis.IsArray(t) && x.changesElements(over, l.Body) {
		return "", nil, refactor.Refusef("the loop at line %d ranges over the array %s and changes its elements; a range reads a copy of the array, an index loop the array itself", x.line, over.Name)
	}
	key := name(l.Key)
	var objs []*types.Var
	for _, e := range []ast.Expr{l.Key, l.Value} {
		if id, _ := e.(*ast.Ident); id != nil {
			if v, ok := x.Info.Defs[id].(*types.Var); ok {
				objs = append(objs, v)
			}
		}
	}
	for _, v := range objs {
		if err := x.notShared(l, v); err != nil {
			return "", nil, err
		}
	}
	if key == "" || key == "_" {
		key = x.unused("i", l)
	}
	head := fmt.Sprintf("for %s := 0; %s < len(%s); %s++", key, key, over.Name, key)
	edits := []edit.TextEdit{{Start: x.Off(l.For), End: x.Off(l.Body.Lbrace), NewText: head + " "}}
	if v := name(l.Value); v != "" && v != "_" {
		indent := x.indentOf(l.Pos()) + "\t"
		at := x.Off(l.Body.Lbrace) + 1
		edits = append(edits, edit.TextEdit{Start: at, End: at, NewText: fmt.Sprintf("\n%s%s := %s[%s]", indent, v, over.Name, key)})
	}
	return head, edits, nil
}

// stable refuses when over can be another slice or array on a later pass:
// it must be a local variable the body does not assign or take the
// address of.
func (x *conversion) stable(over *ast.Ident, body *ast.BlockStmt) error {
	v, ok := x.Info.Uses[over].(*types.Var)
	if !ok || v.Parent() == nil || v.Parent() == v.Pkg().Scope() || v.IsField() {
		return refactor.Refusef("the loop at line %d walks %s, which is not a local variable; a range reads it once and an index loop on each pass. Put it in a local first (extract_variable)", x.line, over.Name)
	}
	return x.notAssigned(body, v)
}

// notAssigned refuses when body assigns v or takes its address.
func (x *conversion) notAssigned(body *ast.BlockStmt, v *types.Var) error {
	var err error
	ast.Inspect(body, func(n ast.Node) bool {
		if err != nil {
			return false
		}
		switch n := n.(type) {
		case *ast.AssignStmt:
			for _, l := range n.Lhs {
				if x.is(l, v) {
					err = refactor.Refusef("the loop at line %d assigns %s (line %d)", x.line, v.Name(), x.TF.Line(n.Pos()))
				}
			}
		case *ast.IncDecStmt:
			if x.is(n.X, v) {
				err = refactor.Refusef("the loop at line %d assigns %s (line %d)", x.line, v.Name(), x.TF.Line(n.Pos()))
			}
		case *ast.UnaryExpr:
			if n.Op == token.AND && x.is(n.X, v) {
				err = refactor.Refusef("the loop at line %d takes the address of %s (line %d)", x.line, v.Name(), x.TF.Line(n.Pos()))
			}
		}
		return true
	})
	return err
}

// notShared refuses, before Go 1.22, when a function literal in the
// loop's body captures v: every pass then shares the variable, and the
// two forms leave it with different values when the loop ends.
func (x *conversion) notShared(l ast.Stmt, v *types.Var) error {
	if fv := x.Info.FileVersions[x.File]; fv == "" || version.Compare(fv, "go1.22") >= 0 {
		return nil
	}
	var err error
	ast.Inspect(l, func(n ast.Node) bool {
		lit, ok := n.(*ast.FuncLit)
		if !ok || err != nil {
			return err == nil
		}
		ast.Inspect(lit, func(m ast.Node) bool {
			if id, ok := m.(*ast.Ident); ok && x.Info.Uses[id] == v {
				err = refactor.Refusef("a function literal at line %d captures %s, which every pass shares before Go 1.22 (this file is %s); the two forms leave it with different values. Copy it into a local in the body first", x.TF.Line(lit.Pos()), v.Name(), x.Info.FileVersions[x.File])
			}
			return err == nil
		})
		return false
	})
	return err
}

// written says how n, an element of the walked slice, is written, or "".
func (x *conversion) written(n ast.Expr, parents map[ast.Node]ast.Node) string {
	var e ast.Node = n
	for {
		p := parents[e]
		switch p := p.(type) {
		case *ast.SelectorExpr:
			if p.X != e {
				return ""
			}
			if sel := x.Info.Selections[p]; sel != nil && sel.Kind() == types.MethodVal && analysis.PtrRecv(sel) {
				return "a method with a pointer receiver"
			}
			e = p
			continue
		case *ast.IndexExpr:
			if p.X != e {
				return ""
			}
			if !analysis.IsArray(x.Info.TypeOf(p.X)) {
				return ""
			}
			e = p
			continue
		case *ast.ParenExpr:
			e = p
			continue
		case *ast.AssignStmt:
			for _, l := range p.Lhs {
				if l == e {
					return "an assignment"
				}
			}
		case *ast.IncDecStmt:
			return "an increment"
		case *ast.UnaryExpr:
			if p.Op == token.AND {
				return "its address taken"
			}
		}
		return ""
	}
}

// changesElements reports whether body writes an element of the array
// over, or takes its address.
func (x *conversion) changesElements(over *ast.Ident, body *ast.BlockStmt) bool {
	o := x.Info.Uses[over]
	changed := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			for _, l := range n.Lhs {
				changed = changed || x.rootedAt(l, o)
			}
		case *ast.IncDecStmt:
			changed = changed || x.rootedAt(n.X, o)
		case *ast.UnaryExpr:
			changed = changed || (n.Op == token.AND && x.rootedAt(n.X, o))
		}
		return !changed
	})
	return changed
}

// rootedAt reports whether e is o or an element or field of it.
func (x *conversion) rootedAt(e ast.Expr, o types.Object) bool {
	for {
		switch t := e.(type) {
		case *ast.Ident:
			return x.Info.Uses[t] == o
		case *ast.IndexExpr:
			e = t.X
		case *ast.SelectorExpr:
			e = t.X
		case *ast.ParenExpr:
			e = t.X
		default:
			return false
		}
	}
}

// firstCopy returns the body's first statement when it is v := xs[i], one
// of reads, and v is not assigned later, with v's name.
func (x *conversion) firstCopy(body *ast.BlockStmt, reads []*ast.IndexExpr) (*ast.AssignStmt, string) {
	if len(body.List) == 0 {
		return nil, ""
	}
	a, ok := body.List[0].(*ast.AssignStmt)
	if !ok || a.Tok != token.DEFINE || len(a.Lhs) != 1 || len(a.Rhs) != 1 {
		return nil, ""
	}
	id, _ := a.Lhs[0].(*ast.Ident)
	r, _ := a.Rhs[0].(*ast.IndexExpr)
	if id == nil || r == nil || id.Name == "_" || !contains(reads, r) {
		return nil, ""
	}
	v, _ := x.Info.Defs[id].(*types.Var)
	if v == nil || x.notAssigned(body, v) != nil {
		return nil, ""
	}
	return a, id.Name
}

// walkable reports whether t is a slice or an array.
func (x *conversion) walkable(t types.Type) bool {
	return t != nil && (analysis.IsSlice(t) || analysis.IsArray(t))
}

// is reports whether e is the identifier of v.
func (x *conversion) is(e ast.Expr, v types.Object) bool {
	id, ok := e.(*ast.Ident)
	return ok && x.Info.Uses[id] == v
}

// builtin reports whether e names the builtin function name.
func (x *conversion) builtin(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	if !ok {
		return false
	}
	b, ok := x.Info.Uses[id].(*types.Builtin)
	return ok && b.Name() == name
}

// unused returns name, or name with a number, that no scope at the loop
// and no identifier in it already uses.
func (x *conversion) unused(name string, l ast.Stmt) string {
	taken := map[string]bool{}
	ast.Inspect(l, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			taken[id.Name] = true
		}
		return true
	})
	scope := x.Pkg.Types.Scope().Innermost(l.Pos())
	free := func(n string) bool {
		if taken[n] {
			return false
		}
		if scope == nil {
			return true
		}
		_, o := scope.LookupParent(n, l.Pos())
		return o == nil
	}
	if free(name) {
		return name
	}
	for k := 2; ; k++ {
		if n := fmt.Sprintf("%s%d", name, k); free(n) {
			return n
		}
	}
}

// singular names an element of xs: a plural's singular (names to name,
// entries to entry), a List or Slice suffix dropped, else item.
func singular(xs string) string {
	s := ""
	switch {
	case strings.HasSuffix(xs, "List") && len(xs) > 4:
		s = xs[:len(xs)-4]
	case strings.HasSuffix(xs, "Slice") && len(xs) > 5:
		s = xs[:len(xs)-5]
	case strings.HasSuffix(xs, "ies") && len(xs) > 3:
		s = xs[:len(xs)-3] + "y"
	case strings.HasSuffix(xs, "s") && !strings.HasSuffix(xs, "ss") && len(xs) > 1:
		s = xs[:len(xs)-1]
	}
	if s == "" || token.IsKeyword(s) {
		return "item"
	}
	return s
}

// name returns the identifier's name, or "" for none.
func name(e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

func (x *conversion) indentOf(p token.Pos) string {
	start := x.Off(x.TF.LineStart(x.TF.Line(p)))
	ws := x.Src[start:x.Off(p)]
	for i, c := range ws {
		if c != ' ' && c != '\t' {
			return string(ws[:i])
		}
	}
	return string(ws)
}

// lineStart is the offset of the start of p's line.
func (x *conversion) lineStart(p token.Pos) int { return x.Off(x.TF.LineStart(x.TF.Line(p))) }

// lineEnd is the offset just past the newline that ends the line of p.
func (x *conversion) lineEnd(p token.Pos) int {
	off := x.Off(p)
	for off < len(x.Src) && x.Src[off] != '\n' {
		off++
	}
	if off < len(x.Src) {
		off++
	}
	return off
}

func contains(rs []*ast.IndexExpr, r *ast.IndexExpr) bool {
	for _, x := range rs {
		if x == r {
			return true
		}
	}
	return false
}

func remove(rs []*ast.IndexExpr, r *ast.IndexExpr) []*ast.IndexExpr {
	var out []*ast.IndexExpr
	for _, x := range rs {
		if x != r {
			out = append(out, x)
		}
	}
	return out
}
