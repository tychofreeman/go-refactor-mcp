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
	if err := m.addMethods(); err != nil {
		return nil, err
	}
	if err := m.checkOtherPackage(to); err != nil {
		return nil, err
	}
	if err := m.checkMembers(); err != nil {
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
	for _, d := range m.decls {
		if token.IsExported(d.newName) || !d.usedOutside {
			continue
		}
		if d.newName != d.obj.Name() {
			return nil, refactor.Refusef("the new name %s must be exported: %s is used outside package %s", d.newName, d.obj.Name(), m.dest.Name)
		}
		how := "give new_name"
		if d != m.moved {
			how = "rename it first"
		}
		return nil, refactor.Refusef("%s is unexported and used outside package %s, so it must become exported; %s (e.g. %s)",
			d.obj.Name(), m.dest.Name, how, strings.ToUpper(d.newName[:1])+d.newName[1:])
	}
	if err := m.checkNoCycle(users); err != nil {
		return nil, err
	}
	m.deleteDecls()
	if _, err := os.Stat(to); os.IsNotExist(err) {
		text, err := m.newFile(func(pn *types.PkgName) string {
			if pn.Imported().Path() == m.dest.PkgPath {
				return ""
			}
			return pn.Name()
		})
		if err != nil {
			return nil, err
		}
		m.files[to] = &fileEdits{edits: []edit.TextEdit{{NewText: text}}}
	} else if err := m.appendTo(to, m.dest.PkgPath); err != nil {
		return nil, err
	}

	plan := &refactor.Plan{
		Summary: fmt.Sprintf("moved %s from %s to %s as %s.%s%s (%d reference%s updated)",
			m.obj.Name(), m.fromFiles(), m.ws.Rel(to), m.dest.Name, m.newName, m.withAlso(), refs, plural(refs)),
		Edits: m.fileEdits(),
	}
	if m.created {
		plan.Notes = append(plan.Notes, fmt.Sprintf("created package %s (%s)", m.dest.Name, m.dest.PkgPath))
	}
	for _, d := range m.decls {
		if d.obj.Exported() {
			plan.Notes = append(plan.Notes, fmt.Sprintf("%s was exported: references in other modules are not updated", d.obj.Name()))
		}
	}
	return plan, nil
}

// checkOtherPackage checks the destination package and file, and that the
// declaration can leave its package.
func (m *mover) checkOtherPackage(to string) error {
	if !strings.HasSuffix(to, ".go") {
		return refactor.Refusef("to_file must be a .go file")
	}
	for _, s := range m.sources {
		if strings.HasSuffix(s.path, "_test.go") || strings.HasSuffix(to, "_test.go") {
			return refactor.Refusef("moving to or from a test file in another package is not supported yet")
		}
		if err := s.checkBuiltEverywhere(m.ws.Rel(s.path)); err != nil {
			return err
		}
	}
	dir := filepath.Dir(to)
	m.dest = packageIn(m.ws.Pkgs, dir)
	if m.dest == nil {
		dest, err := m.newPackage(dir)
		if err != nil {
			return err
		}
		m.dest, m.created = dest, true
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
			for _, d := range m.decls {
				if imp.Name != nil && imp.Name.Name == d.newName {
					return refactor.Refusef("%s imports a package as %s, the declaration's name", m.ws.Rel(to), d.newName)
				}
			}
		}
	}

	for _, d := range m.decls {
		if err := m.checkCanLeave(d); err != nil {
			return err
		}
	}
	return nil
}

// checkCanLeave refuses a declaration that cannot move to the destination
// package.
func (m *mover) checkCanLeave(md *moved) error {
	switch d := md.decl.(type) {
	case *ast.FuncDecl:
		if d.Recv != nil && !m.is(recvTypeName(md.obj)) {
			return refactor.Refusef("%s is a method, and a method must stay in the package of its type; move the type, which takes its methods along", d.Name.Name)
		}
	case *ast.GenDecl:
		spec := d.Specs[0]
		if md.spec != nil {
			spec = md.spec
		}
		switch s := spec.(type) {
		case *ast.ValueSpec:
			if len(s.Names) > 1 {
				return refactor.Refusef("the declaration declares %d names; moving it to another package is not supported yet", len(s.Names))
			}
		}
	}
	if !token.IsIdentifier(md.newName) {
		return refactor.Refusef("new_name %q is not a valid Go identifier", md.newName)
	}
	if o := m.dest.Types.Scope().Lookup(md.newName); o != nil {
		return refactor.Refusef("package %s already declares %s (at %s)", m.dest.Name, md.newName, m.ws.Position(o.Pos()))
	}
	return nil
}

// newPackage describes the package a move into dir, which has none,
// creates: named after the directory, with the import path of the module
// it is in. Its scope is empty.
func (m *mover) newPackage(dir string) (*packages.Package, error) {
	rel := m.ws.Rel(dir)
	if !m.ws.Contains(dir) {
		return nil, refactor.Refusef("%s is outside the workspace", rel)
	}
	for _, elem := range strings.Split(filepath.ToSlash(rel), "/") {
		if elem == "testdata" || strings.HasPrefix(elem, "_") || strings.HasPrefix(elem, ".") && elem != "." && elem != ".." {
			return nil, refactor.Refusef("the go command ignores %s (a testdata directory, or a name starting with _ or .), so it cannot hold a package", rel)
		}
	}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
				return nil, refactor.Refusef("%s has Go files, but none in a package this build loads (build constraints?); moving into it is not supported", rel)
			}
		}
	}
	name := filepath.Base(dir)
	switch {
	case !token.IsIdentifier(name):
		return nil, refactor.Refusef("%s is not a valid package name, so %s cannot hold a new package; choose a directory named like an identifier", name, rel)
	case name == "main":
		return nil, refactor.Refusef("a package named main is a command, which cannot be imported; choose another directory")
	}
	mod := m.pkg.Module
	if mod == nil {
		return nil, refactor.Refusef("package %s is not in a module, so the new package's import path is unknown", m.pkg.Name)
	}
	r, err := filepath.Rel(mod.Dir, dir)
	if err != nil || strings.HasPrefix(r, "..") {
		return nil, refactor.Refusef("%s is outside module %s", rel, mod.Path)
	}
	path := mod.Path + "/" + filepath.ToSlash(r)
	return &packages.Package{ID: path, PkgPath: path, Name: name, Types: types.NewPackage(path, name), Imports: map[string]*packages.Package{}}, nil
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
	for _, d := range m.decls {
		ast.Inspect(d.node(), func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok || err != nil {
				return err == nil
			}
			if o := info.Uses[id]; o != nil && o.Parent() == scope && !m.is(o) {
				err = refactor.Refusef("%s refers to %s, declared in package %s at %s; move that first (or with it), since package %s cannot import %s back",
					d.obj.Name(), o.Name(), m.pkg.Name, m.ws.Position(o.Pos()), m.dest.Name, m.pkg.Name)
			}
			return true
		})
	}
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
				d := m.movedOf(o)
				if d == nil || recvTypeName(o) != nil || id.Pos() < f.FileStart || id.Pos() >= f.FileEnd {
					continue // a method is used through a value, whose type it follows
				}
				if m.inMoved(path, m.off(id.Pos())) {
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
				text := d.newName
				if !inDest {
					users[p.PkgPath] = true
					d.usedOutside = true
					q, err := fe.adder.Name(m.dest.Types)
					if err != nil {
						return nil, 0, refactor.Refusef("%s refers to %s, but cannot import package %s, %v", m.ws.Rel(path), d.obj.Name(), m.dest.PkgPath, err)
					}
					fe.needs[m.dest.PkgPath] = true
					text = q + "." + d.newName
				}
				fe.edits = append(fe.edits, edit.TextEdit{Start: m.off(from), End: m.off(id.End()), NewText: text})
			}
		}
	}
	return users, refs, nil
}

// inMoved reports whether an offset of the file at path is in the moved
// code.
func (m *mover) inMoved(path string, off int) bool {
	for _, d := range m.decls {
		if d.from.path == path && d.start <= off && off < d.end {
			return true
		}
	}
	return false
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
	for _, imp := range m.allImports() {
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

// recvTypeName returns the type name of a method's receiver, or nil for
// anything but a method.
func recvTypeName(o types.Object) types.Object {
	fn, ok := o.(*types.Func)
	if !ok || fn.Signature().Recv() == nil {
		return nil
	}
	t := fn.Signature().Recv().Type()
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := t.(*types.Named); ok {
		return n.Obj()
	}
	return nil
}

// addMethods adds the methods of each moved type: a method must stay in
// its type's package, so they move along. They must be in the same file.
func (m *mover) addMethods() error {
	for _, d := range append([]*moved(nil), m.decls...) {
		tn, ok := d.obj.(*types.TypeName)
		if !ok {
			continue
		}
		n, ok := tn.Type().(*types.Named)
		if !ok {
			continue
		}
		for i := 0; i < n.NumMethods(); i++ {
			fn := n.Method(i)
			pos := m.ws.Fset.Position(fn.Pos())
			from, err := m.sourceFor(pos.Filename)
			if err != nil {
				return err
			}
			for _, fd := range from.file.Decls {
				if declares(m.ws.Fset, fd, pos.Offset) && !m.moves(fd, nil) {
					md := &moved{from: from, decl: fd, obj: fn, newName: fn.Name()}
					if err := m.checkDecl(md); err != nil {
						return err
					}
					m.decls = append(m.decls, md)
				}
			}
		}
	}
	m.sortDecls()
	return nil
}

// checkMembers refuses a move after which code staying outside the
// destination package would use an unexported field or method of a moved
// type (by a selector, a key or an unkeyed composite literal), or after
// which a type would stop implementing an interface: an unexported method
// belongs to its package, so an interface naming it no longer matches.
func (m *mover) checkMembers() error {
	type member struct{ what, of string }
	unexported := map[string]member{} // by keyOf
	var named []*types.Named
	for _, d := range m.decls {
		tn, ok := d.obj.(*types.TypeName)
		if !ok {
			continue
		}
		n, ok := tn.Type().(*types.Named)
		if !ok {
			continue
		}
		named = append(named, n)
		if st, ok := n.Underlying().(*types.Struct); ok {
			for i := 0; i < st.NumFields(); i++ {
				if f := st.Field(i); !f.Exported() {
					unexported[keyOf(m.ws.Fset, f)] = member{"field", tn.Name()}
				}
			}
		}
		for i := 0; i < n.NumMethods(); i++ {
			if fn := n.Method(i); !fn.Exported() {
				unexported[keyOf(m.ws.Fset, fn)] = member{"method", tn.Name()}
				if err := m.checkNoInterface(fn); err != nil {
					return err
				}
			}
		}
	}
	if len(unexported) == 0 {
		return nil
	}
	inMoved := func(p token.Pos) bool {
		return m.inMoved(m.ws.Fset.Position(p).Filename, m.off(p))
	}
	refuse := func(name string, mb member, at token.Pos) error {
		return refactor.Refusef("%s, a %s of %s, is unexported, and %s uses it outside package %s; export it first (rename), or move that code too",
			name, mb.what, mb.of, m.ws.Position(at), m.dest.Name)
	}
	for _, p := range m.ws.Pkgs {
		if p.TypesInfo == nil || p.PkgPath == m.dest.PkgPath {
			continue
		}
		for id, o := range p.TypesInfo.Uses {
			if mb, ok := unexported[keyOf(m.ws.Fset, o)]; ok && !inMoved(id.Pos()) {
				return refuse(o.Name(), mb, id.Pos())
			}
		}
		for _, f := range p.Syntax {
			var err error
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok || err != nil || len(lit.Elts) == 0 {
					return err == nil
				}
				if _, keyed := lit.Elts[0].(*ast.KeyValueExpr); keyed || inMoved(lit.Pos()) || !m.isMovedType(p.TypesInfo.TypeOf(lit), named) {
					return true
				}
				st, ok := p.TypesInfo.TypeOf(lit).Underlying().(*types.Struct)
				for i := 0; ok && i < st.NumFields() && i < len(lit.Elts); i++ {
					if f := st.Field(i); !f.Exported() {
						err = refuse(f.Name(), member{"field", p.TypesInfo.TypeOf(lit).(*types.Named).Obj().Name()}, lit.Elts[i].Pos())
						return false
					}
				}
				return true
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// isMovedType reports whether t is one of the moved named types, seen
// through any package variant.
func (m *mover) isMovedType(t types.Type, moved []*types.Named) bool {
	n, ok := t.(*types.Named)
	if !ok {
		return false
	}
	for _, mt := range moved {
		if keyOf(m.ws.Fset, n.Obj()) == keyOf(m.ws.Fset, mt.Obj()) {
			return true
		}
	}
	return false
}

// checkNoInterface refuses if an interface in the workspace has the
// unexported method fn's name in fn's package: after the move fn belongs
// to the destination, so its type no longer implements that interface, and
// a type assertion would quietly fail.
func (m *mover) checkNoInterface(fn *types.Func) error {
	has := func(it *types.Interface) bool {
		for i := 0; i < it.NumMethods(); i++ {
			if im := it.Method(i); im.Name() == fn.Name() && im.Pkg() != nil && im.Pkg().Path() == fn.Pkg().Path() {
				return true
			}
		}
		return false
	}
	for _, p := range m.ws.Pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for id, o := range p.TypesInfo.Defs {
			if tn, ok := o.(*types.TypeName); ok {
				if it, ok := tn.Type().Underlying().(*types.Interface); ok && has(it) {
					return refactor.Refusef("the interface %s (at %s) has the unexported method %s, which belongs to package %s; once moved, the type's %s would not implement it, and a type assertion would quietly fail",
						tn.Name(), m.ws.Position(id.Pos()), fn.Name(), m.pkg.Name, fn.Name())
				}
			}
		}
		for e, tv := range p.TypesInfo.Types {
			if it, ok := tv.Type.(*types.Interface); ok && tv.IsType() && has(it) {
				return refactor.Refusef("the interface at %s has the unexported method %s, which belongs to package %s; once moved, the type's %s would not implement it",
					m.ws.Position(e.Pos()), fn.Name(), m.pkg.Name, fn.Name())
			}
		}
	}
	return nil
}
