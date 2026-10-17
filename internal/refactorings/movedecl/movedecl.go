// Package movedecl implements move_declaration: move a top-level
// declaration to another file of the same package.
//
// Within a package, top-level names are in the package scope, so no
// reference changes. What does change is per file: the imports the moved
// code needs (added to the destination, removed from the source when
// nothing else uses them), and which builds see the code (test files and
// build constraints). And because package initialization follows the
// order of files, an init function or a variable whose initializer has
// side effects stays where it is.
package movedecl

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/build"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// MoveDeclaration is the "move_declaration" refactoring.
type MoveDeclaration struct{}

// New returns the move_declaration refactoring.
func New() *MoveDeclaration { return &MoveDeclaration{} }

func (*MoveDeclaration) Name() string { return "move_declaration" }

func (*MoveDeclaration) Description() string {
	return `Move a top-level declaration (function, method, type, variable or constant, with its doc comment) to another file of the same package, creating the file if needed.
Use it to put code where it belongs before a change, e.g. a helper that two files now share.
Identify the declaration by file, line and name, at the declaration or any use, and give to_file: the destination, relative to the workspace root, in the same directory.
Imports the moved code needs are added to the destination (keeping an alias, or using the destination's name for the package); imports only it used are removed from the source.
Refuses moving to another directory (package), between a test file and a non-test file, to or from a file with build constraints, a declaration grouped with others in parentheses, an init function or a variable whose initializer has side effects (initialization order follows the files), a //go:embed variable, and code that uses a dot import.`
}

func (*MoveDeclaration) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":    {"type": "string",  "description": "Path of a file containing the declaration or a use of it, relative to the workspace root."},
    "line":    {"type": "integer", "description": "1-based line of that occurrence."},
    "name":    {"type": "string",  "description": "The declared name (for a method, just the method name)."},
    "column":  {"type": "integer", "description": "Optional 1-based column, only needed to disambiguate."},
    "to_file": {"type": "string",  "description": "Destination file, relative to the workspace root, in the same directory; created if it does not exist."}
  },
  "required": ["file", "line", "name", "to_file"],
  "additionalProperties": false
}`)
}

type moveArgs struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column,omitempty"`
	Name   string `json:"name"`
	ToFile string `json:"to_file"`
}

// mover holds what one move needs.
type mover struct {
	ws      *workspace.Workspace
	pkg     *packages.Package // a variant containing the source file
	file    *ast.File
	path    string
	src     []byte
	decl    ast.Decl
	start   int // the declaration's text, with its doc comment
	end     int // and a comment that ends its last line
	imports []*ast.ImportSpec
}

// Plan checks preconditions and computes the edits.
func (*MoveDeclaration) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a moveArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.ToFile == "" {
		return nil, refactor.Refusef("to_file is required")
	}
	m, err := find(ws, locate.IdentQuery{File: a.File, Line: a.Line, Column: a.Column, Name: a.Name})
	if err != nil {
		return nil, err
	}
	if err := m.checkDecl(); err != nil {
		return nil, err
	}
	to := ws.Abs(a.ToFile)
	dst, err := m.checkDestination(to)
	if err != nil {
		return nil, err
	}
	if err := m.collectImports(); err != nil {
		return nil, err
	}

	fes := []edit.FileEdit{{Path: m.path, Edits: append(m.removeEdits(), edit.TextEdit{Start: m.lineStart(m.start), End: m.nextLine(m.end)})}}
	if dst == nil {
		fes = append(fes, edit.FileEdit{Path: to, Edits: []edit.TextEdit{{NewText: m.newFile()}}})
	} else {
		fe, err := m.appendTo(to, dst)
		if err != nil {
			return nil, err
		}
		fes = append(fes, fe)
	}
	return &refactor.Plan{
		Summary: fmt.Sprintf("moved %s from %s to %s", a.Name, ws.Rel(m.path), ws.Rel(to)),
		Edits:   edit.Merge(fes),
	}, nil
}

// find resolves the identifier to its top-level declaration.
func find(ws *workspace.Workspace, q locate.IdentQuery) (*mover, error) {
	pkg, id, err := locate.Ident(ws, q)
	if err != nil {
		return nil, err
	}
	obj := pkg.TypesInfo.ObjectOf(id)
	if obj == nil || !obj.Pos().IsValid() {
		return nil, refactor.Refusef("%q at %s is not a declared name", id.Name, ws.Position(id.Pos()))
	}
	pos := ws.Fset.Position(obj.Pos())
	if !ws.Contains(pos.Filename) {
		return nil, refactor.Refusef("%s is declared outside the workspace", id.Name)
	}
	for _, p := range ws.Pkgs {
		for _, f := range p.Syntax {
			if ws.Fset.File(f.Pos()).Name() != pos.Filename {
				continue
			}
			for _, d := range f.Decls {
				if declares(ws.Fset, d, pos.Offset) {
					src, err := os.ReadFile(pos.Filename)
					if err != nil {
						return nil, err
					}
					return &mover{ws: ws, pkg: p, file: f, path: pos.Filename, src: src, decl: d}, nil
				}
			}
			return nil, refactor.Refusef("%s (declared at %s) is not a top-level declaration", id.Name, ws.Position(obj.Pos()))
		}
	}
	return nil, refactor.Refusef("the declaration of %s is not in a loaded package", id.Name)
}

// declares reports whether d declares a name at the given offset.
func declares(fset *token.FileSet, d ast.Decl, offset int) bool {
	at := func(id *ast.Ident) bool { return id != nil && fset.Position(id.Pos()).Offset == offset }
	switch d := d.(type) {
	case *ast.FuncDecl:
		return at(d.Name)
	case *ast.GenDecl:
		for _, s := range d.Specs {
			switch s := s.(type) {
			case *ast.TypeSpec:
				if at(s.Name) {
					return true
				}
			case *ast.ValueSpec:
				for _, n := range s.Names {
					if at(n) {
						return true
					}
				}
			}
		}
	}
	return false
}

// checkDecl refuses declarations that cannot move, and finds the text to
// move.
func (m *mover) checkDecl() error {
	var doc *ast.CommentGroup
	switch d := m.decl.(type) {
	case *ast.FuncDecl:
		doc = d.Doc
		if d.Recv == nil && d.Name.Name == "init" {
			return refactor.Refusef("init functions run in the order of their files, so moving one could change behavior; this is not supported")
		}
	case *ast.GenDecl:
		doc = d.Doc
		if len(d.Specs) > 1 {
			return refactor.Refusef("the declaration is part of a grouped declaration (at %s) with %d others; moving one out of a group is not supported yet",
				m.ws.Position(d.Pos()), len(d.Specs)-1)
		}
		if vs, ok := d.Specs[0].(*ast.ValueSpec); ok {
			if d.Tok == token.VAR && hasEffect(m.pkg.TypesInfo, vs) {
				return refactor.Refusef("the initializer of %s has side effects, and variables are initialized in the order of their files, so moving it could change the initialization order; this is not supported", vs.Names[0].Name)
			}
			if hasDirective(d.Doc, "//go:embed") || hasDirective(vs.Doc, "//go:embed") {
				return refactor.Refusef("%s has a //go:embed directive; moving embedded variables is not supported yet", vs.Names[0].Name)
			}
		}
	}
	m.start = m.off(m.decl.Pos())
	if doc != nil {
		m.start = m.off(doc.Pos())
	}
	m.end = m.off(m.decl.End())
	if rest := m.src[m.end:m.nextLine(m.end)]; strings.HasPrefix(strings.TrimSpace(string(rest)), "//") {
		m.end += len(strings.TrimRight(string(rest), "\n"))
	}
	return nil
}

// checkDestination refuses a destination outside the source's package or
// built differently, and returns its syntax if it exists.
func (m *mover) checkDestination(to string) (*ast.File, error) {
	rel := m.ws.Rel(m.path)
	switch {
	case to == m.path:
		return nil, refactor.Refusef("the declaration is already in %s", rel)
	case !strings.HasSuffix(to, ".go"):
		return nil, refactor.Refusef("to_file must be a .go file")
	case filepath.Dir(to) != filepath.Dir(m.path):
		return nil, refactor.Refusef("%s is in another directory, so another package; moving to another package is not supported yet", m.ws.Rel(to))
	case strings.HasSuffix(to, "_test.go") != strings.HasSuffix(m.path, "_test.go"):
		return nil, refactor.Refusef("only one of %s and %s is a test file, so they are built differently; moving between them is not supported", rel, m.ws.Rel(to))
	}
	for _, imp := range m.file.Imports {
		if imp.Path.Value == `"C"` {
			return nil, refactor.Refusef("%s uses cgo; moving declarations out of cgo files is not supported", rel)
		}
	}
	if constrained(m.path, m.file) {
		return nil, refactor.Refusef("%s has build constraints, so it is not built everywhere; moving out of it is not supported", rel)
	}
	if _, err := os.Stat(to); os.IsNotExist(err) {
		if !buildsEverywhere(to) {
			return nil, refactor.Refusef("%s would have build constraints (from its name), so it would not be built everywhere; choose another name", m.ws.Rel(to))
		}
		return nil, nil
	}
	_, dst := m.ws.FileSyntax(to)
	if dst == nil {
		return nil, refactor.Refusef("%s exists but is not part of any loaded package", m.ws.Rel(to))
	}
	if dst.Name.Name != m.file.Name.Name {
		return nil, refactor.Refusef("%s is in package %s, not %s", m.ws.Rel(to), dst.Name.Name, m.file.Name.Name)
	}
	if constrained(to, dst) {
		return nil, refactor.Refusef("%s has build constraints, so it is not built everywhere; moving into it is not supported", m.ws.Rel(to))
	}
	return dst, nil
}

// collectImports finds the imports the moved code uses, and refuses code
// that uses a dot import.
func (m *mover) collectImports() error {
	info := m.pkg.TypesInfo
	used := map[*types.PkgName]bool{}
	sels := map[*ast.Ident]bool{}
	var err error
	ast.Inspect(m.decl, func(n ast.Node) bool {
		if s, ok := n.(*ast.SelectorExpr); ok {
			sels[s.Sel] = true
		}
		id, ok := n.(*ast.Ident)
		if !ok || err != nil {
			return err == nil
		}
		switch o := info.Uses[id].(type) {
		case *types.PkgName:
			used[o] = true
		case nil:
		default:
			if !sels[id] && o.Pkg() != nil && o.Pkg() != m.pkg.Types && o.Parent() == o.Pkg().Scope() {
				err = refactor.Refusef("%s at %s comes from a dot import; moving code that uses dot imports is not supported", id.Name, m.ws.Position(id.Pos()))
			}
		}
		return true
	})
	for _, imp := range m.file.Imports {
		if pn := m.pkgName(imp); pn != nil && used[pn] {
			m.imports = append(m.imports, imp)
		}
	}
	return err
}

func (m *mover) pkgName(imp *ast.ImportSpec) *types.PkgName {
	var o types.Object
	if imp.Name != nil {
		o = m.pkg.TypesInfo.Defs[imp.Name]
	} else {
		o = m.pkg.TypesInfo.Implicits[imp]
	}
	pn, _ := o.(*types.PkgName)
	return pn
}

// removeEdits deletes the source's imports that only the moved code uses:
// each such spec's line, or the whole import declaration when none of its
// specs remain.
func (m *mover) removeEdits() []edit.TextEdit {
	inMoved := func(p token.Pos) bool { o := m.off(p); return m.start <= o && o < m.end }
	unused := map[*ast.ImportSpec]bool{}
	for _, imp := range m.imports {
		pn := m.pkgName(imp)
		kept := false
		for id, o := range m.pkg.TypesInfo.Uses {
			if o == pn && !inMoved(id.Pos()) {
				kept = true
				break
			}
		}
		if !kept {
			unused[imp] = true
		}
	}
	var edits []edit.TextEdit
	for _, d := range m.file.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.IMPORT {
			continue
		}
		var gone []ast.Spec
		for _, s := range g.Specs {
			if unused[s.(*ast.ImportSpec)] {
				gone = append(gone, s)
			}
		}
		if len(gone) == len(g.Specs) && len(gone) > 0 {
			edits = append(edits, edit.TextEdit{Start: m.lineStart(m.off(g.Pos())), End: m.nextLine(m.off(g.End()))})
			continue
		}
		for _, s := range gone {
			edits = append(edits, edit.TextEdit{Start: m.lineStart(m.off(s.Pos())), End: m.nextLine(m.off(s.End()))})
		}
	}
	return edits
}

// newFile is the destination's contents when it does not exist: the
// package clause, the imports the code needs as the source writes them,
// and the code.
func (m *mover) newFile() string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", m.file.Name.Name)
	var specs []string
	for _, imp := range m.imports {
		specs = append(specs, string(m.src[m.off(imp.Pos()):m.off(imp.End())]))
	}
	switch len(specs) {
	case 0:
	case 1:
		fmt.Fprintf(&b, "import %s\n\n", specs[0])
	default:
		fmt.Fprintf(&b, "import (\n\t%s\n)\n\n", strings.Join(specs, "\n\t"))
	}
	b.WriteString(string(m.src[m.start:m.end]))
	b.WriteString("\n")
	return b.String()
}

// appendTo adds the code at the end of an existing file, with the imports
// it needs. Where the file imports a package under another name, the
// code's qualifiers change to match.
func (m *mover) appendTo(to string, dst *ast.File) (edit.FileEdit, error) {
	dstPkg, _ := m.ws.FileSyntax(to)
	dstSrc, err := os.ReadFile(to)
	if err != nil {
		return edit.FileEdit{}, err
	}
	adder := imports.New(m.ws, dstPkg, dst, dstSrc)
	names := map[*types.PkgName]string{}
	for _, imp := range m.imports {
		pn := m.pkgName(imp)
		q, err := adder.Name(pn.Imported())
		if err != nil {
			return edit.FileEdit{}, refactor.Refusef("%s cannot import %s: %v", m.ws.Rel(to), pn.Imported().Path(), err)
		}
		names[pn] = q
	}
	var renames []edit.TextEdit
	ast.Inspect(m.decl, func(n ast.Node) bool {
		s, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		x, ok := s.X.(*ast.Ident)
		if !ok {
			return true
		}
		pn, ok := m.pkg.TypesInfo.Uses[x].(*types.PkgName)
		if !ok || names[pn] == pn.Name() {
			return true
		}
		if q := names[pn]; q == "" { // a dot import in the destination
			renames = append(renames, edit.TextEdit{Start: m.off(x.Pos()) - m.start, End: m.off(s.Sel.Pos()) - m.start})
		} else {
			renames = append(renames, edit.TextEdit{Start: m.off(x.Pos()) - m.start, End: m.off(x.End()) - m.start, NewText: q})
		}
		return true
	})
	code, err := edit.Apply(m.src[m.start:m.end], renames)
	if err != nil {
		return edit.FileEdit{}, err
	}
	edits := append(adder.Edits(), edit.TextEdit{Start: len(dstSrc), End: len(dstSrc), NewText: "\n" + string(code) + "\n"})
	return edit.FileEdit{Path: to, Edits: edits}, nil
}

func (m *mover) off(p token.Pos) int { return m.ws.Fset.Position(p).Offset }

func (m *mover) lineStart(off int) int {
	return strings.LastIndexByte(string(m.src[:off]), '\n') + 1
}

// nextLine returns the offset just past the newline that ends off's line.
func (m *mover) nextLine(off int) int {
	if i := strings.IndexByte(string(m.src[off:]), '\n'); i >= 0 {
		return off + i + 1
	}
	return len(m.src)
}

// hasEffect reports whether evaluating the spec's values could have a
// side effect. (Defining a function literal runs nothing.)
func hasEffect(info *types.Info, vs *ast.ValueSpec) bool {
	found := false
	for _, v := range vs.Values {
		ast.Inspect(v, func(n ast.Node) bool {
			if _, ok := n.(*ast.FuncLit); ok || found {
				return false
			}
			found = analysis.HasEffect(info, n)
			return !found
		})
	}
	return found
}

func hasDirective(doc *ast.CommentGroup, prefix string) bool {
	if doc == nil {
		return false
	}
	for _, c := range doc.List {
		if strings.HasPrefix(c.Text, prefix) {
			return true
		}
	}
	return false
}

// constrained reports whether a file is built only for some platforms or
// tags: a //go:build line, or a GOOS or GOARCH in its name.
func constrained(path string, f *ast.File) bool {
	for _, cg := range f.Comments {
		if cg.Pos() > f.Package {
			break
		}
		for _, c := range cg.List {
			if strings.HasPrefix(c.Text, "//go:build") || strings.HasPrefix(c.Text, "// +build") {
				return true
			}
		}
	}
	return !buildsEverywhere(path)
}

// buildsEverywhere reports whether a file's name lets it build on any
// platform: it is built for two platforms that share no GOOS or GOARCH.
func buildsEverywhere(path string) bool {
	for _, p := range [][2]string{{"linux", "amd64"}, {"windows", "arm64"}} {
		ctxt := build.Default
		ctxt.GOOS, ctxt.GOARCH = p[0], p[1]
		ctxt.OpenFile = func(string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("package p\n")), nil
		}
		if ok, err := ctxt.MatchFile(filepath.Dir(path), filepath.Base(path)); err != nil || !ok {
			return false
		}
	}
	return true
}
