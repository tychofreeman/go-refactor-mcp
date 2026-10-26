// Package adddecl implements add_declaration: an edit that adds new
// top-level declarations to a file.
package adddecl

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/site"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// AddDeclaration is the "add_declaration" edit.
type AddDeclaration struct{}

// New returns the add_declaration edit.
func New() *AddDeclaration { return &AddDeclaration{} }

func (*AddDeclaration) Name() string { return "add_declaration" }

func (*AddDeclaration) Description() string {
	return `Add top-level declarations (functions, methods, types, variables, constants) to an existing file: an edit; for a new file, use create_file. content is Go source with doc comments, without package clause or imports; packages it names are imported (imports picks one when a name is ambiguous). They go at the end of the file, or just after or before the declaration named in after or before: after "Cart.Total" puts a new method right after Total.
A function or method named like a stub (a body of only panic("not implemented"), as create_from_usage writes) replaces the stub, wherever it is in the package.`
}

func (*AddDeclaration) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":    {"type": "string", "description": "Relative to the workspace root."},
    "content": {"type": "string", "description": "Go declarations, without package clause or imports."},
    "after":   {"type": "string", "description": "Optional: put them just after this declaration of file (Name, or Type.Method)."},
    "before":  {"type": "string", "description": "Optional: put them just before this declaration of file."},
    "imports": {"type": "array", "items": {"type": "string"}, "description": "Optional: import paths for ambiguous package names."}
  },
  "required": ["file", "content"],
  "additionalProperties": false
}`)
}

// ChangesBehavior marks this as an edit.
func (*AddDeclaration) ChangesBehavior() {}

type args struct {
	File    string   `json:"file"`
	Content string   `json:"content"`
	After   string   `json:"after,omitempty"`
	Before  string   `json:"before,omitempty"`
	Imports []string `json:"imports,omitempty"`
}

// Plan checks preconditions and computes the edits.
func (*AddDeclaration) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	opened, err := site.At(ws, path)
	if err != nil {
		return nil, err
	}
	pkg, file, src := opened.Pkg, opened.File, opened.Src
	code, decls, err := gocode.ParseDeclarations(a.Content)
	if err != nil {
		return nil, err
	}
	at, pre, post, err := offset(opened.TF, file, src, a.File, a.After, a.Before)
	if err != nil {
		return nil, err
	}
	adder := imports.New(ws, pkg, file, src)
	added, err := gocode.AddImports(ws, pkg, file, adder, file.Name.Pos(), code, a.Imports)
	if err != nil {
		return nil, err
	}
	text := pre + strings.Trim(a.Content, "\n") + "\n" + post
	plan := &refactor.Plan{
		Summary:  fmt.Sprintf("added %d declaration%s to %s", len(decls), textutil.Plural(len(decls)), a.File),
		Edits:    []edit.FileEdit{{Path: path, Edits: append([]edit.TextEdit{{Start: at, End: at, NewText: text}}, adder.Edits()...)}},
		Supplied: a.Content,
	}
	if len(added) > 0 {
		plan.Notes = append(plan.Notes, gocode.ImportNote("added", added))
	}
	stubs, notes, err := replacedStubs(ws, pkg, decls)
	if err != nil {
		return nil, err
	}
	plan.Edits = append(plan.Edits, stubs...)
	plan.Notes = append(plan.Notes, notes...)
	return plan, nil
}
