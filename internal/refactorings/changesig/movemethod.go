package changesig

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// MoveMethod is the "move_method" refactoring.
type MoveMethod struct{ refactor.Named[moveMethodArgs] }

// NewMoveMethod returns the move_method refactoring.
func NewMoveMethod() *MoveMethod {
	return &MoveMethod{Named: refactor.Called[moveMethodArgs]("move_method")}
}

func (*MoveMethod) Description() string {
	return `Move a method to a struct type that embeds its receiver's type, and every call follows: with type Bed struct { Soil; name string }, moving Soil.acid to Bed makes func (s *Soil) acid() bool { return s.ph < 6 } func (s *Bed) acid() bool { return s.ph < 6 }, b.Soil.acid() b.acid(), and b.acid() stays. Uses of the receiver go through the embedded field where they must (check(s) becomes check(&s.Soil)). Every call must reach the method through the new type: also names more methods of the type to move with it, such as those that call it on their receiver.
To move a method to a parameter's type instead, use method_to_function, then function_to_method with receiver.
Refuses, saying why: a call on a value no struct of the new type holds, a type that does not embed the receiver's, a name the new type already has, a method some interface has.`
}

// ChecksByCompiling makes a compile error after the move a refusal: the
// last precondition is that every use still compiles.
func (*MoveMethod) ChecksByCompiling() {}

type moveMethodArgs struct {
	File   string   `json:"file" desc:"Path of a file containing the method's declaration or a call, relative to the workspace root."`
	Line   int      `json:"line,omitempty" desc:"1-based line of that occurrence. Omit it to find name as a symbol of the package of file."`
	Name   string   `json:"name" desc:"The method: Type.Method without line, the method's name with it."`
	Column int      `json:"column,omitempty" desc:"Optional 1-based column, only needed to disambiguate."`
	To     string   `json:"to" desc:"The struct type to move it to: one of the package's that embeds the receiver's type."`
	Also   []string `json:"also,omitempty" desc:"Other methods of the same type to move with it (Method or Type.Method)."`
}

// methodMove is one planning run: the methods that move, from their
// receiver type to the type that embeds it, through field.
type methodMove struct {
	ws       *workspace.Workspace
	from, to *types.Named
	field    string // the embedded field of to that holds from
	byPtr    bool   // to embeds *from
	moving   []*target
	isMoving map[string]bool // by keyOf
	srcs     map[string][]byte
}

// Plan checks preconditions and computes the edits.
func (*MoveMethod) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a moveMethodArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	t, err := findTarget(ws, locate.IdentQuery{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name})
	if err != nil {
		return nil, err
	}
	recv := t.obj.Signature().Recv()
	if recv == nil {
		return nil, refactor.Refusef("%s is a function, not a method; function_to_method makes it one", t.obj.Name())
	}
	from := recvNamed(recv.Type())
	if from == nil || from.TypeParams().Len() > 0 {
		return nil, refactor.Refusef("the receiver of %s is not a named type without type parameters", t.obj.Name())
	}
	mv := &methodMove{ws: ws, from: from, isMoving: map[string]bool{}, srcs: map[string][]byte{}}
	if err := mv.findTo(a.To); err != nil {
		return nil, err
	}
	mv.add(t)
	for _, n := range a.Also {
		if !strings.Contains(n, ".") {
			n = from.Obj().Name() + "." + n
		}
		m, err := findTarget(ws, locate.IdentQuery{File: a.File, Name: n})
		if err != nil {
			return nil, err
		}
		if r := m.obj.Signature().Recv(); r == nil || recvNamed(r.Type()) == nil || typeKey(recvNamed(r.Type())) != typeKey(from) {
			return nil, refactor.Refusef("%s is not a method of %s; also names methods of the same type", n, from.Obj().Name())
		}
		mv.add(m)
	}
	var fes []edit.FileEdit
	var names []string
	for _, m := range mv.moving {
		if err := mv.check(m); err != nil {
			return nil, err
		}
		es, err := mv.declEdits(m)
		if err != nil {
			return nil, err
		}
		fes = append(fes, edit.FileEdit{Path: ws.Fset.Position(m.decl.Pos()).Filename, Edits: es})
		for _, c := range m.calls {
			ce, err := mv.callEdits(m, c)
			if err != nil {
				return nil, err
			}
			if ce != nil {
				fes = append(fes, *ce)
			}
		}
		names = append(names, m.obj.Name())
	}
	return t.newPlan(fmt.Sprintf("moved %s from %s to %s", strings.Join(names, ", "), from.Obj().Name(), mv.to.Obj().Name()), fes), nil
}

func (mv *methodMove) add(m *target) {
	mv.moving = append(mv.moving, m)
	mv.isMoving[keyOf(mv.ws.Fset, m.obj)] = true
}

// findTo resolves name, a struct type of from's package that embeds from
// or *from.
func (mv *methodMove) findTo(name string) error {
	tn, ok := mv.from.Obj().Pkg().Scope().Lookup(name).(*types.TypeName)
	if !ok {
		return refactor.Refusef("package %s declares no type %s", mv.from.Obj().Pkg().Name(), name)
	}
	to, ok := tn.Type().(*types.Named)
	if !ok {
		return refactor.Refusef("%s is not a named type", name)
	}
	st, ok := to.Underlying().(*types.Struct)
	if !ok {
		return refactor.Refusef("%s is not a struct type", name)
	}
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		if !f.Embedded() {
			continue
		}
		ft, ptr := f.Type(), false
		if p, ok := ft.(*types.Pointer); ok {
			ft, ptr = p.Elem(), true
		}
		if n, ok := ft.(*types.Named); ok && typeKey(n) == typeKey(mv.from) {
			mv.to, mv.field, mv.byPtr = to, f.Name(), ptr
			return nil
		}
	}
	return refactor.Refusef("%s does not embed %s, so calls could not reach the method there; to move it to a parameter's type, use method_to_function, then function_to_method with receiver", name, mv.from.Obj().Name())
}

// check refuses moving m: a method an interface may need, or a name the
// new type already has.
func (mv *methodMove) check(m *target) error {
	name := m.obj.Name()
	if m.obj.Exported() && analysis.MethodInterface(mv.ws.Pkgs, mv.from, name, false) != "" {
		return refactor.Refusef("%s may be how %s implements %s, which code checks for at run time; this is not supported", name, mv.from.Obj().Name(), analysis.MethodInterface(mv.ws.Pkgs, mv.from, name, false))
	}
	if err := m.checkNoInterface(name); err != nil {
		return err
	}
	st := mv.to.Underlying().(*types.Struct)
	for i := 0; i < st.NumFields(); i++ {
		if st.Field(i).Name() == name {
			return refactor.Refusef("%s already has a field %s", mv.to.Obj().Name(), name)
		}
	}
	for i := 0; i < mv.to.NumMethods(); i++ {
		if mv.to.Method(i).Name() == name {
			return refactor.Refusef("%s already has a method %s", mv.to.Obj().Name(), name)
		}
	}
	if !analysis.IsPointer(m.obj.Signature().Recv().Type()) && mv.byPtr {
		return refactor.Refusef("%s has a value receiver and %s holds a *%s; the method would see the value it points to, not a copy, so this is not supported", name, mv.to.Obj().Name(), mv.from.Obj().Name())
	}
	return nil
}

// declEdits make m's receiver the new type and send the receiver's uses
// through the embedded field where they must go: as a value, or a
// selector the new type resolves to something else.
func (mv *methodMove) declEdits(m *target) ([]edit.TextEdit, error) {
	field := m.decl.Recv.List[0]
	typ := mv.to.Obj().Name()
	ptr := analysis.IsPointer(m.obj.Signature().Recv().Type())
	if ptr {
		typ = "*" + typ
	}
	es := []edit.TextEdit{{Start: m.Off(field.Type.Pos()), End: m.Off(field.Type.End()), NewText: typ}}
	if len(field.Names) == 0 || m.decl.Body == nil {
		return es, nil
	}
	info := m.Pkg.TypesInfo
	rv := info.Defs[field.Names[0]]
	holder := field.Names[0].Name + "." + mv.field
	if ptr && !mv.byPtr {
		holder = "&" + holder
	}
	var newRecv types.Type = mv.to
	if ptr {
		newRecv = types.NewPointer(mv.to)
	}
	parents := map[ast.Node]ast.Node{}
	var stack []ast.Node
	ast.Inspect(m.decl.Body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if len(stack) > 0 {
			parents[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})
	ast.Inspect(m.decl.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || rv == nil || info.Uses[id] != rv {
			return true
		}
		if se, ok := parents[id].(*ast.SelectorExpr); ok && se.X == id {
			sel := info.Selections[se]
			if sel == nil || mv.isMoving[keyOf(mv.ws.Fset, sel.Obj())] {
				return true
			}
			o, _, _ := types.LookupFieldOrMethod(newRecv, true, m.Pkg.Types, se.Sel.Name)
			if o == nil || keyOf(mv.ws.Fset, o) != keyOf(mv.ws.Fset, sel.Obj()) {
				es = append(es, edit.TextEdit{Start: m.Off(se.Sel.Pos()), End: m.Off(se.Sel.Pos()), NewText: mv.field + "."})
			}
			return true
		}
		text := holder
		switch parents[id].(type) {
		case *ast.CallExpr, *ast.ReturnStmt, *ast.AssignStmt, *ast.KeyValueExpr, *ast.CompositeLit, *ast.ValueSpec:
		default:
			if strings.HasPrefix(text, "&") {
				text = "(" + text + ")"
			}
		}
		es = append(es, edit.TextEdit{Start: m.Off(id.Pos()), End: m.Off(id.End()), NewText: text})
		return true
	})
	return es, nil
}

// callEdits rewrite the call c of m, x.m(...): unchanged where x reaches m
// through the new type (or is a moving method's own receiver), and
// y.Field.m(...) becomes y.m(...); any other call is refused.
func (mv *methodMove) callEdits(m *target, c call) (*edit.FileEdit, error) {
	se, ok := c.expr.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, refactor.Refusef("the call at %s does not select %s on a value", mv.ws.Position(c.expr.Pos()), m.obj.Name())
	}
	info := c.pkg.TypesInfo
	if id, ok := se.X.(*ast.Ident); ok {
		if v, ok := info.Uses[id].(*types.Var); ok {
			for _, o := range mv.moving {
				if r := o.decl.Recv.List[0]; len(r.Names) > 0 && keyOf(mv.ws.Fset, o.Pkg.TypesInfo.Defs[r.Names[0]]) == keyOf(mv.ws.Fset, v) {
					return nil, nil // a moving method's own receiver, which becomes the new type
				}
			}
		}
	}
	if mv.reaches(info.TypeOf(se.X)) {
		return nil, nil
	}
	if inner, ok := se.X.(*ast.SelectorExpr); ok && inner.Sel.Name == mv.field && mv.reaches(info.TypeOf(inner.X)) {
		path := mv.ws.Fset.Position(c.expr.Pos()).Filename
		if mv.srcs[path] == nil {
			src, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			mv.srcs[path] = src
		}
		return &edit.FileEdit{Path: path, Edits: []edit.TextEdit{{Start: m.Off(inner.X.End()), End: m.Off(inner.Sel.End())}}}, nil
	}
	return nil, refactor.Refusef("the call at %s calls %s on %s, a %s that no %s holds; every call must reach it through a %s (move the methods that call it with it, in also)",
		mv.ws.Position(c.expr.Pos()), m.obj.Name(), types.ExprString(se.X), mv.from.Obj().Name(), mv.to.Obj().Name(), mv.to.Obj().Name())
}

// reaches reports whether a value of type t reaches the new type: it is
// the type or a pointer to it, or embeds it.
func (mv *methodMove) reaches(t types.Type) bool {
	if t == nil {
		return false
	}
	if n, ok := deref(t).(*types.Named); ok && typeKey(n) == typeKey(mv.to) {
		return true
	}
	return embeds(t, mv.to, map[types.Type]bool{})
}
