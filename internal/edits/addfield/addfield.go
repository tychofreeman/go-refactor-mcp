// Package addfield implements add_field: an edit that adds fields to a
// struct type.
package addfield

import (
	"encoding/json"
	"fmt"
	"go/types"
	"os"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// AddField is the "add_field" edit.
type AddField struct{}

// New returns the add_field edit.
func New() *AddField { return &AddField{} }

func (*AddField) Name() string { return "add_field" }

func (*AddField) Description() string {
	return `Add fields to a struct type. This is an edit, not a refactoring: it changes the struct (its literals, its size, what reflection sees).
Use it to give a type the state a feature needs; use extract_struct to move fields that are there.
Identify the struct by file, line and name, at its declaration or any use. field is one or more fields as Go source (with doc or trailing comments, tags, embedded types); they go at the end, or just after or before the field named in after or before. A struct written on one line is opened up into lines. Packages the fields' types name are imported, as replace_statements does (imports chooses when a name is ambiguous).
Like every step, the result must type-check: a duplicate name, or an unkeyed literal of the type that would no longer list every field, is refused with the compiler's errors.`
}

func (*AddField) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":    {"type": "string",  "description": "Path of a file containing the struct type's declaration or a use, relative to the workspace root."},
    "line":    {"type": "integer", "description": "1-based line of that occurrence. Omit it to find name as a symbol of the package of file."},
    "name":    {"type": "string",  "description": "The struct type's name; without line, it is found among the package-level names."},
    "column":  {"type": "integer", "description": "Optional 1-based column, only needed to disambiguate."},
    "field":   {"type": "string",  "description": "The fields to add, as Go source, e.g. \"timeout time.Duration // per try\"."},
    "after":   {"type": "string",  "description": "Optional: a field to put them just after."},
    "before":  {"type": "string",  "description": "Optional: a field to put them just before."},
    "imports": {"type": "array", "items": {"type": "string"}, "description": "Optional: import paths to use for package names the fields use, when the name alone is ambiguous."}
  },
  "required": ["file", "name", "field"],
  "additionalProperties": false
}`)
}

// ChangesBehavior marks this as an edit.
func (*AddField) ChangesBehavior() {}

type args struct {
	File    string   `json:"file"`
	Line    int      `json:"line"`
	Column  int      `json:"column,omitempty"`
	Name    string   `json:"name"`
	Field   string   `json:"field"`
	After   string   `json:"after,omitempty"`
	Before  string   `json:"before,omitempty"`
	Imports []string `json:"imports,omitempty"`
}

// Plan checks preconditions and computes the edits.
func (*AddField) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
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
	dpkg, file := ws.FileSyntax(path)
	if file == nil {
		return nil, refactor.Refusef("%s is declared outside the loaded packages", a.Name)
	}
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
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	e, err := insertEdit(ws.Fset.File(file.Pos()), st, src, a.Field, a.Name, a.After, a.Before)
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
		Edits:    []edit.FileEdit{{Path: path, Edits: append([]edit.TextEdit{e}, adder.Edits()...)}},
		Supplied: a.Field,
	}
	if len(added) > 0 {
		plan.Notes = append(plan.Notes, gocode.ImportNote("added", added))
	}
	return plan, nil
}
