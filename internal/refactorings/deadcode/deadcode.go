// Package deadcode implements remove_dead_code: delete a declaration that
// nothing uses and whose deletion changes nothing.
package deadcode

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

// RemoveDeadCode is the "remove_dead_code" refactoring.
type RemoveDeadCode struct{}

// New returns the remove_dead_code refactoring.
func New() *RemoveDeadCode { return &RemoveDeadCode{} }

func (*RemoveDeadCode) Name() string { return "remove_dead_code" }

func (*RemoveDeadCode) Description() string {
	return `Delete a top-level declaration (function, method, type, variable or constant) that nothing uses, with its doc comment and the imports only it used.
Use it to clean up after a change: a helper whose last call went, a copy that was merged, a variable no longer read.
Identify it by file, line and name. It must be unused anywhere in the workspace (its own body aside), unexported (except in a main package, which nothing imports), not init, main or a test, benchmark, fuzz or example function, and not a method an interface in the workspace names. A variable's initializer must have no side effects. In a parenthesized group only its spec goes.
Refuses blank names (var _ I = (*T)(nil) checks something at compile time), declarations with a //go:embed, //go:linkname or //export directive, a type that still has methods (remove them first), and a spec that declares other names still used.`
}

func (*RemoveDeadCode) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":   {"type": "string",  "description": "Path of the file declaring it, relative to the workspace root."},
    "line":   {"type": "integer", "description": "1-based line of its declaration. Omit it to find name as a symbol of the package of file."},
    "name":   {"type": "string",  "description": "The declared name: with line, just the method name for a method; without line, the package-level name or Type.Method."},
    "column": {"type": "integer", "description": "Optional 1-based column, only needed to disambiguate."}
  },
  "required": ["file", "name"],
  "additionalProperties": false
}`)
}

type args struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column,omitempty"`
	Name   string `json:"name"`
}

// Plan checks preconditions and computes the edits.
func (*RemoveDeadCode) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	pkg, id, err := locate.Ident(ws, locate.IdentQuery{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name})
	if err != nil {
		return nil, err
	}
	obj := pkg.TypesInfo.ObjectOf(id)
	if obj == nil || obj.Pkg() == nil {
		return nil, refactor.Refusef("%s at %s is not a declared name", a.Name, ws.Position(id.Pos()))
	}
	if fn, ok := obj.(*types.Func); !(ok && fn.Signature().Recv() != nil) && obj.Parent() != obj.Pkg().Scope() {
		return nil, refactor.Refusef("%s is not declared at the top level; only top-level declarations are supported", a.Name)
	}
	t, err := find(ws, pkg, obj)
	if err != nil {
		return nil, err
	}
	if err := t.check(ws); err != nil {
		return nil, err
	}
	src, err := os.ReadFile(t.path)
	if err != nil {
		return nil, err
	}
	from, to := t.span(ws.Fset.File(t.file.Pos()), src)
	adder := imports.New(ws, pkg, t.file, src)
	none, _ := gocode.ParseStatements("")
	removed := gocode.RemoveImports(pkg, t.file, adder, t.node().Pos(), t.node().End(), none)
	plan := &refactor.Plan{
		Summary: fmt.Sprintf("removed %s, which nothing uses, from %s", a.Name, a.File),
		Edits:   []edit.FileEdit{{Path: t.path, Edits: append([]edit.TextEdit{{Start: from, End: to}}, adder.Edits()...)}},
	}
	if len(removed) > 0 {
		plan.Notes = append(plan.Notes, gocode.ImportNote("removed", removed)+", which only it used")
	}
	return plan, nil
}
