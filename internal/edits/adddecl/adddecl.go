// Package adddecl implements add_declaration: an edit that adds new
// top-level declarations to a file.
package adddecl

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// AddDeclaration is the "add_declaration" edit.
type AddDeclaration struct{}

// New returns the add_declaration edit.
func New() *AddDeclaration { return &AddDeclaration{} }

func (*AddDeclaration) Name() string { return "add_declaration" }

func (*AddDeclaration) Description() string {
	return `Add new top-level declarations (functions, methods, types, variables, constants) to a file. This is an edit, not a refactoring: it may change behavior.
Use it to add a function, a type or a method to an existing file; use create_file for a new file, and the refactorings to move or change declarations that exist.
Give file and content, the declarations as Go source with their doc comments, but without a package clause or imports: packages the code names are imported, as replace_statements does (imports chooses when a name is ambiguous). They go at the end of the file, or just after or before the top-level declaration named in after or before (a method as Type.Method).
Like every step, the result must type-check (so a name the package already declares is refused with the compiler's error), or nothing changes.`
}

func (*AddDeclaration) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":    {"type": "string", "description": "Path of the file, relative to the workspace root."},
    "content": {"type": "string", "description": "The declarations, as Go source, without package clause or imports."},
    "after":   {"type": "string", "description": "Optional: a top-level declaration of the file (a function, type, variable or constant name, or Type.Method for a method) to put the new ones just after."},
    "before":  {"type": "string", "description": "Optional: a top-level declaration of the file (or Type.Method) to put the new ones just before."},
    "imports": {"type": "array", "items": {"type": "string"}, "description": "Optional: import paths to use for package names the content uses, when the name alone is ambiguous."}
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
	pkg, file := ws.FileSyntax(path)
	if file == nil {
		return nil, refactor.Refusef("file %s is not part of the loaded packages", a.File)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	code, decls, err := gocode.ParseDeclarations(a.Content)
	if err != nil {
		return nil, err
	}
	at, pre, post, err := offset(ws.Fset.File(file.Pos()), file, src, a.File, a.After, a.Before)
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
	return plan, nil
}
