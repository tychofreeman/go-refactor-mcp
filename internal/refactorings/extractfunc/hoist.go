package extractfunc

// Moving local constants and types to package level.
//
// Statements that use a constant or type declared elsewhere in the
// enclosing function cannot take it with them: a copied type would be a
// different type (a type switch on a value from the other function would
// stop matching), and a constant passed as a parameter would lose being a
// constant (untyped arithmetic, array lengths). So the declaration moves
// to package level, just above the enclosing declaration, where both
// functions see the same one. The same applies to a constant or type the
// statements declare and code after them uses.
//
// That is only behavior-preserving when the declaration can be written at
// package level (it uses no local variables or type parameters; local
// constants and types it uses move with it), stands alone (not part of a
// grouped declaration, whose iota and implicit repetition depend on the
// group), its name is free at package level in every file, and every use
// of it still refers to it once the local declaration is gone.

import (
	"fmt"
	"go/ast"
	"go/types"
	"sort"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
)

// hoist is one local declaration moved to package level.
type hoist struct {
	obj  types.Object
	decl *ast.DeclStmt
}

// planHoists works out which declarations move for the given objects (and
// the local constants and types they use), or refuses.
func (x *extraction) planHoists(objs []types.Object) error {
	fn := x.outermostFunc()
	queued := map[types.Object]bool{}
	queue := append([]types.Object(nil), objs...)
	for _, o := range objs {
		queued[o] = true
	}
	names := map[string]types.Object{}
	for len(queue) > 0 {
		o := queue[0]
		queue = queue[1:]
		decl, spec := x.declOf(fn, o)
		if decl == nil {
			return refactor.Refusef("cannot find the declaration of %s %s in %s", kindOf(o), o.Name(), x.enclosingName())
		}
		what := fmt.Sprintf("%s %s (line %d)", kindOf(o), o.Name(), x.line(o.Pos()))
		gd := decl.Decl.(*ast.GenDecl)
		if len(gd.Specs) != 1 {
			return refactor.Refusef("the selected lines need %s, declared in %s, moved to package level so both functions can use it, but it is part of a grouped declaration; move it out of the group first", what, x.enclosingName())
		}
		if vs, ok := spec.(*ast.ValueSpec); ok && len(vs.Names) != 1 {
			return refactor.Refusef("the selected lines need %s moved to package level, but it is declared together with other constants; declare it on its own first", what)
		}
		if other := names[o.Name()]; other != nil && other != o {
			return refactor.Refusef("the selected lines need two declarations named %s moved to package level; rename one first", o.Name())
		}
		names[o.Name()] = o
		if err := x.checkHoistName(o, what); err != nil {
			return err
		}
		// What the declaration uses must exist at package level too.
		var err error
		ast.Inspect(spec, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok || err != nil {
				return err == nil
			}
			u := x.info.Uses[id]
			if u == nil || u == o || !isLocal(u) {
				return true
			}
			switch u := u.(type) {
			case *types.Const:
				if !queued[u] {
					queued[u] = true
					queue = append(queue, u)
				}
			case *types.TypeName:
				if _, tp := u.Type().(*types.TypeParam); !tp {
					if !queued[u] {
						queued[u] = true
						queue = append(queue, u)
					}
					return true
				}
				err = refactor.Refusef("the selected lines need %s moved to package level, but it uses the type parameter %s", what, u.Name())
			default:
				err = refactor.Refusef("the selected lines need %s moved to package level, but it uses the %s %s, which only exists in %s", what, kindOf(u), u.Name(), x.enclosingName())
			}
			return err == nil
		})
		if err != nil {
			return err
		}
		x.hoists = append(x.hoists, hoist{o, decl})
		if tn, ok := o.(*types.TypeName); ok {
			x.nameable[tn] = true
		}
	}
	sort.Slice(x.hoists, func(i, j int) bool { return x.hoists[i].decl.Pos() < x.hoists[j].decl.Pos() })
	return nil
}

// declOf finds the declaration statement and spec in fn that declare o.
func (x *extraction) declOf(fn ast.Node, o types.Object) (*ast.DeclStmt, ast.Spec) {
	var decl *ast.DeclStmt
	var spec ast.Spec
	ast.Inspect(fn, func(n ast.Node) bool {
		if decl != nil {
			return false
		}
		ds, ok := n.(*ast.DeclStmt)
		if !ok {
			return true
		}
		for _, sp := range ds.Decl.(*ast.GenDecl).Specs {
			switch sp := sp.(type) {
			case *ast.ValueSpec:
				for _, name := range sp.Names {
					if x.info.Defs[name] == o {
						decl, spec = ds, sp
					}
				}
			case *ast.TypeSpec:
				if x.info.Defs[sp.Name] == o {
					decl, spec = ds, sp
				}
			}
		}
		return decl == nil
	})
	return decl, spec
}

// checkHoistName refuses moving o to package level when its name is taken
// there (in any file or variant of the package), or when some use of o
// would then refer to something else.
func (x *extraction) checkHoistName(o types.Object, what string) error {
	name := o.Name()
	if u := types.Universe.Lookup(name); u != nil {
		return refactor.Refusef("the selected lines need %s moved to package level, but there it would hide the predeclared %s in the whole package; rename it first", what, name)
	}
	for _, p := range x.ws.Pkgs {
		if p.PkgPath != x.pkg.PkgPath || p.Types == nil {
			continue
		}
		if other := p.Types.Scope().Lookup(name); other != nil {
			return refactor.Refusef("the selected lines need %s moved to package level, but the package already declares %s (%s at %s); rename it first",
				what, name, kindOf(other), x.ws.Position(other.Pos()))
		}
		for _, f := range p.Syntax {
			if s := p.TypesInfo.Scopes[f]; s != nil && s.Lookup(name) != nil {
				return refactor.Refusef("the selected lines need %s moved to package level, but %s is imported as %s; rename it first",
					what, x.ws.Rel(x.ws.Fset.File(f.Pos()).Name()), name)
			}
		}
	}
	// Each use must still find o once its local declaration is gone: no
	// other declaration of the name between the use and package level.
	for id, u := range x.info.Uses {
		if u != o {
			continue
		}
		for s := x.innermostScope(id.Pos()); s != nil && s != x.pkg.Types.Scope(); s = s.Parent() {
			if s == o.Parent() {
				continue
			}
			if other := s.Lookup(name); other != nil && other.Pos() < id.Pos() {
				return refactor.Refusef("the selected lines need %s moved to package level, but its use at line %d is also inside the scope of the %s %s declared at line %d, which would hide it; rename one first",
					what, x.line(id.Pos()), kindOf(other), name, x.line(other.Pos()))
			}
		}
	}
	return nil
}

// hoistEdits returns the file edits that move the declarations, and the
// edits inside the selection (for declarations the selected statements
// make) that remove them from the new function's body.
func (x *extraction) hoistEdits(decl ast.Decl) (file, body []edit.TextEdit) {
	if len(x.hoists) == 0 {
		return nil, nil
	}
	var texts []string
	for _, h := range x.hoists {
		start, end := x.off(h.decl.Pos()), x.off(h.decl.End())
		texts = append(texts, reindent(string(x.src[start:end]), x.indentOf(h.decl.Pos()), ""))

		// Remove the statement, with its line if nothing else is on it.
		ls := x.off(x.tf.LineStart(x.line(h.decl.Pos())))
		le := len(x.src)
		if l := x.line(h.decl.End()); l < x.tf.LineCount() {
			le = x.off(x.tf.LineStart(l+1)) - 1
		}
		del := edit.TextEdit{Start: start, End: end}
		if strings.TrimSpace(string(x.src[ls:start])) == "" && strings.TrimSpace(string(x.src[end:le])) == "" {
			del = edit.TextEdit{Start: ls, End: le + 1} // the whole line(s) and the newline
		}
		if x.inSelectionPos(h.decl.Pos()) {
			// Inside the moved text, which ends without its last newline.
			if del.End > x.end {
				del.End = x.end
				if del.Start > x.start {
					del.Start-- // the newline before it instead
				}
			}
			body = append(body, del)
		} else {
			file = append(file, del)
		}
	}
	// Above the enclosing declaration (and its doc comment).
	at := decl.Pos()
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Doc != nil {
			at = d.Doc.Pos()
		}
	case *ast.GenDecl:
		if d.Doc != nil {
			at = d.Doc.Pos()
		}
	}
	file = append(file, edit.TextEdit{Start: x.off(at), End: x.off(at), NewText: strings.Join(texts, "\n\n") + "\n\n"})
	return file, body
}

// hoistNote describes the moved declarations.
func (x *extraction) hoistNote() string {
	var parts []string
	for _, h := range x.hoists {
		parts = append(parts, fmt.Sprintf("%s %s", kindOf(h.obj), h.obj.Name()))
	}
	return fmt.Sprintf("moved %s from %s to package level, so both functions use the same declaration",
		strings.Join(parts, ", "), x.enclosingName())
}
