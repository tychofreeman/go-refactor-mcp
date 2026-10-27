// Package movedecl implements move_declaration: move a top-level
// declaration to another file of the same package, or to another package
// (see otherpkg.go).
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
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/site"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// MoveDeclaration is the "move_declaration" refactoring.
type MoveDeclaration struct{}

// New returns the move_declaration refactoring.
func New() *MoveDeclaration { return &MoveDeclaration{} }

func (*MoveDeclaration) Name() string { return "move_declaration" }

func (*MoveDeclaration) Description() string {
	return `Move a top-level declaration (function, method, type, variable or constant, with its doc comment) to another file of its package or of another package, creating the file and package if needed: moving defaultHost from notify/notify.go to config/config.go with new_name DefaultHost declares func DefaultHost there and turns defaultHost() into config.DefaultHost(). Imports follow the code; a file left with only its package clause is deleted.
Identify the declaration by file and name, or by line at it or any use; to_file is the destination, relative to the workspace root. also names more declarations of the package to move with it, such as the helpers it uses. A type takes its methods along to another package; a declaration grouped in parentheses moves out as its own. To another package, an unexported declaration used outside it needs an exported new_name.
Refuses, saying why: moving between a test file and a non-test file or to or from a file with build constraints; init, or a variable whose initializer has side effects; a method on its own to another package; code that uses declarations of its package that stay behind (move them with it, in also); an import cycle.`
}

func (*MoveDeclaration) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":    {"type": "string",  "description": "Path of a file containing the declaration or a use of it, relative to the workspace root."},
    "line":    {"type": "integer", "description": "1-based line of that occurrence. Omit it to find name as a symbol of the package of file."},
    "name":    {"type": "string",  "description": "The declared name: with line, just the method name for a method; without line, the package-level name or Type.Method."},
    "column":  {"type": "integer", "description": "Optional 1-based column, only needed to disambiguate."},
    "to_file": {"type": "string",  "description": "Destination file, relative to the workspace root; created if it does not exist. In another directory, the declaration moves to that package."},
    "new_name": {"type": "string", "description": "Optional, when moving to another package: the name in the new package (required for an unexported declaration used outside it, e.g. defaultHost -> DefaultHost)."},
    "also":     {"type": "array", "items": {"type": "string"}, "description": "Optional: other top-level declarations of the package (in any of its files) to move with it, by name (not methods), e.g. the helpers it uses. They keep their names."},
    "with_dependencies": {"type": "boolean", "description": "Optional, when moving to another package: also move every top-level declaration of the package that the moved code uses, and what those use, in place of naming them in also. One the package still uses after the move is exported (helper becomes Helper). The result lists each on a chose: line."}
  },
  "required": ["file", "name", "to_file"],
  "additionalProperties": false
}`)
}

type moveArgs struct {
	File    string   `json:"file"`
	Line    int      `json:"line"`
	Column  int      `json:"column,omitempty"`
	Name    string   `json:"name"`
	ToFile  string   `json:"to_file"`
	NewName string   `json:"new_name,omitempty"`
	Also    []string `json:"also,omitempty"`

	WithDependencies bool `json:"with_dependencies,omitempty"`
}

// mover holds what one move needs. The embedded declaration is the one
// the arguments name; decls holds it and those moving with it, in source
// order.
type mover struct {
	*moved
	decls []*moved
	// The embedded source is the file of the declaration the arguments
	// name; sources holds it and the others declarations move from.
	*source
	sources []*source

	// Moving to another package (which created means the move creates):
	dest    *packages.Package
	created bool
	files   map[string]*fileEdits
	// emptied holds the source files the move leaves with only a package
	// clause, which it deletes.
	emptied []string

	// withDeps moves the declarations the moved code uses along;
	// needed records, for each one added that way, the moved code that
	// needed it, for the result's choices.
	withDeps bool
	needed   map[*moved]string
}

// source is a file that declarations move from.
type source struct {
	path string
	site.Site
	imports []*ast.ImportSpec // those the moved code uses
}

// moved is one declaration a move carries.
type moved struct {
	from *source
	decl ast.Decl
	// spec is the spec declaring obj when decl groups several in
	// parentheses; it moves out of the group as a declaration of its own,
	// unless whole: every spec of the group moves, and so the group as it
	// is.
	spec    ast.Spec
	whole   bool
	obj     types.Object // the (first) name it declares
	start   int          // the declaration's text, with its doc comment
	end     int          // and a comment that ends its last line
	newName string       // its name at the destination

	usedOutside bool // referred to outside the destination package
}

// fileEdits collects the edits to one file and the imports to add or
// remove; cuts are the ranges whose package references go away, which
// can leave an import unused, and needs the import paths that new
// references use.
type fileEdits struct {
	pkg   *packages.Package
	file  *ast.File
	adder *imports.Adder
	edits []edit.TextEdit
	cuts  [][2]int
	needs map[string]bool
}

// empties reports whether the edits leave the source file at path with
// nothing but its package clause: no declarations, imports or comments.
func (m *mover) empties(path string, edits []edit.TextEdit) bool {
	i := slices.IndexFunc(m.sources, func(s *source) bool { return s.path == path })
	if i < 0 {
		return false
	}
	after, err := edit.Apply(m.sources[i].Src, edits)
	if err != nil {
		return false
	}
	f, err := parser.ParseFile(token.NewFileSet(), path, after, parser.ParseComments)
	return err == nil && len(f.Decls) == 0 && len(f.Comments) == 0
}

// emptiedNotes notes the files the move deletes.
func (m *mover) emptiedNotes() []string {
	var notes []string
	for _, path := range m.emptied {
		notes = append(notes, fmt.Sprintf("deleted %s, which the move left empty", m.WS.Rel(path)))
	}
	return notes
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
	if err := m.addAlso(a.Also); err != nil {
		return nil, err
	}
	for _, d := range m.decls {
		if err := m.checkDecl(d); err != nil {
			return nil, err
		}
		d.newName = d.obj.Name()
	}
	to := ws.Abs(a.ToFile)
	if filepath.Dir(to) != filepath.Dir(m.path) {
		if a.NewName != "" {
			m.newName = a.NewName
		}
		m.withDeps = a.WithDependencies
		return m.planOtherPackage(to)
	}
	if a.NewName != "" && a.NewName != m.obj.Name() {
		return nil, refactor.Refusef("within a package the name does not change; use rename for that")
	}
	dst, err := m.checkDestination(to)
	if err != nil {
		return nil, err
	}
	if err := m.collectImports(); err != nil {
		return nil, err
	}
	m.files = map[string]*fileEdits{}
	m.deleteDecls()
	same := func(pn *types.PkgName) string { return pn.Name() }
	if dst == nil {
		text, err := m.newFile(same)
		if err != nil {
			return nil, err
		}
		m.files[to] = &fileEdits{edits: []edit.TextEdit{{NewText: text}}}
	} else if err := m.appendTo(to, ""); err != nil {
		return nil, err
	}
	edits := m.fileEdits()
	return &refactor.Plan{
		Summary: fmt.Sprintf("moved %s%s from %s to %s", a.Name, m.withAlso(), m.fromFiles(), ws.Rel(to)),
		Edits:   edits,
		Notes:   m.emptiedNotes(),
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
					s := &source{path: pos.Filename, Site: site.Site{WS: ws, Pkg: p, Info: p.TypesInfo, File: f, TF: ws.Fset.File(f.Pos()), Src: src}}
					md := &moved{from: s, decl: d, obj: obj, spec: groupSpec(ws.Fset, d, pos.Offset)}
					return &mover{moved: md, decls: []*moved{md}, source: s, sources: []*source{s}}, nil
				}
			}
			return nil, refactor.Refusef("%s (declared at %s) is not a top-level declaration", id.Name, ws.Position(obj.Pos()))
		}
	}
	return nil, refactor.Refusef("the declaration of %s is not in a loaded package", id.Name)
}

// addAlso adds the declarations named in also, which must be top-level
// declarations of the source file, keeping m.decls in source order.
func (m *mover) addAlso(also []string) error {
	for _, name := range also {
		o := m.lookup(name)
		if o == nil {
			return refactor.Refusef("package %s declares no top-level %s (also names functions, types, variables and constants, not methods)", m.Pkg.Name, name)
		}
		pos := m.WS.Fset.Position(o.Pos())
		from, err := m.sourceFor(pos.Filename)
		if err != nil {
			return err
		}
		for _, d := range from.File.Decls {
			if spec := groupSpec(m.WS.Fset, d, pos.Offset); declares(m.WS.Fset, d, pos.Offset) && !m.moves(d, spec) {
				m.decls = append(m.decls, &moved{from: from, decl: d, obj: o, spec: spec})
			}
		}
	}
	m.sortDecls()
	m.markWhole()
	return nil
}

// lookup finds a top-level name of the package, in any of its variants
// (a test file's declarations are only in the test variant).
func (m *mover) lookup(name string) types.Object {
	if o := m.Pkg.Types.Scope().Lookup(name); o != nil {
		return o
	}
	for _, p := range m.WS.Pkgs {
		if p.Types != nil && p.PkgPath == m.Pkg.PkgPath {
			if o := p.Types.Scope().Lookup(name); o != nil {
				return o
			}
		}
	}
	return nil
}

// sourceFor returns the source for path, a file of the package, taking
// its syntax from the variant the move uses when it has the file.
func (m *mover) sourceFor(path string) (*source, error) {
	for _, s := range m.sources {
		if s.path == path {
			return s, nil
		}
	}
	var file *ast.File
	var fp *packages.Package // the variant whose syntax holds it
	for _, p := range append([]*packages.Package{m.Pkg}, m.WS.Pkgs...) {
		if p.PkgPath != m.Pkg.PkgPath || file != nil {
			continue
		}
		for _, f := range p.Syntax {
			if m.WS.Fset.File(f.Pos()).Name() == path {
				file, fp = f, p
			}
		}
	}
	if file == nil {
		return nil, refactor.Refusef("%s is not a file of package %s", m.WS.Rel(path), m.Pkg.Name)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := &source{path: path, Site: site.Site{WS: m.WS, Pkg: fp, Info: fp.TypesInfo, File: file, TF: m.WS.Fset.File(file.Pos()), Src: src}}
	m.sources = append(m.sources, s)
	return s, nil
}

// sortDecls puts the moved declarations in order: by source (the named
// declaration's file first), then as each file has them.
func (m *mover) sortDecls() {
	rank := map[*source]int{}
	for i, s := range m.sources {
		rank[s] = i
	}
	sort.SliceStable(m.decls, func(i, j int) bool {
		a, b := m.decls[i], m.decls[j]
		if a.from != b.from {
			return rank[a.from] < rank[b.from]
		}
		return a.node().Pos() < b.node().Pos()
	})
}

// fromFiles names the source files, for a summary.
func (m *mover) fromFiles() string {
	var names []string
	for _, s := range m.sources {
		names = append(names, m.WS.Rel(s.path))
	}
	return strings.Join(names, ", ")
}

// moves reports whether the declaration (or, in a group, the spec) is
// already among those moved.
func (m *mover) moves(decl ast.Decl, spec ast.Spec) bool {
	for _, d := range m.decls {
		if d.decl == decl && d.spec == spec {
			return true
		}
	}
	return false
}

// node is the syntax that moves: the spec, or the whole declaration.
func (d *moved) node() ast.Node {
	if d.spec != nil && !d.whole {
		return d.spec
	}
	return d.decl
}

// groupSpec returns the spec of d declaring the name at offset when d
// groups several specs, or nil.
func groupSpec(fset *token.FileSet, d ast.Decl, offset int) ast.Spec {
	g, ok := d.(*ast.GenDecl)
	if !ok || len(g.Specs) < 2 {
		return nil
	}
	for _, s := range g.Specs {
		if declares(fset, &ast.GenDecl{Specs: []ast.Spec{s}}, offset) {
			return s
		}
	}
	return nil
}

// markWhole marks the specs of groups that move entirely.
func (m *mover) markWhole() {
	count := map[ast.Decl]int{}
	for _, d := range m.decls {
		if d.spec != nil {
			count[d.decl]++
		}
	}
	for _, d := range m.decls {
		if d.spec != nil {
			d.whole = count[d.decl] == len(d.decl.(*ast.GenDecl).Specs)
		}
	}
}

// units returns the moved text's pieces: each declaration once (a group
// that moves whole is one piece), in source order.
func (m *mover) units() []*moved {
	var out []*moved
	seen := map[ast.Decl]bool{}
	for _, d := range m.decls {
		if d.whole && seen[d.decl] {
			continue
		}
		seen[d.decl] = true
		out = append(out, d)
	}
	return out
}

// withAlso names, for a summary, the declarations moved along with the
// one the arguments name.
func (m *mover) withAlso() string {
	var names []string
	for _, d := range m.decls {
		if d != m.moved {
			names = append(names, d.obj.Name())
		}
	}
	if len(names) == 0 {
		return ""
	}
	return " with " + strings.Join(names, ", ")
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
func (m *mover) checkDecl(d *moved) error {
	switch decl := d.decl.(type) {
	case *ast.FuncDecl:
		if decl.Recv == nil && decl.Name.Name == "init" {
			return refactor.Refusef("init functions run in the order of their files, so moving one could change behavior; this is not supported")
		}
	case *ast.GenDecl:
		specs := decl.Specs
		if d.spec != nil {
			if !d.whole {
				if err := m.checkLeavesGroup(decl, d.spec); err != nil {
					return err
				}
			}
			specs = []ast.Spec{d.spec}
		}
		for _, sp := range specs {
			vs, ok := sp.(*ast.ValueSpec)
			if !ok {
				continue
			}
			if decl.Tok == token.VAR && hasEffect(m.Pkg.TypesInfo, vs, m.WS.Effects()) {
				return refactor.Refusef("the initializer of %s has side effects, and variables are initialized in the order of their files, so moving it could change the initialization order; this is not supported", vs.Names[0].Name)
			}
			if hasDirective(decl.Doc, "//go:embed") || hasDirective(vs.Doc, "//go:embed") {
				return refactor.Refusef("%s has a //go:embed directive; moving embedded variables is not supported yet", vs.Names[0].Name)
			}
		}
	}
	m.setRange(d)
	return nil
}

// setRange finds the moved text: the declaration (or spec) with its doc
// comment and a comment ending its last line.
func (m *mover) setRange(d *moved) {
	d.start = m.Off(d.node().Pos())
	if doc := docOf(d.node()); doc != nil {
		d.start = m.Off(doc.Pos())
	}
	d.end = m.Off(d.node().End())
	if rest := d.from.Src[d.end:d.from.nextLine(d.end)]; strings.HasPrefix(strings.TrimSpace(string(rest)), "//") {
		d.end += len(strings.TrimRight(string(rest), "\n"))
	}
}

// checkDestination refuses a destination outside the source's package or
// built differently, and returns its syntax if it exists.
func (m *mover) checkDestination(to string) (*ast.File, error) {
	if to == m.path {
		return nil, refactor.Refusef("the declaration is already in %s", m.WS.Rel(to))
	}
	for _, d := range m.decls {
		if d.from.path == to {
			return nil, refactor.Refusef("%s is already in %s", d.obj.Name(), m.WS.Rel(to))
		}
	}
	switch {
	case !strings.HasSuffix(to, ".go"):
		return nil, refactor.Refusef("to_file must be a .go file")
	case filepath.Dir(to) != filepath.Dir(m.path):
		return nil, refactor.Refusef("%s is in another directory, so another package; moving to another package is not supported yet", m.WS.Rel(to))
	}
	for _, s := range m.sources {
		rel := m.WS.Rel(s.path)
		if strings.HasSuffix(to, "_test.go") != strings.HasSuffix(s.path, "_test.go") {
			return nil, refactor.Refusef("only one of %s and %s is a test file, so they are built differently; moving between them is not supported", rel, m.WS.Rel(to))
		}
		if err := s.checkBuiltEverywhere(rel); err != nil {
			return nil, err
		}
	}
	if _, err := os.Stat(to); os.IsNotExist(err) {
		if !buildsEverywhere(to) {
			return nil, refactor.Refusef("%s would have build constraints (from its name), so it would not be built everywhere; choose another name", m.WS.Rel(to))
		}
		return nil, nil
	}
	_, dst := m.WS.FileSyntax(to)
	if dst == nil {
		return nil, refactor.Refusef("%s exists but is not part of any loaded package", m.WS.Rel(to))
	}
	if dst.Name.Name != m.File.Name.Name {
		return nil, refactor.Refusef("%s is in package %s, not %s", m.WS.Rel(to), dst.Name.Name, m.File.Name.Name)
	}
	if constrained(to, dst) {
		return nil, refactor.Refusef("%s has build constraints, so it is not built everywhere; moving into it is not supported", m.WS.Rel(to))
	}
	return dst, nil
}

// collectImports finds the imports the moved code uses, and refuses code
// that uses a dot import.
func (m *mover) collectImports() error {
	info := m.Pkg.TypesInfo
	used := map[*types.PkgName]bool{}
	sels := map[*ast.Ident]bool{}
	var err error
	for _, d := range m.decls {
		ast.Inspect(d.node(), func(n ast.Node) bool {
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
				if !sels[id] && o.Pkg() != nil && o.Pkg() != m.Pkg.Types && o.Parent() == o.Pkg().Scope() {
					err = refactor.Refusef("%s at %s comes from a dot import; moving code that uses dot imports is not supported", id.Name, m.WS.Position(id.Pos()))
				}
			}
			return true
		})
	}
	for _, s := range m.sources {
		for _, imp := range s.File.Imports {
			if pn := pkgNameOf(m.Pkg.TypesInfo, imp); pn != nil && used[pn] {
				s.imports = append(s.imports, imp)
			}
		}
	}
	return err
}

// checkBuiltEverywhere refuses a source file that is not built for every
// platform: build constraints, or cgo.
func (s *source) checkBuiltEverywhere(rel string) error {
	for _, imp := range s.File.Imports {
		if imp.Path.Value == `"C"` {
			return refactor.Refusef("%s uses cgo; moving declarations out of cgo files is not supported", rel)
		}
	}
	if constrained(s.path, s.File) {
		return refactor.Refusef("%s has build constraints, so it is not built everywhere; moving out of it is not supported", rel)
	}
	return nil
}

// allImports returns the imports the moved code uses, from every source.
func (m *mover) allImports() []*ast.ImportSpec {
	var out []*ast.ImportSpec
	for _, s := range m.sources {
		out = append(out, s.imports...)
	}
	return out
}

// pkgNameOf returns the name an import declares (nil for a blank or dot
// import).
func pkgNameOf(info *types.Info, imp *ast.ImportSpec) *types.PkgName {
	var o types.Object
	if imp.Name != nil {
		o = info.Defs[imp.Name]
	} else {
		o = info.Implicits[imp]
	}
	pn, _ := o.(*types.PkgName)
	if pn != nil && (pn.Name() == "_" || pn.Name() == ".") {
		return nil
	}
	return pn
}

// is reports whether o is a moved declaration's object, in any package
// variant.
func (m *mover) is(o types.Object) bool { return m.movedOf(o) != nil }

// movedOf returns the moved declaration whose object is o, in any package
// variant, or nil.
func (m *mover) movedOf(o types.Object) *moved {
	if o == nil {
		return nil
	}
	for _, d := range m.decls {
		if analysis.ObjectKey(m.WS.Fset, o) == analysis.ObjectKey(m.WS.Fset, d.obj) {
			return d
		}
	}
	return nil
}

// fileFor returns the edits to the file at path, which must be in a
// loaded package.
func (m *mover) fileFor(path string) (*fileEdits, error) {
	if fe := m.files[path]; fe != nil {
		return fe, nil
	}
	opened, err := site.At(m.WS, path)
	if err != nil {
		return nil, err
	}
	fe := &fileEdits{pkg: opened.Pkg, file: opened.File, adder: imports.New(m.WS, opened.Pkg, opened.File, opened.Src), needs: map[string]bool{}}
	m.files[path] = fe
	return fe, nil
}

// deleteDecls deletes the moved declarations' lines from the source file.
func (m *mover) deleteDecls() {
	for _, d := range m.units() {
		m.deleteDecl(d)
	}
}

// deleteDecl deletes the declaration's lines from the source file.
func (m *mover) deleteDecl(d *moved) {
	fe, _ := m.fileFor(d.from.path) // the source file is loaded
	fe.edits = append(fe.edits, edit.TextEdit{Start: d.from.lineStart(d.start), End: d.from.nextLine(d.end)})
	fe.cuts = append(fe.cuts, [2]int{d.start, d.end})
}

// fileEdits removes the imports the cuts leave unused and returns every
// file's edits.
func (m *mover) fileEdits() []edit.FileEdit {
	var fes []edit.FileEdit
	for path, fe := range m.files {
		if fe.adder != nil {
			fe.dropUnused(m.WS)
			fe.edits = append(fe.edits, fe.adder.Edits()...)
		}
		if m.empties(path, fe.edits) {
			m.emptied = append(m.emptied, path)
			fes = append(fes, edit.FileEdit{Path: path, Delete: true})
			continue
		}
		fes = append(fes, edit.FileEdit{Path: path, Edits: fe.edits})
	}
	sort.Slice(fes, func(i, j int) bool { return fes[i].Path < fes[j].Path })
	sort.Strings(m.emptied)
	return edit.Merge(fes)
}

// dropUnused removes the file's imports that are used only inside cuts.
func (fe *fileEdits) dropUnused(ws *workspace.Workspace) {
	info := fe.pkg.TypesInfo
	cut := func(p token.Pos) bool {
		o := ws.Fset.Position(p).Offset
		for _, c := range fe.cuts {
			if c[0] <= o && o < c[1] {
				return true
			}
		}
		return false
	}
	fileStart, fileEnd := fe.file.FileStart, fe.file.FileEnd
	for _, imp := range fe.file.Imports {
		pn := pkgNameOf(info, imp)
		if pn == nil || fe.needs[pn.Imported().Path()] {
			continue
		}
		used, kept := false, false
		for id, o := range info.Uses {
			if o != pn || id.Pos() < fileStart || id.Pos() >= fileEnd {
				continue
			}
			if cut(id.Pos()) {
				used = true
			} else {
				kept = true
			}
		}
		if used && !kept {
			fe.adder.Remove(imp)
		}
	}
}

// code returns a moved declaration's text for its destination: qual
// gives the name there of each package it refers to ("" for the
// destination's own package, whose qualifier goes), and references to the
// moved declarations use their new names.
func (m *mover) code(d *moved, qual func(*types.PkgName) string) (string, error) {
	info := m.Pkg.TypesInfo
	var edits []edit.TextEdit
	rel := func(p token.Pos) int { return m.Off(p) - d.start }
	ast.Inspect(d.node(), func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			x, ok := n.X.(*ast.Ident)
			if !ok {
				return true
			}
			pn, ok := info.Uses[x].(*types.PkgName)
			if !ok {
				return true
			}
			switch q := qual(pn); {
			case q == "":
				edits = append(edits, edit.TextEdit{Start: rel(x.Pos()), End: rel(n.Sel.Pos())})
			case q != pn.Name():
				edits = append(edits, edit.TextEdit{Start: rel(x.Pos()), End: rel(x.End()), NewText: q})
			}
			return false
		case *ast.Ident:
			o := info.Defs[n]
			if o == nil {
				o = info.Uses[n]
			}
			if r := m.movedOf(o); r != nil && r.newName != r.obj.Name() && n.Name == r.obj.Name() {
				edits = append(edits, edit.TextEdit{Start: rel(n.Pos()), End: rel(n.End()), NewText: r.newName})
			}
		}
		return true
	})
	if doc := docOf(d.node()); doc != nil && d.newName != d.obj.Name() && !d.whole {
		// The doc comment names the declaration first, by convention.
		if c := doc.List[0]; strings.HasPrefix(c.Text, "// "+d.obj.Name()+" ") {
			at := rel(c.Pos()) + len("// ")
			edits = append(edits, edit.TextEdit{Start: at, End: at + len(d.obj.Name()), NewText: d.newName})
		}
	}
	if d.node() == d.spec {
		// Out of its group: "x = 1" becomes "var x = 1", one level less
		// indented (unless a raw string spans lines, which must not change).
		kw := d.decl.(*ast.GenDecl).Tok.String()
		at := rel(d.spec.Pos())
		edits = append(edits, edit.TextEdit{Start: at, End: at, NewText: kw + " "})
	}
	out, err := edit.Apply(d.from.Src[d.start:d.end], edits)
	if err != nil {
		return "", err
	}
	if d.node() == d.spec && !multiLineRaw(d.spec) {
		out = []byte(strings.ReplaceAll(string(out), "\n\t", "\n"))
	}
	return string(out), nil
}

// multiLineRaw reports whether n contains a raw string literal spanning
// lines.
func multiLineRaw(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		if b, ok := n.(*ast.BasicLit); ok && b.Kind == token.STRING && strings.HasPrefix(b.Value, "`") && strings.Contains(b.Value, "\n") {
			found = true
		}
		return !found
	})
	return found
}

// checkLeavesGroup refuses taking spec out of the group decl when that
// changes a constant: in a const group, iota is a spec's index, and a spec
// without values repeats the previous expression.
func (m *mover) checkLeavesGroup(decl *ast.GenDecl, spec ast.Spec) error {
	if decl.Tok != token.CONST {
		return nil
	}
	name := spec.(*ast.ValueSpec).Names[0].Name
	i := slices.Index(decl.Specs, spec)
	vs := spec.(*ast.ValueSpec)
	switch {
	case len(vs.Values) == 0:
		return refactor.Refusef("%s has no value of its own: it repeats the expression of an earlier constant in the group, with its own iota; move the whole group (name the others in also)", name)
	case analysis.DependsOnPosition(m.Pkg.TypesInfo, vs):
		return refactor.Refusef("%s uses iota, its position in the group; move the whole group (name the others in also)", name)
	}
	for _, later := range decl.Specs[i+1:] {
		if lv := later.(*ast.ValueSpec); analysis.DependsOnPosition(m.Pkg.TypesInfo, lv) {
			return refactor.Refusef("%s (at %s) uses iota or repeats an earlier expression, so its value depends on the position of %s; move the whole group (name the others in also)",
				lv.Names[0].Name, m.WS.Position(lv.Pos()), name)
		}
	}
	return nil
}

// allCode returns the moved declarations' text for their destination, in
// source order (see code).
func (m *mover) allCode(qual func(*types.PkgName) string) (string, error) {
	var codes []string
	for _, d := range m.units() {
		c, err := m.code(d, qual)
		if err != nil {
			return "", err
		}
		codes = append(codes, c)
	}
	return strings.Join(codes, "\n\n"), nil
}

func docOf(n ast.Node) *ast.CommentGroup {
	switch n := n.(type) {
	case *ast.FuncDecl:
		return n.Doc
	case *ast.GenDecl:
		return n.Doc
	case *ast.ValueSpec:
		return n.Doc
	case *ast.TypeSpec:
		return n.Doc
	}
	return nil
}

// newFile is the destination's contents when it does not exist: the
// package clause, the imports the code needs as the source writes them,
// and the code.
func (m *mover) newFile(qual func(*types.PkgName) string) (string, error) {
	var b strings.Builder
	name := m.File.Name.Name
	if m.dest != nil {
		name = m.dest.Name
	}
	fmt.Fprintf(&b, "package %s\n\n", name)
	type spec struct{ path, text string }
	var specs []spec
	byPath := map[string]*source{} // which source first imports a path
	for _, s := range m.sources {
		for _, imp := range s.imports {
			pn := pkgNameOf(m.Pkg.TypesInfo, imp)
			if qual(pn) == "" {
				continue
			}
			path := pn.Imported().Path()
			if first := byPath[path]; first != nil {
				if other := m.importName(first, path); other != pn.Name() {
					return "", refactor.Refusef("%s imports %q as %s, and %s as %s; the new file would need both names, so rename one first",
						m.WS.Rel(first.path), path, other, m.WS.Rel(s.path), pn.Name())
				}
				continue
			}
			byPath[path] = s
			specs = append(specs, spec{path, string(s.Src[m.Off(imp.Pos()):m.Off(imp.End())])})
		}
	}
	// As goimports writes them: the standard library first, then the others,
	// each sorted.
	sort.SliceStable(specs, func(i, j int) bool {
		x, y := specs[i], specs[j]
		if imports.IsStd(x.path) != imports.IsStd(y.path) {
			return imports.IsStd(x.path)
		}
		return x.path < y.path
	})
	var lines []string
	for i, sp := range specs {
		if i > 0 && imports.IsStd(specs[i-1].path) != imports.IsStd(sp.path) {
			lines = append(lines, "")
		}
		lines = append(lines, sp.text)
	}
	switch len(specs) {
	case 0:
	case 1:
		fmt.Fprintf(&b, "import %s\n\n", specs[0].text)
	default:
		fmt.Fprintf(&b, "import (\n\t%s\n)\n\n", strings.Join(lines, "\n\t"))
	}
	code, _ := m.allCode(qual) // its edits come from its own syntax, so they apply
	b.WriteString(code)
	b.WriteString("\n")
	return b.String(), nil
}

// importName returns the name the source s imports path as.
func (m *mover) importName(s *source, path string) string {
	for _, imp := range s.imports {
		if pn := pkgNameOf(m.Pkg.TypesInfo, imp); pn != nil && pn.Imported().Path() == path {
			return pn.Name()
		}
	}
	return ""
}

// appendTo adds the code at the end of an existing file, with the imports
// it needs. Where the file imports a package under another name, the
// code's qualifiers change to match; the package destPath (if any) is the
// file's own, so its qualifier goes.
func (m *mover) appendTo(to, destPath string) error {
	fe, err := m.fileFor(to)
	if err != nil {
		return err
	}
	names := map[*types.PkgName]string{}
	for _, imp := range m.allImports() {
		pn := pkgNameOf(m.Pkg.TypesInfo, imp)
		if pn.Imported().Path() == destPath {
			names[pn] = ""
			continue
		}
		q, err := fe.adder.Name(pn.Imported())
		if err != nil {
			return refactor.Refusef("%s cannot import %s: %v", m.WS.Rel(to), pn.Imported().Path(), err)
		}
		names[pn] = q
	}
	code, err := m.allCode(func(pn *types.PkgName) string { return names[pn] })
	if err != nil {
		return err
	}
	src, err := os.ReadFile(to)
	if err != nil {
		return err
	}
	fe.edits = append(fe.edits, edit.TextEdit{Start: len(src), End: len(src), NewText: "\n" + code + "\n"})
	return nil
}

func (s *source) lineStart(off int) int {
	return strings.LastIndexByte(string(s.Src[:off]), '\n') + 1
}

// nextLine returns the offset just past the newline that ends off's line.
func (s *source) nextLine(off int) int {
	if i := strings.IndexByte(string(s.Src[off:]), '\n'); i >= 0 {
		return off + i + 1
	}
	return len(s.Src)
}

// hasEffect reports whether evaluating the spec's values could have a
// side effect. (Defining a function literal runs nothing.)
func hasEffect(info *types.Info, vs *ast.ValueSpec, effs *analysis.Effects) bool {
	found := false
	for _, v := range vs.Values {
		ast.Inspect(v, func(n ast.Node) bool {
			if _, ok := n.(*ast.FuncLit); ok || found {
				return false
			}
			found = effs.Acts(info, n)
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
