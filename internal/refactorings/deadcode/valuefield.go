package deadcode

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// Expand makes a removal with value two steps: the field's reads become
// value, a path of the struct's other fields that every literal sets it
// equal to, and its elements go from the literals; then the field, which
// nothing uses any more, goes.
func (*RemoveDeadCode) Expand(ws *workspace.Workspace, raw json.RawMessage) ([]refactor.Step, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.Value == "" {
		return nil, nil
	}
	b := a
	b.Value = ""
	rest, err := json.Marshal(b)
	if err != nil {
		return nil, err
	}
	return []refactor.Step{{Refactoring: readValue{}, Args: raw}, {Refactoring: New(), Args: rest}}, nil
}

// readValue is the first step of remove_dead_code with value: the field's
// reads become value and its elements leave the literals, once every
// literal is shown to set it to value and nothing writes it or the
// fields value reads.
type readValue struct{}

func (readValue) Name() string                 { return "remove_dead_code" }
func (readValue) Description() string          { return "" }
func (readValue) InputSchema() json.RawMessage { return (*RemoveDeadCode)(nil).InputSchema() }

func (readValue) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	pkg, id, err := locate.Ident(ws, locate.IdentQuery{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name})
	if err != nil {
		return nil, err
	}
	f, ok := pkg.TypesInfo.ObjectOf(id).(*types.Var)
	if !ok || !f.IsField() || f.Embedded() {
		return nil, refactor.Refusef("value is for a field that is not embedded; %s is not one", a.Name)
	}
	t, err := findField(ws, pkg, f)
	if err != nil {
		return nil, err
	}
	named, ok := t.typeName.Type().(*types.Named)
	if !ok {
		return nil, refactor.Refusef("%s is not a field of a named struct type", f.Name())
	}
	path, err := valuePath(named, f, a.Value)
	if err != nil {
		return nil, err
	}
	watched := map[types.Object]bool{f: true}
	for _, p := range path {
		watched[p] = true
	}
	key := analysis.ObjectKey(ws.Fset, named.Obj())
	isT := func(t types.Type) bool {
		if p, ok := t.(*types.Pointer); ok {
			t = p.Elem()
		}
		n, ok := t.(*types.Named)
		return ok && analysis.ObjectKey(ws.Fset, n.Origin().Obj()) == key
	}
	same := func(o types.Object) types.Object {
		for w := range watched {
			if analysis.ObjectKey(ws.Fset, w) == analysis.ObjectKey(ws.Fset, o) {
				return w
			}
		}
		return nil
	}
	byFile := map[string][]edit.TextEdit{}
	seen := map[string]bool{} // edits by position, across package variants
	add := func(p token.Pos, e edit.TextEdit) {
		pos := ws.Fset.Position(p)
		if k := fmt.Sprintf("%s:%d", pos.Filename, e.Start); !seen[k] {
			seen[k] = true
			byFile[pos.Filename] = append(byFile[pos.Filename], e)
		}
	}
	reads, lits := 0, 0
	for _, p := range ws.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		info := p.TypesInfo
		for _, file := range p.Syntax {
			src, err := os.ReadFile(ws.Fset.File(file.Pos()).Name())
			if err != nil {
				return nil, err
			}
			off := func(p token.Pos) int { return ws.Fset.Position(p).Offset }
			var why error
			// Writes to the field, or to what value reads, outside literals:
			// a selector on the path of what is written (x.f, x.f.g, x.f[i]).
			written := func(x ast.Expr) {
				for x != nil && why == nil {
					switch e := ast.Unparen(x).(type) {
					case *ast.SelectorExpr:
						if s := info.Selections[e]; s != nil && same(s.Obj()) != nil {
							why = refactor.Refusef("%s is written at %s, so %s could then differ from %s", types.ExprString(e), ws.Position(e.Pos()), f.Name(), a.Value)
						}
						x = e.X
					case *ast.IndexExpr:
						x = e.X
					case *ast.StarExpr:
						x = e.X
					default:
						x = nil
					}
				}
			}
			ast.Inspect(file, func(n ast.Node) bool {
				if why != nil {
					return false
				}
				switch n := n.(type) {
				case *ast.AssignStmt:
					for _, l := range n.Lhs {
						written(l)
					}
				case *ast.IncDecStmt:
					written(n.X)
				case *ast.RangeStmt:
					if n.Tok == token.ASSIGN {
						if n.Key != nil {
							written(n.Key)
						}
						if n.Value != nil {
							written(n.Value)
						}
					}
				case *ast.UnaryExpr:
					if n.Op == token.AND {
						written(n.X)
					}
				case *ast.SelectorExpr:
					if s := info.Selections[n]; s != nil && same(s.Obj()) == f {
						add(n.Pos(), edit.TextEdit{Start: off(n.Sel.Pos()), End: off(n.Sel.End()), NewText: a.Value})
						reads++
					}
				case *ast.CompositeLit:
					if !isT(info.TypeOf(n)) {
						return true
					}
					e, err := literalEdit(ws, info, n, f, path, a.Value, src, same)
					if err != nil {
						why = err
						return false
					}
					add(n.Pos(), e)
					lits++
				}
				return true
			})
			if why != nil {
				return nil, why
			}
		}
	}
	plan := &refactor.Plan{Summary: fmt.Sprintf("wrote %s for %s at %d read%s, and deleted it from %d literal%s", a.Value, f.Name(), reads, plural(reads), lits, plural(lits))}
	for path, es := range byFile {
		plan.Edits = append(plan.Edits, edit.FileEdit{Path: path, Edits: es})
	}
	plan.Edits = edit.Merge(plan.Edits)
	return plan, nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// valuePath resolves value, a path of named's fields ("WS", "Site.WS"),
// to the fields it goes through, promoted ones included; the last must
// have f's type, and none may be f or go through a pointer, which a read
// at construction would dereference.
func valuePath(named *types.Named, f *types.Var, value string) ([]*types.Var, error) {
	e, err := parser.ParseExpr(value)
	if err != nil {
		return nil, refactor.Refusef("value %q is not a path of fields: %v", value, err)
	}
	var names []string
	for {
		switch x := e.(type) {
		case *ast.Ident:
			names = append([]string{x.Name}, names...)
		case *ast.SelectorExpr:
			names = append([]string{x.Sel.Name}, names...)
			e = x.X
			continue
		default:
			return nil, refactor.Refusef("value %s is not a path of fields, like WS or Site.WS", value)
		}
		break
	}
	var path []*types.Var
	var t types.Type = named
	for _, name := range names {
		if _, ok := t.Underlying().(*types.Pointer); ok {
			return nil, refactor.Refusef("value %s goes through a pointer, which each literal would dereference; only fields of values are supported", value)
		}
		obj, idx, _ := types.LookupFieldOrMethod(t, false, f.Pkg(), name)
		v, ok := obj.(*types.Var)
		if !ok || !v.IsField() {
			return nil, refactor.Refusef("%s has no field %s on the path %s", types.TypeString(t, nil), name, value)
		}
		// The fields it is promoted through, then it.
		st := t
		for _, i := range idx {
			s, ok := st.Underlying().(*types.Struct)
			if !ok {
				return nil, refactor.Refusef("value %s goes through a pointer, which each literal would dereference; only fields of values are supported", value)
			}
			path = append(path, s.Field(i))
			st = s.Field(i).Type()
		}
		t = v.Type()
	}
	for _, p := range path {
		if p == f {
			return nil, refactor.Refusef("value %s reads %s itself", value, f.Name())
		}
	}
	if !types.Identical(t, f.Type()) {
		return nil, refactor.Refusef("value %s is of type %s, not %s's type %s", value, types.TypeString(t, nil), f.Name(), types.TypeString(f.Type(), nil))
	}
	return path, nil
}

// literalEdit returns the edit that deletes f's element from lit, a
// literal of its struct, after checking that the element is value in the
// literal's terms: the element of path's first field, then the rest of
// path.
func literalEdit(ws *workspace.Workspace, info *types.Info, lit *ast.CompositeLit, f *types.Var, path []*types.Var, value string, src []byte, same func(types.Object) types.Object) (edit.TextEdit, error) {
	at := ws.Position(lit.Pos())
	if len(lit.Elts) > 0 {
		if _, kv := lit.Elts[0].(*ast.KeyValueExpr); !kv {
			return edit.TextEdit{}, refactor.Refusef("the literal at %s has no keys; give it keys first", at)
		}
	}
	text := func(e ast.Expr) string {
		return string(src[ws.Fset.Position(e.Pos()).Offset:ws.Fset.Position(e.End()).Offset])
	}
	var mine, first *ast.KeyValueExpr
	var i int
	for j, e := range lit.Elts {
		kv := e.(*ast.KeyValueExpr)
		k, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		o := same(info.ObjectOf(k))
		switch {
		case o == f:
			mine, i = kv, j
		case o == path[0]:
			first = kv
		}
	}
	switch {
	case mine == nil:
		return edit.TextEdit{}, refactor.Refusef("the literal at %s leaves %s zero, not %s; give it %s first", at, f.Name(), value, f.Name())
	case first == nil:
		return edit.TextEdit{}, refactor.Refusef("the literal at %s leaves %s zero, so %s would read its zero value; give it %s first", at, path[0].Name(), value, path[0].Name())
	}
	if !operand(first.Value) {
		return edit.TextEdit{}, refactor.Refusef("the literal at %s gives %s for %s, which is not a name or a path of fields, so %s may not read the same as %s", at, text(first.Value), path[0].Name(), value, f.Name())
	}
	want := text(first.Value)
	for _, p := range path[1:] {
		want += "." + p.Name()
	}
	got, err1 := locate.Tokens([]byte(text(mine.Value)))
	exp, err2 := locate.Tokens([]byte(want))
	if err1 != nil || err2 != nil || !locate.EqualTokens(got, exp) {
		return edit.TextEdit{}, refactor.Refusef("the literal at %s sets %s to %s, not %s (%s in its terms)", at, f.Name(), text(mine.Value), want, value)
	}
	off := func(p token.Pos) int { return ws.Fset.Position(p).Offset }
	from, to := off(mine.Pos()), off(mine.End())
	if i+1 < len(lit.Elts) {
		to = off(lit.Elts[i+1].Pos())
	} else if i > 0 {
		from = off(lit.Elts[i-1].End())
	}
	// A line of its own goes whole.
	ls := strings.LastIndexByte(string(src[:off(mine.Pos())]), '\n') + 1
	if strings.TrimSpace(string(src[ls:off(mine.Pos())])) == "" {
		if j := strings.IndexByte(string(src[off(mine.End()):]), '\n'); j >= 0 && strings.TrimSpace(strings.TrimPrefix(string(src[off(mine.End()):off(mine.End())+j]), ",")) == "" {
			from, to = ls, off(mine.End())+j+1
		}
	}
	return edit.TextEdit{Start: from, End: to}, nil
}

// operand reports whether e is a name or a path of fields of one, which
// reads the same each time.
func operand(e ast.Expr) bool {
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		return true
	case *ast.SelectorExpr:
		return operand(x.X)
	}
	return false
}
