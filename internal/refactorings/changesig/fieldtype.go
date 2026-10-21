package changesig

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// ChangeFieldType is the "change_field_type" refactoring.
type ChangeFieldType struct{}

// NewChangeFieldType returns the change_field_type refactoring.
func NewChangeFieldType() *ChangeFieldType { return &ChangeFieldType{} }

func (*ChangeFieldType) Name() string { return "change_field_type" }

func (*ChangeFieldType) Description() string {
	return `Change the type of a struct field, and every use of it in the workspace with it, keeping what every value means.
Use it to give a field a more precise type (a named type such as UserID instead of string), a wider number type, or an interface its type implements.
Identify the field by file and name, Type.Field, without line (or by file, line and the field's name at its declaration or a use), and give the new type as Go source.
The new type must hold every value of the old one: the same underlying type, a number type that holds every value of the old one, or an interface the old type implements. Values written to the field (assigned, in a literal, keyed or not) are converted; reads where the type matters are converted back, as change_parameter_type does for a parameter. A use that cannot follow is listed in the refusal, one per line, and nothing changes: its address taken, a read of an interface-typed field that needs the old type, or ++, -- or += with an interface.
Refuses an embedded field and a field of a generic type.`
}

func (*ChangeFieldType) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":   {"type": "string",  "description": "Path of a file containing the field's declaration or a use, relative to the workspace root."},
    "line":   {"type": "integer", "description": "1-based line of that occurrence. Omit it to find name as Type.Field in the package of file."},
    "name":   {"type": "string",  "description": "Type.Field without line; with line, the field's name."},
    "column": {"type": "integer", "description": "Optional 1-based column, only needed to disambiguate."},
    "type":   {"type": "string",  "description": "The new type, as Go source valid in the declaring file, e.g. UserID or int64."}
  },
  "required": ["file", "name", "type"],
  "additionalProperties": false
}`)
}

type fieldArgs struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column,omitempty"`
	Name   string `json:"name"`
	Type   string `json:"type"`
}

// fieldChange is a field whose type changes, and what its uses need.
type fieldChange struct {
	ws       *workspace.Workspace
	field    *types.Var
	key      string
	old, new types.Type
	widen    bool // new is an interface old implements: nothing is converted
	files    *fileSet
	cannot   []string // uses that cannot follow, "file:line:col: why"
}

// Plan checks preconditions and computes the edits.
func (*ChangeFieldType) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a fieldArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	pkg, id, err := locate.Ident(ws, locate.IdentQuery{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name})
	if err != nil {
		return nil, err
	}
	field, ok := pkg.TypesInfo.ObjectOf(id).(*types.Var)
	if !ok || !field.IsField() {
		return nil, refactor.Refusef("%q at %s is not a struct field", id.Name, ws.Position(id.Pos()))
	}
	field = field.Origin()
	if field.Embedded() {
		return nil, refactor.Refusef("%s is an embedded field; changing its type is not supported", field.Name())
	}
	if !ws.Contains(ws.Fset.Position(field.Pos()).Filename) {
		return nil, refactor.Refusef("%s is declared outside the workspace", field.Name())
	}
	declPkg, decl, err := fieldDecl(ws, field)
	if err != nil {
		return nil, err
	}
	t := &target{ws: ws, pkg: declPkg, obj: types.NewFunc(field.Pos(), field.Pkg(), field.Name(), nil)}
	t.ftype = &ast.FuncType{Params: &ast.FieldList{Closing: decl.End()}}
	newT, err := t.parseType(a.Type)
	if err != nil {
		return nil, err
	}
	if containsTypeParam(field.Type()) || containsTypeParam(newT) {
		return nil, refactor.Refusef("changing a field to or from a type with type parameters is not supported")
	}
	if types.Identical(field.Type(), newT) {
		return nil, refactor.Refusef("%s is already of type %s", field.Name(), t.typeName(newT))
	}
	widen, err := t.checkConversion(field.Type(), newT)
	if err != nil {
		return nil, refactor.Refusef("%s", strings.Replace(err.Error(), "change_parameter_type", "change_field_type", 1))
	}
	c := &fieldChange{ws: ws, field: field, key: keyOf(ws.Fset, field), old: field.Type(), new: newT, widen: widen, files: newFileEdits(ws)}
	if err := c.declEdit(declPkg, decl); err != nil {
		return nil, err
	}
	uses, err := c.uses()
	if err != nil {
		return nil, err
	}
	if len(c.cannot) > 0 {
		return nil, refactor.Refusef("these uses of %s cannot follow its new type %s, so nothing changed:\n  %s", field.Name(), t.typeName(newT), strings.Join(c.cannot, "\n  "))
	}
	plan := &refactor.Plan{
		Summary: fmt.Sprintf("changed field %s from %s to %s (%d use%s)", field.Name(), t.typeName(field.Type()), t.typeName(newT), uses, map[bool]string{true: "", false: "s"}[uses == 1]),
		Edits:   edit.Merge(c.files.all()),
	}
	if analysis.PublicAPI(field, ws.Pkgs) {
		plan.Notes = append(plan.Notes, refactor.APINote(field.Name(), "changing its type"))
	}
	return plan, nil
}

// fieldDecl finds the field list entry that declares field.
func fieldDecl(ws *workspace.Workspace, field *types.Var) (*packages.Package, *ast.Field, error) {
	key := keyOf(ws.Fset, field)
	for _, p := range ws.Pkgs {
		for _, f := range p.Syntax {
			var found *ast.Field
			ast.Inspect(f, func(n ast.Node) bool {
				st, ok := n.(*ast.StructType)
				if !ok || found != nil {
					return found == nil
				}
				for _, fl := range st.Fields.List {
					for _, nm := range fl.Names {
						if keyOf(ws.Fset, p.TypesInfo.Defs[nm]) == key {
							found = fl
						}
					}
				}
				return true
			})
			if found != nil {
				return p, found, nil
			}
		}
	}
	return nil, nil, refactor.Refusef("cannot find the declaration of %s", field.Name())
}

// declEdit gives the field its new type, splitting its group if it
// shares one (n, max int32 to n int64 and max int32, on lines of their
// own).
func (c *fieldChange) declEdit(pkg *packages.Package, f *ast.Field) error {
	fe, err := c.files.of(pkg, f.Pos())
	if err != nil {
		return err
	}
	typ := fe.write(c.new)
	off := func(p token.Pos) int { return c.ws.Fset.Position(p).Offset }
	if len(f.Names) == 1 {
		fe.edits = append(fe.edits, edit.TextEdit{Start: off(f.Type.Pos()), End: off(f.Type.End()), NewText: typ})
		return nil
	}
	old := types.ExprString(f.Type)
	var parts []string
	var rest []string
	for _, n := range f.Names {
		if n.Name == c.field.Name() {
			if len(rest) > 0 {
				parts = append(parts, strings.Join(rest, ", ")+" "+old)
				rest = nil
			}
			parts = append(parts, n.Name+" "+typ)
			continue
		}
		rest = append(rest, n.Name)
	}
	if len(rest) > 0 {
		parts = append(parts, strings.Join(rest, ", ")+" "+old)
	}
	fe.edits = append(fe.edits, edit.TextEdit{Start: off(f.Names[0].Pos()), End: off(f.Type.End()), NewText: strings.Join(parts, "\n")})
	return nil
}

// uses adjusts every use of the field, noting those that cannot follow,
// and returns how many there are.
func (c *fieldChange) uses() (int, error) {
	seen := map[token.Position]bool{}
	n := 0
	for _, p := range c.ws.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, f := range p.Syntax {
			var err error
			ast.Inspect(f, func(node ast.Node) bool {
				if err != nil {
					return false
				}
				switch node := node.(type) {
				case *ast.Ident:
					if keyOf(c.ws.Fset, p.TypesInfo.Uses[node]) != c.key || seen[c.ws.Fset.Position(node.Pos())] {
						return true
					}
					seen[c.ws.Fset.Position(node.Pos())] = true
					n++
					err = c.use(p, f, node)
				case *ast.CompositeLit:
					if len(node.Elts) == 0 || seen[c.ws.Fset.Position(node.Pos())] {
						return true
					}
					if _, keyed := node.Elts[0].(*ast.KeyValueExpr); keyed {
						return true
					}
					st, ok := p.TypesInfo.TypeOf(node).Underlying().(*types.Struct)
					if !ok {
						return true
					}
					for i := range st.NumFields() {
						if keyOf(c.ws.Fset, st.Field(i)) == c.key && i < len(node.Elts) {
							seen[c.ws.Fset.Position(node.Pos())] = true
							n++
							err = c.write(p, node.Elts[i])
						}
					}
				}
				return true
			})
			if err != nil {
				return 0, err
			}
		}
	}
	return n, nil
}

// use adjusts the use id of the field: a key in a literal, or x.F.
func (c *fieldChange) use(p *packages.Package, f *ast.File, id *ast.Ident) error {
	path, _ := astutil.PathEnclosingInterval(f, id.Pos(), id.End())
	if kv, ok := path[1].(*ast.KeyValueExpr); ok && kv.Key == id {
		return c.write(p, kv.Value)
	}
	sel, ok := path[1].(*ast.SelectorExpr)
	if !ok || sel.Sel != id {
		return nil
	}
	path = path[1:]
	var e ast.Expr = sel
	for len(path) > 1 {
		if _, paren := path[1].(*ast.ParenExpr); !paren {
			break
		}
		e, path = path[1].(ast.Expr), path[1:]
	}
	info := p.TypesInfo
	where := c.ws.Position(sel.Sel.Pos())
	switch u := path[1].(type) {
	case *ast.AssignStmt:
		for i, l := range u.Lhs {
			if l != e {
				continue
			}
			switch {
			case len(u.Lhs) != len(u.Rhs):
				c.cannot = append(c.cannot, where+": is assigned another call's result")
			case u.Tok == token.ASSIGN:
				return c.write(p, u.Rhs[i])
			case c.widen:
				c.cannot = append(c.cannot, where+": "+u.Tok.String()+" on an interface")
			default:
				return c.write(p, u.Rhs[i])
			}
			return nil
		}
	case *ast.IncDecStmt:
		if c.widen {
			c.cannot = append(c.cannot, where+": "+u.Tok.String()+" on an interface")
		}
		return nil
	case *ast.UnaryExpr:
		if u.Op == token.AND {
			c.cannot = append(c.cannot, where+": takes its address")
			return nil
		}
	}
	// A read.
	if typeIrrelevant(info, path, types.Identical(c.old.Underlying(), c.new.Underlying())) {
		return nil
	}
	if c.widen {
		if callsMethodOf(info, path, c.new) {
			return nil
		}
		c.cannot = append(c.cannot, where+": uses its value as "+types.TypeString(c.old, nil))
		return nil
	}
	fe, err := c.files.of(p, e.Pos())
	if err != nil {
		return err
	}
	fe.edits = append(fe.edits, (&target{ws: c.ws}).conversion(e, fe.write(c.old))...)
	return nil
}

// write converts v, a value written to the field, to its new type,
// unless it is an untyped constant or the new type is an interface.
func (c *fieldChange) write(p *packages.Package, v ast.Expr) error {
	if c.widen || untypedConstant(p.TypesInfo, v) {
		return nil
	}
	fe, err := c.files.of(p, v.Pos())
	if err != nil {
		return err
	}
	fe.edits = append(fe.edits, (&target{ws: c.ws}).conversion(v, fe.write(c.new))...)
	return nil
}

// callsMethodOf reports whether path[0] is the receiver of a method call
// whose method the interface it has.
func callsMethodOf(info *types.Info, path []ast.Node, it types.Type) bool {
	sel, ok := path[1].(*ast.SelectorExpr)
	if !ok || sel.X != path[0] {
		return false
	}
	obj, _, _ := types.LookupFieldOrMethod(it, false, nil, sel.Sel.Name)
	_, isMethod := obj.(*types.Func)
	return isMethod && info.Selections[sel] != nil
}
