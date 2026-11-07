// Package changetype implements change_type: an edit that changes what a
// type is (an alias's target, or a defined type's underlying type), for
// the uses that no longer fit to be fixed in the same batch.
package changetype

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/site"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// ChangeType is the "change_type" edit.
type ChangeType struct{ refactor.Named[args] }

// New returns the change_type edit.
func New() *ChangeType { return &ChangeType{Named: refactor.Called[args]("change_type")} }

// Route names the tool the arguments ask for (refactor.ChangeTypeRoute).
func (*ChangeType) Route(args json.RawMessage) (string, error) { return refactor.ChangeTypeRoute(args) }

// Covers names the tools change_type stands for.
func (*ChangeType) Covers() []string { return refactor.ChangeTypeCovers }

func (*ChangeType) Description() string {
	return `Change a type; what you give says which:
- name Type.Field: the field's type, every value keeping its meaning: converted where it is written and, where its type matters, read. ID string to ID PlantID turns Plant{ID: s} into Plant{ID: PlantID(s)} and strings.ToUpper(p.ID) into strings.ToUpper(string(p.ID)). values, narrowing to an interface, maps old constants to new values.
- name (a function, or Type.Method) and parameter: the parameter's type, with the body and every call: to an interface the old type implements (func save(b *bytes.Buffer) to b io.Writer, calls unchanged), to a type with the same underlying type (find(name string) to name UserID: calls convert, find(UserID(n))), or to an alias of the old type.
- name, a type of the module: what it is, an alias's target (type renderArg = string to any) or a defined type's underlying type. An edit: every use takes the new type. After a parameter is changed to an alias, change the alias here, in a batch with check "end" with the fixes its result lists.`
}

// ChangesBehavior marks this as an edit.
func (*ChangeType) ChangesBehavior() {}

// NotesBreakage: in a batch checked at its end, the result lists the
// errors it leaves.
func (*ChangeType) NotesBreakage() {}

type args struct {
	File      string            `json:"file" desc:"Path of a file containing the type's declaration or a use, relative to the workspace root."`
	Line      int               `json:"line,omitempty" desc:"1-based line of that occurrence. Omit it to find name as a symbol of the package of file."`
	Name      string            `json:"name" desc:"The type's name; Type.Field for a field; the function (or Type.Method) with parameter."`
	Column    int               `json:"column,omitempty" desc:"Optional 1-based column, only needed to disambiguate."`
	Type      string            `json:"type" desc:"What the type is to be, as Go source, e.g. any or int64."`
	Imports   []string          `json:"imports,omitempty" desc:"Optional: import paths to use for package names type uses, when the name alone is ambiguous."`
	Parameter string            `json:"parameter,omitempty" desc:"For a parameter: its name; name is then the function."`
	Values    map[string]string `json:"values,omitempty" desc:"For a field narrowed to an interface: for each old constant (\"fern\" or fern), the value of the new type, e.g. {\"fern\": \"Fern{}\"}."`
}

// Plan checks preconditions and computes the edits.
func (*ChangeType) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	pkg, id, err := locate.Ident(ws, locate.IdentQuery{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name})
	if err != nil {
		return nil, err
	}
	tn, ok := pkg.TypesInfo.ObjectOf(id).(*types.TypeName)
	if !ok {
		return nil, refactor.Refusef("%s is not a type", a.Name)
	}
	path := ws.Fset.Position(tn.Pos()).Filename
	declaring, err := site.Declaring(ws, tn)
	if err != nil {
		return nil, err
	}
	dpkg, file := declaring.Pkg, declaring.File
	spec := typeSpec(file, tn.Pos())
	if spec == nil {
		return nil, refactor.Refusef("%s is not declared at the top level of %s", a.Name, ws.Rel(path))
	}
	if spec.TypeParams != nil {
		return nil, refactor.Refusef("%s has type parameters; changing a generic type is not supported", a.Name)
	}
	src := declaring.Src
	tf := ws.Fset.File(file.Pos())
	oldText := string(src[tf.Offset(spec.Type.Pos()):tf.Offset(spec.Type.End())])
	newText := strings.TrimSpace(a.Type)
	if newText == oldText {
		return nil, refactor.Refusef("%s is already %s", a.Name, oldText)
	}
	assign := ""
	if spec.Assign.IsValid() {
		assign = "= "
	}
	code, decls, err := gocode.ParseDeclarations("type _ " + assign + newText)
	if err != nil || len(decls) != 1 {
		return nil, refactor.Refusef("type %q is not a Go type: %v", a.Type, err)
	}
	adder := imports.New(ws, dpkg, file, src)
	added, err := gocode.AddImports(ws, dpkg, file, adder, file.Name.Pos(), code, a.Imports)
	if err != nil {
		return nil, err
	}
	removed := gocode.RemoveImports(dpkg, file, adder, gocode.Between(spec.Type.Pos(), spec.Type.End()), code)
	e := edit.TextEdit{Start: tf.Offset(spec.Type.Pos()), End: tf.Offset(spec.Type.End()), NewText: newText}
	kind := "the underlying type of"
	if assign != "" {
		kind = "the alias"
	}
	plan := &refactor.Plan{
		Summary:  fmt.Sprintf("changed %s %s from %s to %s in %s", kind, a.Name, oldText, newText, ws.Rel(path)),
		Edits:    []edit.FileEdit{{Path: path, Edits: append([]edit.TextEdit{e}, adder.Edits()...)}},
		Supplied: newText,
	}
	if len(added) > 0 {
		plan.Notes = append(plan.Notes, gocode.ImportNote("added", added))
	}
	if len(removed) > 0 {
		plan.Notes = append(plan.Notes, gocode.ImportNote("removed", removed)+", which only the old type used")
	}
	return plan, nil
}

// typeSpec is the top-level type declaration of file that declares the
// name at pos.
func typeSpec(file *ast.File, pos token.Pos) *ast.TypeSpec {
	for _, d := range file.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.TYPE {
			continue
		}
		for _, s := range g.Specs {
			if ts := s.(*ast.TypeSpec); ts.Name.Pos() == pos {
				return ts
			}
		}
	}
	return nil
}
