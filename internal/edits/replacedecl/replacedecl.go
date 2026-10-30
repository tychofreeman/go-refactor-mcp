// Package replacedecl implements replace_declaration, an edit that
// replaces a top-level declaration, found by name, with new source.
package replacedecl

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"slices"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/site"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// ReplaceDeclaration is the "replace_declaration" edit.
type ReplaceDeclaration struct{ refactor.Named[args] }

// New returns the replace_declaration edit.
func New() *ReplaceDeclaration {
	return &ReplaceDeclaration{Named: refactor.Called[args]("replace_declaration")}
}

func (*ReplaceDeclaration) Description() string {
	return `Replace a whole top-level declaration, found by name, with new source: an edit, for a signature and body that change together, or a declaration too long to restate as replace_text's old (a table, a type); for a body alone, use replace_statements with function. With name "Cart.Total" and content func (c *Cart) Total() (int, error) { ... }, Total's signature and body are replaced together.
content is the new declaration, declaring the same name (to rename, use rename), then any declarations to add after it (a helper it now calls); in a parenthesized group, just its spec. The old doc comment stays unless content has its own. Packages content names are imported, and those only the old declaration used are removed. Empty content deletes the declaration and its doc comment. Callers a new signature or a deletion breaks are refused, or fixed in the same batch with check "end".`
}

// ChangesBehavior marks this as an edit.
func (*ReplaceDeclaration) ChangesBehavior() {}

type args struct {
	File    string   `json:"file" desc:"Path of a file containing the declaration or a use, relative to the workspace root."`
	Line    int      `json:"line,omitempty" desc:"1-based line of that occurrence. Omit it to find name as a symbol of the package of file."`
	Name    string   `json:"name" desc:"The declared name: Name, or Type.Method for a method."`
	Column  int      `json:"column,omitempty" desc:"Optional 1-based column, only needed to disambiguate."`
	Content string   `json:"content" desc:"The new declaration, as Go source, with a doc comment to replace the old one; declarations after it are added after it. Empty to delete it."`
	Imports []string `json:"imports,omitempty" desc:"Optional: import paths to use for package names content uses, when the name alone is ambiguous."`
}

// header is what gocode.ParseDeclarations puts before content.
const header = "package p\n"

// Plan checks preconditions and computes the edits.
func (*ReplaceDeclaration) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	pkg, id, err := locate.Ident(ws, locate.IdentQuery{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name})
	if err != nil {
		return nil, err
	}
	obj := pkg.TypesInfo.ObjectOf(id)
	if obj == nil {
		return nil, refactor.Refusef("%s at %s is not a declared name", a.Name, ws.Position(id.Pos()))
	}
	path := ws.Fset.Position(obj.Pos()).Filename
	declaring, err := site.Declaring(ws, obj)
	if err != nil {
		return nil, err
	}
	dpkg, file := declaring.Pkg, declaring.File
	decl, spec := declOf(file, obj.Pos())
	if decl == nil {
		return nil, refactor.Refusef("%s is not a top-level declaration", a.Name)
	}
	if strings.TrimSpace(a.Content) == "" {
		return deletion(ws, declaring, path, a.Name, decl, spec)
	}
	code, decls, err := gocode.ParseDeclarations(a.Content)
	if err != nil {
		return nil, err
	}
	// The new declaration, then any to add after it.
	nd := decls[0]
	var extra []string
	for _, d := range decls[1:] {
		extra = append(extra, declared(d)...)
	}
	if names := declared(nd); !slices.Contains(names, obj.Name()) {
		if slices.Contains(extra, obj.Name()) {
			return nil, refactor.Refusef("content must start with the new %s; declarations to add go after it", obj.Name())
		}
		return nil, refactor.Refusef("content declares %s, not %s; to rename it, use rename", strings.Join(names, ", "), obj.Name())
	}
	if err := sameKind(decl, nd); err != nil {
		return nil, err
	}
	src := declaring.Src
	tf := ws.Fset.File(file.Pos())
	text := strings.TrimSpace(a.Content)
	start, end := decl.Pos(), decl.End()
	switch {
	case spec != nil:
		// One spec of a group: its doc stays, and the new spec's text goes in.
		g := nd.(*ast.GenDecl)
		if len(g.Specs) != 1 || len(decls) != 1 {
			return nil, refactor.Refusef("%s is one spec of a group; content must declare just it", obj.Name())
		}
		text = textOf(a.Content, g.Specs[0])
		start, end = spec.Pos(), spec.End()
	case docOf(nd) != nil:
		if d := docOf(decl); d != nil {
			start = d.Pos()
		}
	}
	adder := imports.New(ws, dpkg, file, src)
	added, err := gocode.AddImports(ws, dpkg, file, adder, file.Name.Pos(), code, a.Imports)
	if err != nil {
		return nil, err
	}
	removed := gocode.RemoveImports(dpkg, file, adder, gocode.Between(start, end), code)
	plan := &refactor.Plan{
		Summary:  fmt.Sprintf("replaced %s in %s", a.Name, ws.Rel(path)),
		Edits:    []edit.FileEdit{{Path: path, Edits: append([]edit.TextEdit{{Start: tf.Offset(start), End: tf.Offset(end), NewText: text}}, adder.Edits()...)}},
		Supplied: a.Content,
	}
	if len(extra) > 0 {
		plan.Notes = append(plan.Notes, fmt.Sprintf("added %s after %s", strings.Join(extra, ", "), obj.Name()))
	}
	if len(added) > 0 {
		plan.Notes = append(plan.Notes, gocode.ImportNote("added", added))
	}
	if len(removed) > 0 {
		plan.Notes = append(plan.Notes, gocode.ImportNote("removed", removed)+", which only the old declaration used")
	}
	return plan, nil
}

// deletion is the plan that deletes decl, or its spec when that is one of
// a group, with its doc comment and the imports only it used.
func deletion(ws *workspace.Workspace, declaring *site.Site, path, name string, decl ast.Decl, spec ast.Spec) (*refactor.Plan, error) {
	file, src := declaring.File, declaring.Src
	tf := ws.Fset.File(file.Pos())
	start, end, doc := decl.Pos(), decl.End(), docOf(decl)
	if spec != nil {
		start, end, doc = spec.Pos(), spec.End(), specDoc(spec)
	}
	if doc != nil {
		start = doc.Pos()
	}
	from := tf.Offset(tf.LineStart(tf.Line(start)))
	to := tf.Offset(end)
	for to < len(src) && src[to] != '\n' {
		to++
	}
	to = min(to+1, len(src))
	switch {
	case spec != nil:
	case to < len(src) && src[to] == '\n':
		to++ // the blank line after it
	case from > 1 && src[from-1] == '\n' && src[from-2] == '\n':
		from-- // the last declaration: the blank line before it
	}
	none, err := gocode.ParseStatements("")
	if err != nil {
		return nil, err
	}
	adder := imports.New(ws, declaring.Pkg, file, src)
	removed := gocode.RemoveImports(declaring.Pkg, file, adder, gocode.Between(start, end), none)
	plan := &refactor.Plan{
		Summary: fmt.Sprintf("deleted %s from %s", name, ws.Rel(path)),
		Edits:   []edit.FileEdit{{Path: path, Edits: append([]edit.TextEdit{{Start: from, End: to}}, adder.Edits()...)}},
	}
	if len(removed) > 0 {
		plan.Notes = append(plan.Notes, gocode.ImportNote("removed", removed)+", which only the deleted declaration used")
	}
	return plan, nil
}

// specDoc is the doc comment of a spec of a group.
func specDoc(s ast.Spec) *ast.CommentGroup {
	switch s := s.(type) {
	case *ast.ValueSpec:
		return s.Doc
	case *ast.TypeSpec:
		return s.Doc
	}
	return nil
}

// declOf is the top-level declaration of file that declares the name at
// pos, and its spec when that is one of several in a group.
func declOf(file *ast.File, pos token.Pos) (ast.Decl, ast.Spec) {
	for _, d := range file.Decls {
		if slices.Contains(declaredAt(d), pos) {
			if g, ok := d.(*ast.GenDecl); ok && len(g.Specs) > 1 {
				for _, s := range g.Specs {
					if slices.Contains(specAt(s), pos) {
						return d, s
					}
				}
			}
			return d, nil
		}
	}
	return nil, nil
}

// declaredAt are the positions of the names d declares.
func declaredAt(d ast.Decl) []token.Pos {
	var out []token.Pos
	switch d := d.(type) {
	case *ast.FuncDecl:
		out = append(out, d.Name.Pos())
	case *ast.GenDecl:
		for _, s := range d.Specs {
			out = append(out, specAt(s)...)
		}
	}
	return out
}

// specAt are the positions of the names s declares.
func specAt(s ast.Spec) []token.Pos {
	switch s := s.(type) {
	case *ast.TypeSpec:
		return []token.Pos{s.Name.Pos()}
	case *ast.ValueSpec:
		var out []token.Pos
		for _, n := range s.Names {
			out = append(out, n.Pos())
		}
		return out
	}
	return nil
}

// declared are the names d declares.
func declared(d ast.Decl) []string {
	var out []string
	switch d := d.(type) {
	case *ast.FuncDecl:
		out = append(out, d.Name.Name)
	case *ast.GenDecl:
		for _, s := range d.Specs {
			switch s := s.(type) {
			case *ast.TypeSpec:
				out = append(out, s.Name.Name)
			case *ast.ValueSpec:
				for _, n := range s.Names {
					out = append(out, n.Name)
				}
			}
		}
	}
	return out
}

// sameKind refuses new, the replacement of old, when it is another kind of
// declaration: a function for a variable, a const for a var.
func sameKind(old, new ast.Decl) error {
	og, oldGen := old.(*ast.GenDecl)
	ng, newGen := new.(*ast.GenDecl)
	switch {
	case oldGen != newGen:
		return refactor.Refusef("content is another kind of declaration than the old one (a function for a %s, or the reverse)", map[bool]string{true: og.Tok.String(), false: "func"}[oldGen])
	case oldGen && og.Tok != ng.Tok:
		return refactor.Refusef("content is a %s declaration, the old one a %s", ng.Tok, og.Tok)
	}
	return nil
}

// docOf is d's doc comment, or nil.
func docOf(d ast.Decl) *ast.CommentGroup {
	switch d := d.(type) {
	case *ast.FuncDecl:
		return d.Doc
	case *ast.GenDecl:
		return d.Doc
	}
	return nil
}

// textOf is the source of n in content, as gocode.ParseDeclarations parsed
// it after the header.
func textOf(content string, n ast.Node) string {
	off := func(p token.Pos) int { return int(p) - 1 - len(header) }
	return content[off(n.Pos()):off(n.End())]
}
