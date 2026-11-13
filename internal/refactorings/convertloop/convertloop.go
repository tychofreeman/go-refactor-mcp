// Package convertloop implements "convert_loop": a loop rewritten as
// another statement form that runs the same code in the same order.
//
//	for i := 0; i < len(names); i++ {        for _, name := range names {
//		fmt.Println(names[i])          <=>        fmt.Println(name)
//	}                                        }
//
// The pairs are those java-refactor-mcp's convert_loop has where Go has
// both forms: an index loop and a range loop (to=for_each and to=for),
// and a for with init and post statements and one with only a condition,
// Go's while (to=while and to=for).
package convertloop

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"go/version"
	"sort"
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
	return `Rewrite a loop as another statement form that runs the same code in the same order. to=for_each turns an index loop over a slice or an array, for i := 0; i < len(names); i++ { fmt.Println(names[i]) }, into for _, name := range names { fmt.Println(name) }, keeping i as the key when the body still uses it; to=for turns a range over a slice or an array back into an index loop, the element assigned first in the body. to=while turns for i := 0; i < n; i++ into i := 0 followed by for i < n, with i++ last in the body and before each continue; to=for turns such a loop back, the variable declared just before it and the body's last assignment moving into its header.
Identify the loop by file and the line of its for keyword (column if the line has two).`
}

type args struct {
	File   string `json:"file" desc:"Path of the file, relative to the workspace root."`
	Line   int    `json:"line" desc:"1-based line of the for keyword."`
	Column int    `json:"column,omitempty" desc:"Optional 1-based column of the for keyword, only needed if the line has more than one loop."`
	To     string `json:"to" desc:"The form to write: for_each (a range loop), for (an index loop, or a for with init and post statements) or while (a for with only a condition)."`
}

type conversion struct {
	site.Site
	path  string
	line  int
	notes []string
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
	case a.To != "for_each" && a.To != "for" && a.To != "while":
		return nil, refactor.Refusef("to is for_each, for or while; to=%q is not built for Go", a.To)
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
		onlyCond := l.Init == nil && l.Post == nil
		switch {
		case onlyCond && a.To == "for":
			summary, edits, err = x.whileToFor(l)
		case onlyCond && a.To == "while":
			return nil, refactor.Refusef("the loop at line %d already has only a condition", a.Line)
		case onlyCond:
			return nil, refactor.Refusef("the loop at line %d has only a condition; turn it into a for with init and post statements first (to=for), then into a range", a.Line)
		case a.To == "for_each":
			summary, edits, err = x.indexToRange(l)
		case a.To == "while":
			summary, edits, err = x.forToWhile(l)
		default:
			return nil, refactor.Refusef("the loop at line %d already has init and post statements; to=for_each turns it into a range, to=while into a loop with only a condition", a.Line)
		}
	case *ast.RangeStmt:
		if a.To != "for" {
			return nil, refactor.Refusef("the loop at line %d is a range loop; to=for turns it into an index loop, which to=while then takes", a.Line)
		}
		summary, edits, err = x.rangeToIndex(l)
	}
	if err != nil {
		return nil, err
	}
	return &refactor.Plan{
		Summary: fmt.Sprintf("rewrote the loop at line %d as %s", a.Line, summary),
		Edits:   []edit.FileEdit{{Path: x.path, Edits: edits}},
		Notes:   x.notes,
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

// forToWhile turns for init; cond; post into init followed by a loop
// with only cond: post goes last in the body and before each continue of
// the loop. The two go in a block when the init's names would clash with
// the block's or be read after the loop.
func (x *conversion) forToWhile(l *ast.ForStmt) (string, []edit.TextEdit, error) {
	whole, list := x.around(l)
	var declared []*types.Var
	if init, ok := l.Init.(*ast.AssignStmt); ok && init.Tok == token.DEFINE {
		for _, e := range init.Lhs {
			if id, ok := e.(*ast.Ident); ok {
				if v, ok := x.Info.Defs[id].(*types.Var); ok {
					declared = append(declared, v)
				}
			}
		}
	}
	if x.perPass() {
		for _, v := range declared {
			if err := x.notCaptured(l, v, "each pass of a for with init and post statements has its own %s since Go 1.22, and a loop with only a condition shares one"); err != nil {
				return "", nil, err
			}
		}
	}
	indent := x.indentOf(l.Pos())
	start := x.lineStart(whole.Pos())
	var inner []edit.TextEdit
	if l.Post != nil {
		post := x.Text(l.Post)
		for _, c := range x.continues(l) {
			inner = append(inner, edit.TextEdit{Start: x.Off(c.Pos()), End: x.Off(c.Pos()), NewText: post + "\n" + x.indentOf(c.Pos())})
		}
		if !analysis.EndsFlow(x.Info, l.Body.List) {
			if x.TF.Line(l.Body.Lbrace) == x.TF.Line(l.Body.Rbrace) {
				inner = append(inner, edit.TextEdit{Start: x.Off(l.Body.Lbrace), End: x.Off(l.Body.End()), NewText: "{\n" + indent + "\t" + post + "\n" + indent + "}"})
			} else {
				at := x.lineStart(l.Body.Rbrace)
				inner = append(inner, edit.TextEdit{Start: at, End: at, NewText: indent + "\t" + post + "\n"})
			}
		}
	}
	head := "for "
	if l.Cond != nil {
		head += x.Text(l.Cond) + " "
	}
	inner = append(inner, edit.TextEdit{Start: x.Off(l.For), End: x.Off(l.Body.Lbrace), NewText: head})
	text := x.applied(start, x.Off(whole.End()), inner)
	if l.Init != nil {
		text = indent + x.Text(l.Init) + "\n" + text
	}
	wrap := false
	for _, v := range declared {
		wrap = wrap || x.clashes(v.Name(), whole, list)
	}
	if wrap {
		lines := strings.Split(text, "\n")
		for k, s := range lines {
			if strings.TrimSpace(s) != "" {
				lines[k] = "\t" + s
			}
		}
		text = indent + "{\n" + strings.Join(lines, "\n") + "\n" + indent + "}"
	}
	summary := strings.TrimSpace(head) + " {"
	if wrap {
		summary += ", in a block with its init"
	}
	return summary, []edit.TextEdit{{Start: start, End: x.Off(whole.End()), NewText: text}}, nil
}

// whileToFor turns a loop with only a condition into one with init and
// post statements: the v := ... just before it, which the condition reads
// and nothing after the loop does, becomes the init, and the body's last
// assignment or increment the post statement, unless a continue of the
// loop would skip it.
func (x *conversion) whileToFor(l *ast.ForStmt) (string, []edit.TextEdit, error) {
	whole, list := x.around(l)
	var edits []edit.TextEdit
	init := ""
	at := -1
	for k, s := range list {
		if s == whole {
			at = k
		}
	}
	if at > 0 {
		if a, ok := list[at-1].(*ast.AssignStmt); ok && a.Tok == token.DEFINE && len(a.Lhs) == 1 && len(a.Rhs) == 1 {
			id, _ := a.Lhs[0].(*ast.Ident)
			v, _ := x.Info.Defs[id].(*types.Var)
			if v != nil && l.Cond != nil && x.reads(l.Cond, v) && !x.readsAny(list[at+1:], v) {
				if x.perPass() {
					if err := x.notCaptured(l, v, "a loop with only a condition shares one %s, and since Go 1.22 each pass of a for with init and post statements has its own"); err != nil {
						return "", nil, err
					}
				}
				init = x.Text(a)
				edits = append(edits, edit.TextEdit{Start: x.lineStart(a.Pos()), End: x.lineEnd(a.End()), NewText: ""})
			}
		}
	}
	post := ""
	if n := len(l.Body.List); n > 0 {
		last := l.Body.List[n-1]
		_, inc := last.(*ast.IncDecStmt)
		a, assign := last.(*ast.AssignStmt)
		if inc || (assign && a.Tok != token.DEFINE) {
			if len(x.continues(l)) == 0 {
				post = x.Text(last)
				edits = append(edits, edit.TextEdit{Start: x.lineStart(last.Pos()), End: x.lineEnd(last.End()), NewText: ""})
			} else {
				x.notes = append(x.notes, fmt.Sprintf("%s stays last in the body: a continue of the loop skips it, and as the post statement it would run", x.Text(last)))
			}
		}
	}
	if init == "" && post == "" {
		return "", nil, refactor.Refusef("the loop at line %d has nothing to move into its header: no variable declared just before it that its condition reads and nothing after it does, and no assignment or increment last in its body", x.line)
	}
	cond := ""
	if l.Cond != nil {
		cond = x.Text(l.Cond)
	}
	head := fmt.Sprintf("for %s; %s; %s", init, cond, post)
	head = strings.TrimRight(head, " ")
	edits = append(edits, edit.TextEdit{Start: x.Off(l.For), End: x.Off(l.Body.Lbrace), NewText: head + " "})
	return head + " {", edits, nil
}

// around returns the loop with its label, if it has one, and the list of
// statements that holds it.
func (x *conversion) around(l ast.Stmt) (ast.Stmt, []ast.Stmt) {
	path := analysis.PathTo(x.File, l)
	var whole ast.Stmt = l
	k := 1
	if lab, ok := path[1].(*ast.LabeledStmt); ok {
		whole = lab
		k = 2
	}
	switch p := path[k].(type) {
	case *ast.BlockStmt:
		return whole, p.List
	case *ast.CaseClause:
		return whole, p.Body
	case *ast.CommClause:
		return whole, p.Body
	}
	return whole, nil
}

// continues returns the continue statements that go to the next pass of
// l: unlabeled ones in no inner loop, and those naming its label.
func (x *conversion) continues(l ast.Stmt) []*ast.BranchStmt {
	label := ""
	if path := analysis.PathTo(x.File, l); len(path) > 1 {
		if lab, ok := path[1].(*ast.LabeledStmt); ok {
			label = lab.Label.Name
		}
	}
	var out []*ast.BranchStmt
	var walk func(n ast.Node, inner bool)
	walk = func(n ast.Node, inner bool) {
		ast.Inspect(n, func(m ast.Node) bool {
			switch m := m.(type) {
			case *ast.FuncLit:
				return false
			case *ast.ForStmt:
				if m != l {
					walk(m.Body, true)
					return false
				}
			case *ast.RangeStmt:
				walk(m.Body, true)
				return false
			case *ast.BranchStmt:
				if m.Tok == token.CONTINUE && ((m.Label == nil && !inner) || (m.Label != nil && m.Label.Name == label)) {
					out = append(out, m)
				}
			}
			return true
		})
	}
	switch l := l.(type) {
	case *ast.ForStmt:
		walk(l.Body, false)
	case *ast.RangeStmt:
		walk(l.Body, false)
	}
	return out
}

// perPass reports whether the file's Go version gives each pass of a for
// with init and post statements its own variables (Go 1.22 and later).
func (x *conversion) perPass() bool {
	fv := x.Info.FileVersions[x.File]
	return fv == "" || version.Compare(fv, "go1.22") >= 0
}

// notCaptured refuses when a function literal in l captures v or l takes
// v's address: the two forms then differ in how many vs there are. why
// says how, with %s for v's name.
func (x *conversion) notCaptured(l ast.Node, v *types.Var, why string) error {
	var err error
	ast.Inspect(l, func(n ast.Node) bool {
		if err != nil {
			return false
		}
		switch n := n.(type) {
		case *ast.FuncLit:
			if x.reads(n, v) {
				err = refactor.Refusef("a function literal at line %d captures %s; "+why+". Copy it into a local in the body first", x.TF.Line(n.Pos()), v.Name(), v.Name())
			}
			return false
		case *ast.UnaryExpr:
			if n.Op == token.AND && x.is(n.X, v) {
				err = refactor.Refusef("line %d takes the address of %s; "+why, x.TF.Line(n.Pos()), v.Name(), v.Name())
			}
		}
		return true
	})
	return err
}

// reads reports whether n uses v.
func (x *conversion) reads(n ast.Node, v types.Object) bool {
	found := false
	ast.Inspect(n, func(m ast.Node) bool {
		if id, ok := m.(*ast.Ident); ok && x.Info.Uses[id] == v {
			found = true
		}
		return !found
	})
	return found
}

// readsAny reports whether any of list uses v.
func (x *conversion) readsAny(list []ast.Stmt, v types.Object) bool {
	for _, s := range list {
		if x.reads(s, v) {
			return true
		}
	}
	return false
}

// clashes reports whether declaring name just before whole, in list,
// would redeclare a name of the block or reach a statement after it that
// names it.
func (x *conversion) clashes(name string, whole ast.Stmt, list []ast.Stmt) bool {
	if list == nil {
		return true
	}
	path := analysis.PathTo(x.File, whole)
	if scope := x.Info.Scopes[path[1]]; scope != nil && scope.Lookup(name) != nil {
		return true
	}
	after := false
	for _, s := range list {
		if s == whole {
			after = true
			continue
		}
		if !after {
			continue
		}
		hit := false
		ast.Inspect(s, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name == name {
				hit = true
			}
			return !hit
		})
		if hit {
			return true
		}
	}
	return false
}

// applied returns the source from start to end with edits, which all fall
// inside it, applied.
func (x *conversion) applied(start, end int, edits []edit.TextEdit) string {
	sort.Slice(edits, func(i, j int) bool { return edits[i].Start > edits[j].Start })
	b := []byte(string(x.Src[start:end]))
	for _, e := range edits {
		b = append(b[:e.Start-start], append([]byte(e.NewText), b[e.End-start:]...)...)
	}
	return string(b)
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
