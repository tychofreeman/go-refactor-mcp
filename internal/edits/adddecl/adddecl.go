// Package adddecl implements add_declaration: an edit that adds new
// top-level declarations to a file.
package adddecl

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/testfor"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/site"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// AddDeclaration is the "add_declaration" edit.
type AddDeclaration struct{ refactor.Named[args] }

// New returns the add_declaration edit.
func New() *AddDeclaration { return &AddDeclaration{Named: refactor.Called[args]("add_declaration")} }

func (*AddDeclaration) Description() string {
	return `Add top-level declarations (functions, methods, types, variables, constants) to an existing file; for a new file, use create_file. content is Go source with doc comments, without package clause or imports. They go at the end of the file, or just after or before the declaration named in after or before: after "Cart.Total" puts a new method right after Total.
A function or method named like a stub (a body of only panic("not implemented"), as create_from_usage writes) replaces the stub, wherever it is in the package. test_for "Bed.Plant" adds a red TestBedPlant instead.`
}

// ChangesBehavior marks this as an edit.
func (*AddDeclaration) ChangesBehavior() {}

type args struct {
	File    string   `json:"file" desc:"Relative to the workspace root."`
	Content string   `json:"content,omitempty" desc:"Go declarations, without package clause or imports."`
	TestFor string   `json:"test_for,omitempty" desc:"Instead of content, in a _test.go file: a function or Type.Method; adds a test that calls it and fails, showing the result, until you write the assertion."`
	After   string   `json:"after,omitempty" desc:"Optional: put them just after this declaration of file (Name, or Type.Method)."`
	Before  string   `json:"before,omitempty" desc:"Optional: put them just before this declaration of file."`
	Imports []string `json:"imports,omitempty" desc:"Optional: import paths for ambiguous package names."`
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
	if a.TestFor != "" {
		if a.Content != "" {
			return nil, refactor.Refusef("give content or test_for, not both: test_for writes the test")
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil, refactor.Refusef("test_for writes a test, so file is a _test.go file; %s is not one", a.File)
		}
		test, err := testfor.Source(pkg.Types, a.TestFor)
		if err != nil {
			return nil, err
		}
		a.Content = test
	}
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
