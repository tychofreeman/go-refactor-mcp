// Package deadcode implements remove_dead_code: delete a declaration that
// nothing uses and whose deletion changes nothing.
package deadcode

import (
	"encoding/json"
	"fmt"
	"go/token"
	"go/types"
	"os"
	"slices"
	"strings"

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
	return `Delete a top-level declaration (function, method, type, variable or constant), or a field of a struct type, that nothing uses, with its doc comment and the imports only it used.
Use it to clean up after a change: a helper whose last call went, a copy that was merged, a variable no longer read.
Identify it by file, line and name; also names more declarations of the package to remove with it, such as functions that only call each other or a type and its methods. It must be unused anywhere in the workspace (its own body, and those removed with it, aside), unexported (except in a main package, which nothing imports, or with exported, when you know no other module uses it), not init, main or a test, benchmark, fuzz or example function, and not a method an interface in the workspace names. A variable's initializer must have no side effects. In a parenthesized group only its spec goes.
Refuses blank names (var _ I = (*T)(nil) checks something at compile time), declarations with a //go:embed, //go:linkname or //export directive, a type that still has methods (remove them first), and a spec that declares other names still used.
A field (Type.Field) goes when nothing reads, writes or names it in a literal, after its last use was moved elsewhere (a change of representation: fold literals through a setter, then remove the old fields). Refuses an embedded field, one declared with others (a, b int), a struct with an unkeyed literal, and one a value of which is passed as an interface (fmt, encoding/json and reflect see its fields).`
}

func (*RemoveDeadCode) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":   {"type": "string",  "description": "Path of the file declaring it, relative to the workspace root."},
    "line":   {"type": "integer", "description": "1-based line of its declaration. Omit it to find name as a symbol of the package of file."},
    "name":   {"type": "string",  "description": "The declared name: with line, just the method or field name; without line, the package-level name, Type.Method or Type.Field."},
    "exported": {"type": "boolean", "description": "Optional: delete exported code too, when no other module uses it."},
    "column": {"type": "integer", "description": "Optional 1-based column, only needed to disambiguate."},
    "also":   {"type": "array", "items": {"type": "string"}, "description": "Optional: more names of the package to remove with it (Name or Type.Method); uses among them do not count."}
  },
  "required": ["file", "name"],
  "additionalProperties": false
}`)
}

type args struct {
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Column   int      `json:"column,omitempty"`
	Name     string   `json:"name"`
	Exported bool     `json:"exported,omitempty"`
	Also     []string `json:"also,omitempty"`
}

// Plan checks preconditions and computes the edits.
func (*RemoveDeadCode) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	var group []*target
	for _, q := range append([]locate.IdentQuery{{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name}}, alsoQueries(a)...) {
		t, err := targetOf(ws, q)
		if err != nil {
			return nil, err
		}
		group = append(group, t)
	}
	for _, t := range group {
		t.group = group
		if err := t.check(ws, a.Exported); err != nil {
			return nil, err
		}
	}
	var paths []string
	byPath := map[string][]*target{}
	for _, t := range group {
		if byPath[t.path] == nil {
			paths = append(paths, t.path)
		}
		byPath[t.path] = append(byPath[t.path], t)
	}
	plan := &refactor.Plan{}
	var removed []string
	for _, path := range paths {
		ts := byPath[path]
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		tf := ws.Fset.File(ts[0].file.Pos())
		var es []edit.TextEdit
		for _, t := range ts {
			from, to := t.span(tf, src)
			es = append(es, edit.TextEdit{Start: from, End: to})
		}
		adder := imports.New(ws, ts[0].pkg, ts[0].file, src)
		none, _ := gocode.ParseStatements("")
		removed = append(removed, gocode.RemoveImports(ts[0].pkg, ts[0].file, adder, func(p token.Pos) bool {
			return slices.ContainsFunc(ts, func(t *target) bool { return t.node().Pos() <= p && p < t.node().End() })
		}, none)...)
		plan.Edits = append(plan.Edits, edit.FileEdit{Path: path, Edits: append(es, adder.Edits()...)})
	}
	names := []string{a.Name}
	names = append(names, a.Also...)
	if len(names) == 1 {
		plan.Summary = fmt.Sprintf("removed %s, which nothing uses, from %s", a.Name, a.File)
	} else {
		plan.Summary = fmt.Sprintf("removed %s, which nothing else uses, from %s", strings.Join(names, ", "), a.File)
	}
	if len(removed) > 0 {
		plan.Notes = append(plan.Notes, gocode.ImportNote("removed", removed)+", which only it used")
	}
	return plan, nil
}

// alsoQueries are the queries for a's also: names of the package of a's
// file, found without a line.
func alsoQueries(a args) []locate.IdentQuery {
	var qs []locate.IdentQuery
	for _, n := range a.Also {
		qs = append(qs, locate.IdentQuery{File: a.File, Name: n})
	}
	return qs
}

// targetOf is the top-level declaration q names.
func targetOf(ws *workspace.Workspace, q locate.IdentQuery) (*target, error) {
	pkg, id, err := locate.Ident(ws, q)
	if err != nil {
		return nil, err
	}
	obj := pkg.TypesInfo.ObjectOf(id)
	if obj == nil || obj.Pkg() == nil {
		return nil, refactor.Refusef("%s at %s is not a declared name", q.Name, ws.Position(id.Pos()))
	}
	if v, ok := obj.(*types.Var); ok && v.IsField() {
		return findField(ws, pkg, v)
	}
	if fn, ok := obj.(*types.Func); !(ok && fn.Signature().Recv() != nil) && obj.Parent() != obj.Pkg().Scope() {
		return nil, refactor.Refusef("%s is not declared at the top level; only top-level declarations are supported", q.Name)
	}
	return find(ws, pkg, obj)
}
