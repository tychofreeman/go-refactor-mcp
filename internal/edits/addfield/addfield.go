// Package addfield implements add_field: an edit that adds fields to a
// struct type.
package addfield

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"strconv"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/site"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
	"golang.org/x/tools/go/packages"
)

// AddField is the "add_field" edit.
type AddField struct{ refactor.Named[args] }

// New returns the add_field edit.
func New() *AddField { return &AddField{Named: refactor.Called[args]("add_field")} }

func (*AddField) Description() string {
	return `Add fields to a struct type: an edit, to give a type the state a feature needs. field "Colour string" with after "Height" adds Colour just after Height in type Plant struct. field is Go source, one or more fields with their comments and tags, or embedded types; without after or before they go at the end. A struct on one line is opened up into lines. Packages the fields' types name are imported. To move a field within its struct, delete it with replace_declaration (Type.Field, empty content), then add it with after; into a new struct, extract_struct.
Identify the struct by file and name, or by line at its declaration or a use.
Unkeyed literals of the struct get the new fields' zero values in their place.
Refused with the compiler's errors: a duplicate name; and an unkeyed literal whose new zero value cannot be written there (a type its file does not import): give it keys first.`
}

// ChangesBehavior marks this as an edit.
func (*AddField) ChangesBehavior() {}

type args struct {
	File    string   `json:"file" desc:"Path of a file containing the struct type's declaration or a use, relative to the workspace root."`
	Line    int      `json:"line,omitempty" desc:"1-based line of that occurrence. Omit it to find name as a symbol of the package of file."`
	Name    string   `json:"name" desc:"The struct type's name; without line, it is found among the package-level names."`
	Column  int      `json:"column,omitempty" desc:"Optional 1-based column, only needed to disambiguate."`
	Field   string   `json:"field" desc:"The fields to add, as Go source, e.g. \"timeout time.Duration // per try\"."`
	Type    string   `json:"type,omitempty" desc:"Optional: the fields' type, when field gives only their names (field \"owner\", type \"string\")."`
	After   string   `json:"after,omitempty" desc:"Optional: a field to put them just after."`
	Before  string   `json:"before,omitempty" desc:"Optional: a field to put them just before."`
	Imports []string `json:"imports,omitempty" desc:"Optional: import paths to use for package names the fields use, when the name alone is ambiguous."`
}

// fieldNames matches a field list without a type: "owner", "x, y".
var fieldNames = regexp.MustCompile(`^\s*\w+(\s*,\s*\w+)*\s*$`)

// Plan checks preconditions and computes the edits.
func (*AddField) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.Type != "" {
		if !fieldNames.MatchString(a.Field) {
			return nil, refactor.Refusef("field %q already gives a type; with type, give only the field's names, e.g. field \"owner\", type %q", a.Field, a.Type)
		}
		a.Field += " " + a.Type
	}
	if a.After != "" && a.Before != "" {
		return nil, refactor.Refusef("give after or before, not both")
	}
	pkg, id, err := locate.Ident(ws, locate.IdentQuery{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name})
	if err != nil {
		return nil, err
	}
	tn, ok := pkg.TypesInfo.ObjectOf(id).(*types.TypeName)
	if !ok {
		return nil, refactor.Refusef("%s at %s is not a type", a.Name, ws.Position(id.Pos()))
	}
	path := ws.Fset.Position(tn.Pos()).Filename
	declaring, err := site.Declaring(ws, tn)
	if err != nil {
		return nil, err
	}
	dpkg, file := declaring.Pkg, declaring.File
	st, found := structOf(file, tn.Pos())
	switch {
	case !found:
		return nil, refactor.Refusef("%s is not declared at the top level of %s", a.Name, ws.Rel(path))
	case st == nil:
		return nil, refactor.Refusef("%s is not declared as a struct type", a.Name)
	}
	code, decls, err := gocode.ParseDeclarations("type _ struct {\n" + a.Field + "\n}")
	if err != nil || len(decls) != 1 {
		return nil, refactor.Refusef("field is not a list of struct fields: %v", err)
	}
	fields := decls[0].(*ast.GenDecl).Specs[0].(*ast.TypeSpec).Type.(*ast.StructType).Fields
	src := declaring.Src
	e, err := insertEdit(ws.Fset.File(file.Pos()), st, src, a.Field, a.Name, a.After, a.Before)
	if err != nil {
		return nil, err
	}
	fills, filled, err := fillUnkeyed(ws, dpkg, tn, st, fields, indexOf(st, a.After, a.Before))
	if err != nil {
		return nil, err
	}
	adder := imports.New(ws, dpkg, file, src)
	added, err := gocode.AddImports(ws, dpkg, file, adder, file.Name.Pos(), code, a.Imports)
	if err != nil {
		return nil, err
	}
	plan := &refactor.Plan{
		Summary:  fmt.Sprintf("added to struct %s in %s", a.Name, ws.Rel(path)),
		Edits:    append([]edit.FileEdit{{Path: path, Edits: append([]edit.TextEdit{e}, adder.Edits()...)}}, fills...),
		Supplied: a.Field,
	}
	if len(added) > 0 {
		plan.Notes = append(plan.Notes, gocode.ImportNote("added", added))
	}
	if filled > 0 {
		plan.Notes = append(plan.Notes, fmt.Sprintf("gave %d unkeyed literal%s of %s the new fields' zero values", filled, textutil.Plural(filled), a.Name))
	}
	return plan, nil
}

// fillUnkeyed returns the edits that give every unkeyed literal of tn,
// in any package, the zero values of fields, added to st at index at
// (counting each name), and how many literals they change.
func fillUnkeyed(ws *workspace.Workspace, dpkg *packages.Package, tn *types.TypeName, st *ast.StructType, fields *ast.FieldList, at int) ([]edit.FileEdit, int, error) {
	key := analysis.ObjectKey(ws.Fset, tn)
	isT := func(t types.Type) bool {
		n, ok := t.(*types.Named)
		return ok && analysis.ObjectKey(ws.Fset, n.Origin().Obj()) == key
	}
	var added []types.Type
	var edits []edit.FileEdit
	seen := map[string]bool{} // literals, by position, across package variants
	for _, p := range ws.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, f := range p.Syntax {
			var err error
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if err != nil || !ok || len(lit.Elts) == 0 || !isT(p.TypesInfo.TypeOf(lit)) {
					return err == nil
				}
				if _, kv := lit.Elts[0].(*ast.KeyValueExpr); kv {
					return true
				}
				pos := ws.Fset.Position(lit.Pos())
				if seen[pos.String()] {
					return true
				}
				seen[pos.String()] = true
				if added == nil {
					if added, err = fieldTypes(ws, dpkg, st, fields); err != nil {
						err = refactor.Refusef("the unkeyed literal of %s at %s would need the zero values of the new fields: %v; give the literal keys first", tn.Name(), ws.Position(lit.Pos()), err)
						return false
					}
				}
				var zeros []string
				for _, t := range added {
					z, missing := zeroIn(t, p.Types, f)
					if missing != "" {
						err = refactor.Refusef("the unkeyed literal of %s at %s would need the zero value of %s, but %s does not import %s; give the literal keys first", tn.Name(), ws.Position(lit.Pos()), types.TypeString(t, nil), ws.Rel(pos.Filename), missing)
						return false
					}
					zeros = append(zeros, z)
				}
				off := func(p token.Pos) int { return ws.Fset.Position(p).Offset }
				e := edit.TextEdit{NewText: strings.Join(zeros, ", ") + ", "}
				switch {
				case at < len(lit.Elts):
					e.Start = off(lit.Elts[at].Pos())
				default:
					e.Start, e.NewText = off(lit.Elts[len(lit.Elts)-1].End()), ", "+strings.Join(zeros, ", ")
				}
				e.End = e.Start
				edits = append(edits, edit.FileEdit{Path: pos.Filename, Edits: []edit.TextEdit{e}})
				return true
			})
			if err != nil {
				return nil, 0, err
			}
		}
	}
	return edits, len(edits), nil
}

// fieldTypes returns the type of each field fields declares, one per
// name, as the package declaring st sees them.
func fieldTypes(ws *workspace.Workspace, dpkg *packages.Package, st *ast.StructType, fields *ast.FieldList) ([]types.Type, error) {
	var out []types.Type
	for _, f := range fields.List {
		tv, err := types.Eval(ws.Fset, dpkg.Types, st.Pos(), types.ExprString(f.Type))
		if err != nil {
			return nil, err
		}
		for range max(len(f.Names), 1) {
			out = append(out, tv.Type)
		}
	}
	return out, nil
}

// zeroIn spells the zero value of t in file f of package p, or returns
// the package it would name that f does not import.
func zeroIn(t types.Type, p *types.Package, f *ast.File) (zero, missing string) {
	if _, ok := t.(*types.TypeParam); ok {
		return "", "the type parameter's package"
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch {
		case u.Info()&types.IsBoolean != 0:
			return "false", ""
		case u.Info()&types.IsString != 0:
			return `""`, ""
		case u.Info()&types.IsNumeric != 0:
			return "0", ""
		}
		return "nil", ""
	case *types.Struct, *types.Array:
		s := types.TypeString(t, func(q *types.Package) string {
			if q.Path() == p.Path() {
				return ""
			}
			name, ok := importedAs(f, q)
			if !ok && missing == "" {
				missing = q.Path()
			}
			return name
		})
		return s + "{}", missing
	}
	return "nil", ""
}

// importedAs returns the name f imports q by.
func importedAs(f *ast.File, q *types.Package) (string, bool) {
	for _, spec := range f.Imports {
		if path, _ := strconv.Unquote(spec.Path.Value); path != q.Path() {
			continue
		}
		switch {
		case spec.Name == nil:
			return q.Name(), true
		case spec.Name.Name != "_" && spec.Name.Name != ".":
			return spec.Name.Name, true
		}
	}
	return q.Name(), false
}

// indexOf returns the index, counting each name, that fields added to st
// take: just after or before the field named, or at the end.
func indexOf(st *ast.StructType, after, before string) int {
	i := 0
	for _, f := range st.Fields.List {
		if before != "" && fieldDeclares(f, before) {
			return i
		}
		i += max(len(f.Names), 1)
		if after != "" && fieldDeclares(f, after) {
			return i
		}
	}
	return i
}
