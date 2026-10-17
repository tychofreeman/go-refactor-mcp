package movedecl

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
)

// planOtherPackage moves the declaration to the package in to's directory.
// Every reference changes: uses outside that package become q.Name (with
// an import of it), uses inside it lose their qualifier, and so does the
// moved code's own references to it.
func (m *mover) planOtherPackage(to string) (*refactor.Plan, error) {
	if err := m.checkOtherPackage(to); err != nil {
		return nil, err
	}
	if err := m.collectImports(); err != nil {
		return nil, err
	}
	if err := m.checkNoSourceRefs(); err != nil {
		return nil, err
	}
	m.files = map[string]*fileEdits{}
	users, refs, err := m.updateReferences()
	if err != nil {
		return nil, err
	}
	if !token.IsExported(m.newName) && len(users) > 0 {
		if m.newName != m.obj.Name() {
			return nil, refactor.Refusef("the new name %s must be exported: %s is used outside package %s", m.newName, m.obj.Name(), m.dest.Name)
		}
		return nil, refactor.Refusef("%s is unexported and used outside package %s, so it must become exported; give new_name (e.g. %s)",
			m.obj.Name(), m.dest.Name, strings.ToUpper(m.newName[:1])+m.newName[1:])
	}
	if err := m.checkNoCycle(users); err != nil {
		return nil, err
	}
	m.deleteDecl()
	if _, err := os.Stat(to); os.IsNotExist(err) {
		m.files[to] = &fileEdits{edits: []edit.TextEdit{{NewText: m.newFile(func(pn *types.PkgName) string {
			if pn.Imported().Path() == m.dest.PkgPath {
				return ""
			}
			return pn.Name()
		})}}}
	} else if err := m.appendTo(to, m.dest.PkgPath); err != nil {
		return nil, err
	}

	plan := &refactor.Plan{
		Summary: fmt.Sprintf("moved %s from %s to %s as %s.%s (%d reference%s updated)",
			m.obj.Name(), m.ws.Rel(m.path), m.ws.Rel(to), m.dest.Name, m.newName, refs, plural(refs)),
		Edits: m.fileEdits(),
	}
	if m.obj.Exported() {
		plan.Notes = append(plan.Notes, fmt.Sprintf("%s was exported: references in other modules are not updated", m.obj.Name()))
	}
	return plan, nil
}

// checkOtherPackage checks the destination package and file, and that the
// declaration can leave its package.
func (m *mover) checkOtherPackage(to string) error {
	rel := m.ws.Rel(m.path)
	switch {
	case !strings.HasSuffix(to, ".go"):
		return refactor.Refusef("to_file must be a .go file")
	case strings.HasSuffix(m.path, "_test.go") || strings.HasSuffix(to, "_test.go"):
		return refactor.Refusef("moving to or from a test file in another package is not supported yet")
	case constrained(m.path, m.file):
		return refactor.Refusef("%s has build constraints, so it is not built everywhere; moving out of it is not supported", rel)
	}
	for _, imp := range m.file.Imports {
		if imp.Path.Value == `"C"` {
			return refactor.Refusef("%s uses cgo; moving declarations out of cgo files is not supported", rel)
		}
	}
	dir := filepath.Dir(to)
	m.dest = packageIn(m.ws.Pkgs, dir)
	if m.dest == nil {
		return refactor.Refusef("%s has no Go package; creating a package is not supported yet", m.ws.Rel(dir))
	}
	if _, err := os.Stat(to); os.IsNotExist(err) {
		if !buildsEverywhere(to) {
			return refactor.Refusef("%s would have build constraints (from its name), so it would not be built everywhere; choose another name", m.ws.Rel(to))
		}
	} else {
		_, dst := m.ws.FileSyntax(to)
		if dst == nil || dst.Name.Name != m.dest.Name {
			return refactor.Refusef("%s is not part of package %s", m.ws.Rel(to), m.dest.Name)
		}
		if constrained(to, dst) {
			return refactor.Refusef("%s has build constraints, so it is not built everywhere; moving into it is not supported", m.ws.Rel(to))
		}
		for _, imp := range dst.Imports {
			if imp.Name != nil && imp.Name.Name == m.newName {
				return refactor.Refusef("%s imports a package as %s, the declaration's name", m.ws.Rel(to), m.newName)
			}
		}
	}

	switch d := m.decl.(type) {
	case *ast.FuncDecl:
		if d.Recv != nil {
			return refactor.Refusef("%s is a method, and a method must stay in the package of its type", d.Name.Name)
		}
	case *ast.GenDecl:
		switch s := d.Specs[0].(type) {
		case *ast.TypeSpec:
			if n, ok := m.obj.Type().(*types.Named); ok && n.NumMethods() > 0 {
				return refactor.Refusef("%s has methods, which must stay in its package; moving a type with methods is not supported yet", s.Name.Name)
			}
		case *ast.ValueSpec:
			if len(s.Names) > 1 {
				return refactor.Refusef("the declaration declares %d names; moving it to another package is not supported yet", len(s.Names))
			}
		}
	}
	if !token.IsIdentifier(m.newName) {
		return refactor.Refusef("new_name %q is not a valid Go identifier", m.newName)
	}
	if o := m.dest.Types.Scope().Lookup(m.newName); o != nil {
		return refactor.Refusef("package %s already declares %s (at %s)", m.dest.Name, m.newName, m.ws.Position(o.Pos()))
	}
	return nil
}

// packageIn returns the (non-test) package whose files are in dir.
func packageIn(pkgs []*packages.Package, dir string) *packages.Package {
	var found *packages.Package
	for _, p := range pkgs {
		if p.Types == nil || len(p.GoFiles) == 0 || filepath.Dir(p.GoFiles[0]) != dir || strings.HasSuffix(p.PkgPath, "_test") {
			continue
		}
		if p.ID == p.PkgPath {
			return p
		}
		if found == nil {
			found = p
		}
	}
	return found
}

// checkNoSourceRefs refuses code that uses other top-level declarations of
// its package, which the destination would have to import.
func (m *mover) checkNoSourceRefs() error {
	info := m.pkg.TypesInfo
	scope := m.pkg.Types.Scope()
	var err error
	ast.Inspect(m.decl, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || err != nil {
			return err == nil
		}
		if o := info.Uses[id]; o != nil && o.Parent() == scope && !m.is(o) {
			err = refactor.Refusef("%s refers to %s, declared in package %s at %s; move that first (or with it), since package %s cannot import %s back",
				m.obj.Name(), o.Name(), m.pkg.Name, m.ws.Position(o.Pos()), m.dest.Name, m.pkg.Name)
		}
		return true
	})
	return err
}

// updateReferences rewrites every reference outside the moved code. It
// returns the paths of the packages outside the destination that refer to
// the declaration, and how many references changed.
func (m *mover) updateReferences() (map[string]bool, int, error) {
	users := map[string]bool{}
	refs := 0
	seen := map[string]bool{}
	for _, p := range m.ws.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		inDest := p.PkgPath == m.dest.PkgPath
		for _, f := range p.Syntax {
			path := m.ws.Fset.File(f.Pos()).Name()
			if seen[path] || !m.ws.Contains(path) {
				continue
			}
			seen[path] = true
			sels := map[*ast.Ident]*ast.SelectorExpr{}
			ast.Inspect(f, func(n ast.Node) bool {
				if s, ok := n.(*ast.SelectorExpr); ok {
					sels[s.Sel] = s
				}
				return true
			})
			for id, o := range p.TypesInfo.Uses {
				if !m.is(o) || id.Pos() < f.FileStart || id.Pos() >= f.FileEnd {
					continue
				}
				if off := m.off(id.Pos()); path == m.path && m.start <= off && off < m.end {
					continue // inside the moved code
				}
				fe, err := m.fileFor(path)
				if err != nil {
					return nil, 0, err
				}
				refs++
				sel := sels[id]
				if sel != nil {
					if _, qualified := p.TypesInfo.Uses[ast.Unparen(sel.X).(*ast.Ident)].(*types.PkgName); !qualified {
						sel = nil
					}
				}
				var from token.Pos
				switch {
				case sel == nil && p.PkgPath != m.pkg.PkgPath:
					return nil, 0, refactor.Refusef("%s at %s comes from a dot import of package %s; moving it is not supported", id.Name, m.ws.Position(id.Pos()), m.pkg.Name)
				case sel == nil:
					from = id.Pos()
				default:
					from = sel.Pos()
					fe.cuts = append(fe.cuts, [2]int{m.off(sel.X.Pos()), m.off(sel.X.End())})
				}
				text := m.newName
				if !inDest {
					users[p.PkgPath] = true
					q, err := fe.adder.Name(m.dest.Types)
					if err != nil {
						return nil, 0, refactor.Refusef("%s refers to %s, but cannot import package %s, %v", m.ws.Rel(path), m.obj.Name(), m.dest.PkgPath, err)
					}
					fe.needs[m.dest.PkgPath] = true
					text = q + "." + m.newName
				}
				fe.edits = append(fe.edits, edit.TextEdit{Start: m.off(from), End: m.off(id.End()), NewText: text})
			}
		}
	}
	return users, refs, nil
}

// checkNoCycle refuses a move after which the destination would depend on
// a package that imports it: its own imports and the moved code's.
func (m *mover) checkNoCycle(users map[string]bool) error {
	deps := map[string]string{} // path -> the package that brings it in
	var walk func(p *packages.Package, via string)
	walk = func(p *packages.Package, via string) {
		for path, d := range p.Imports {
			if _, ok := deps[path]; !ok {
				deps[path] = via
				walk(d, via)
			}
		}
	}
	walk(m.dest, m.dest.PkgPath)
	for _, imp := range m.imports {
		path := pkgNameOf(m.pkg.TypesInfo, imp).Imported().Path()
		if d := m.pkg.Imports[path]; d != nil && path != m.dest.PkgPath {
			deps[path] = m.obj.Name()
			walk(d, m.obj.Name())
		}
	}
	for u := range users {
		if via, ok := deps[u]; ok {
			return refactor.Refusef("moving %s to package %s would make an import cycle: %s refers to it, so it would import %s, which already depends on %s (through %s)",
				m.obj.Name(), m.dest.Name, u, m.dest.PkgPath, u, via)
		}
	}
	return nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
