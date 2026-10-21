package inlinefunc

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
)

// deletable reports whether the function can be deleted after inlining
// this call, or why not.
func (x *inliner) deletable() (bool, string) {
	if x.keep {
		return false, ""
	}
	if x.inDecl(x.id.Pos()) {
		return false, "the call is inside it, so the inlined code still calls it"
	}
	key := x.posKey(x.fn.Pos())
	call := x.posKey(x.id.Pos())
	for _, p := range x.ws.Pkgs {
		if p.TypesInfo == nil || !x.fn.Exported() && p.PkgPath != x.pkg.PkgPath {
			continue
		}
		for id, o := range p.TypesInfo.Uses {
			f, ok := o.(*types.Func)
			if !ok || x.posKey(f.Pos()) != key {
				continue
			}
			if x.posKey(id.Pos()) == call {
				continue
			}
			if pos := x.ws.Fset.Position(id.Pos()); pos.Filename == x.dtf.Name() && x.doff(x.decl.Pos()) <= pos.Offset && pos.Offset < x.doff(x.decl.End()) {
				return false, "it calls itself"
			}
			return false, fmt.Sprintf("it is still used, for example at %s", x.ws.Position(id.Pos()))
		}
	}
	if x.sig.Recv() != nil {
		if _, at, found := analysis.InterfaceWithMethod(x.ws.TypesInfos(), x.fn.Pkg().Path(), x.fn.Name()); found {
			return false, fmt.Sprintf("an interface at %s has a method named %s, which this method may be needed to implement", x.ws.Position(at), x.fn.Name())
		}
	}
	return true, ""
}

func (x *inliner) posKey(p token.Pos) string {
	pos := x.ws.Fset.Position(p)
	return fmt.Sprintf("%s:%d", pos.Filename, pos.Offset)
}

// deletion removes the function (with its doc comment and a blank line next
// to it) and the imports of its file that nothing else uses.
func (x *inliner) deletion() []edit.TextEdit {
	src := x.dsrc
	start := x.decl.Pos()
	if x.decl.Doc != nil {
		start = x.decl.Doc.Pos()
	}
	s := x.doff(x.dtf.LineStart(x.dtf.Line(start)))
	e := x.doff(x.decl.End())
	for e < len(src) && src[e] != '\n' {
		e++
	}
	if e < len(src) {
		e++
	}
	switch {
	case s >= 2 && src[s-1] == '\n' && src[s-2] == '\n':
		s-- // the blank line before
	case e < len(src) && src[e] == '\n':
		e++ // the blank line after
	}
	return []edit.TextEdit{{Start: s, End: e}}
}

// span is a byte range of a file.
type span struct{ start, end int }

// importEdits deletes the imports that the inlining leaves unused: those
// used only by the deleted function's signature, by an absorbed variable
// declaration, or by a dropped argument.
func (x *inliner) importEdits(deletedFn bool) []edit.FileEdit {
	sameFile := x.dtf.Name() == x.tf.Name()
	var caller []span
	for _, e := range x.callerEdits {
		caller = append(caller, span{e.Start, e.End})
	}
	for _, p := range x.params {
		if p.drop && p.arg != nil {
			caller = append(caller, span{x.off(p.arg.Pos()), x.off(p.arg.End())})
		}
	}
	var callee []span
	if deletedFn {
		callee = append(callee,
			span{x.doff(x.decl.Pos()), x.doff(x.decl.Body.Lbrace)},
			span{x.doff(x.decl.Body.Rbrace), x.doff(x.decl.End())})
		if !sameFile {
			callee = []span{{x.doff(x.decl.Pos()), x.doff(x.decl.End())}}
		}
	}
	var out []edit.FileEdit
	if sameFile {
		if es := x.unusedImports(x.file, x.tf, x.src, append(caller, callee...)); len(es) > 0 {
			out = append(out, edit.FileEdit{Path: x.tf.Name(), Edits: es})
		}
		return out
	}
	if len(caller) > 0 {
		if es := x.unusedImports(x.file, x.tf, x.src, caller); len(es) > 0 {
			out = append(out, edit.FileEdit{Path: x.tf.Name(), Edits: es})
		}
	}
	if len(callee) > 0 {
		if es := x.unusedImports(x.dfile, x.dtf, x.dsrc, callee); len(es) > 0 {
			out = append(out, edit.FileEdit{Path: x.dtf.Name(), Edits: es})
		}
	}
	return out
}

// unusedImports deletes the imports of file that nothing outside the
// deleted spans uses, unless the inlined code in the caller's file uses
// them.
func (x *inliner) unusedImports(file *ast.File, tf *token.File, src []byte, deleted []span) []edit.TextEdit {
	if len(deleted) == 0 {
		return nil
	}
	gone := func(p token.Pos) bool {
		o := tf.Offset(p)
		for _, d := range deleted {
			if d.start <= o && o < d.end {
				return true
			}
		}
		return false
	}
	used, before := map[*types.PkgName]bool{}, map[*types.PkgName]bool{}
	for id, o := range x.info.Uses {
		pn, ok := o.(*types.PkgName)
		if !ok || x.ws.Fset.File(id.Pos()) != tf {
			continue
		}
		before[pn] = true
		if !gone(id.Pos()) {
			used[pn] = true
		}
	}
	var edits []edit.TextEdit
	for _, d := range file.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		var dead []*ast.ImportSpec
		for _, sp := range gd.Specs {
			is := sp.(*ast.ImportSpec)
			if is.Name != nil && (is.Name.Name == "_" || is.Name.Name == ".") {
				continue
			}
			var pn *types.PkgName
			if is.Name != nil {
				pn, _ = x.info.Defs[is.Name].(*types.PkgName)
			} else {
				pn, _ = x.info.Implicits[is].(*types.PkgName)
			}
			if pn == nil || !before[pn] || used[pn] || (tf == x.tf && x.usedQ[pn.Name()]) {
				continue
			}
			dead = append(dead, is)
		}
		switch {
		case len(dead) == 0:
		case len(dead) == len(gd.Specs):
			edits = append(edits, x.deleteLine(src, tf, gd))
		default:
			for _, is := range dead {
				edits = append(edits, x.deleteLine(src, tf, is))
			}
		}
	}
	return edits
}
