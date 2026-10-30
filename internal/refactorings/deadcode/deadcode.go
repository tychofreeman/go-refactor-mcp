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
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// RemoveDeadCode is the "remove_dead_code" refactoring.
type RemoveDeadCode struct{}

// New returns the remove_dead_code refactoring.
func New() *RemoveDeadCode { return &RemoveDeadCode{} }

func (*RemoveDeadCode) Name() string { return "remove_dead_code" }

func (*RemoveDeadCode) Description() string {
	return `Delete a top-level declaration or a struct field that nothing uses, with its doc comment and the imports only it used: once the last call of legacyFormat went, removing legacyFormat deletes the function and its import of strings; removing Plant.Colour deletes the field once nothing reads, writes or names it in a literal (the end of a change of representation: fold literals through a setter, then remove the old fields). Exported code goes too, with a note that the module's API changed. value removes a field still read when every literal sets it equal to a path of the struct's other fields: value WS turns m.ws into m.WS and drops ws: s.WS from mover{Site: s, ws: s.WS}.
Identify it by file and name (Name, Type.Method or Type.Field), or by line; also names more declarations of the package to remove with it, such as functions that only call each other, or a type and its methods.
Refuses, saying why: something still used (its own body aside); init, main or a test function; a declaration with a //go: directive; a constant whose iota value a later one depends on; a method an interface names; an initializer with side effects; a blank name (var _ I = (*T)(nil)); a type with methods left; a field that is embedded, declared with others, given other than a constant in an unkeyed literal (a constant's element goes with it), or of a struct passed where reflection may see its fields (encoding/json, a comparison by reflection; what fmt prints is listed in a note).`
}

func (*RemoveDeadCode) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":   {"type": "string",  "description": "Path of the file declaring it, relative to the workspace root."},
    "line":   {"type": "integer", "description": "1-based line of its declaration. Omit it to find name as a symbol of the package of file."},
    "name":   {"type": "string",  "description": "The declared name: with line, just the method or field name; without line, the package-level name, Type.Method or Type.Field."},
    "exported": {"type": "boolean", "description": "No longer needed: exported code is removed, with a note that the module's API changed."},
    "column": {"type": "integer", "description": "Optional 1-based column, only needed to disambiguate."},
    "also":   {"type": "array", "items": {"type": "string"}, "description": "Optional: more names of the package to remove with it (Name or Type.Method); uses among them do not count."},
    "value":  {"type": "string",  "description": "For a field still used: the path of the struct's other fields every literal sets it equal to (WS, or Site.WS); its reads become that."}
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
	Value    string   `json:"value,omitempty"`
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
		if err := t.check(ws); err != nil {
			return nil, err
		}
	}
	var notes []string
	for _, t := range group {
		if n, ok := t.apiNote(); ok {
			notes = append(notes, n)
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
	dropped := 0
	for _, t := range group {
		plan.Edits = append(plan.Edits, t.litEdits...)
		dropped += len(t.litEdits)
	}
	plan.Edits = edit.Merge(plan.Edits)
	if dropped > 0 {
		plan.Notes = append(plan.Notes, fmt.Sprintf("deleted its element from %d unkeyed literal%s", dropped, textutil.Plural(dropped)))
	}
	for _, t := range group {
		if len(t.printed) > 0 {
			plan.Notes = append(plan.Notes, fmt.Sprintf("what these calls print changes, since %s no longer prints: %s", t.obj.Name(), textutil.FirstFew(t.printed, 5)))
		}
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
	plan.Notes = append(plan.Notes, notes...)
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
