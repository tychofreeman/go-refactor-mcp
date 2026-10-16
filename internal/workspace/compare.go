package workspace

import (
	"fmt"
	"go/ast"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Compare reports how two workspaces of the same module differ in what
// refactorings can observe: packages (including test variants of
// dependencies, see sourcePackages), files, errors, and type information
// (definitions, uses, implicit objects, expression types and constant
// values, selections, instantiations, scopes). Everything is described by
// source positions and type strings, so a workspace that was updated
// incrementally can be compared with a fresh load. It returns at most max
// differences; none means equivalent.
func Compare(a, b *Workspace, max int) []string {
	da, db := a.describe(), b.describe()
	var out []string
	add := func(format string, args ...any) bool {
		out = append(out, fmt.Sprintf(format, args...))
		return len(out) < max
	}
	ids := map[string]bool{}
	for id := range da {
		ids[id] = true
	}
	for id := range db {
		ids[id] = true
	}
	var sorted []string
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)
	for _, id := range sorted {
		la, oka := da[id]
		lb, okb := db[id]
		if !oka || !okb {
			if !add("package %s only in %s", id, map[bool]string{true: "first", false: "second"}[oka]) {
				return out
			}
			continue
		}
		onlyA, onlyB := diffSorted(la, lb)
		for _, l := range onlyA {
			if !add("%s: only in first:  %s", id, l) {
				return out
			}
		}
		for _, l := range onlyB {
			if !add("%s: only in second: %s", id, l) {
				return out
			}
		}
	}
	return out
}

// describe returns, per package ID, a sorted list of facts.
func (w *Workspace) describe() map[string][]string {
	out := map[string][]string{}
	pos := func(n ast.Node) string { return w.Position(n.Pos()) }
	obj := func(o types.Object) string {
		if o == nil {
			return "<nil>"
		}
		at := "-"
		if o.Pos().IsValid() {
			at = w.Position(o.Pos())
		}
		return fmt.Sprintf("%T %s @%s", o, types.ObjectString(o, nil), at)
	}
	for _, p := range w.sourcePackages() {
		var facts []string
		f := func(format string, args ...any) { facts = append(facts, fmt.Sprintf(format, args...)) }
		f("name %s path %s", p.Name, p.PkgPath)
		for _, file := range p.CompiledGoFiles {
			f("file %s", w.Rel(file))
		}
		// Errors as Errors() reports them: a failed `go list -export`
		// repeats compile errors as a ListError, which Errors() drops when
		// the precise parse or type errors are present (only a full load
		// produces that duplicate).
		precise := false
		for _, e := range p.Errors {
			precise = precise || e.Kind == packages.ParseError || e.Kind == packages.TypeError
		}
		for _, e := range p.Errors {
			if precise && e.Kind == packages.ListError {
				continue
			}
			f("error %v %s: %s", e.Kind, relPos(w, e.Pos), e.Msg)
		}
		if p.Types != nil {
			f("types %s complete=%v", p.Types.Path(), p.Types.Complete())
			for _, name := range p.Types.Scope().Names() {
				f("pkgscope %s", obj(p.Types.Scope().Lookup(name)))
			}
		}
		info := p.TypesInfo
		if info == nil {
			out[p.ID] = facts
			continue
		}
		for id, o := range info.Defs {
			f("def %s %s -> %s", pos(id), id.Name, obj(o))
		}
		for id, o := range info.Uses {
			f("use %s %s -> %s", pos(id), id.Name, obj(o))
		}
		for n, o := range info.Implicits {
			f("implicit %s %T -> %s", pos(n), n, obj(o))
		}
		for e, tv := range info.Types {
			val := ""
			if tv.Value != nil {
				val = " = " + tv.Value.ExactString()
			}
			f("type %s-%s %T %s mode=%s%s", pos(e), w.endPosition(e), e, typeString(tv.Type), modeOf(tv), val)
		}
		for s, sel := range info.Selections {
			f("selection %s %s", pos(s), sel.String())
		}
		for id, inst := range info.Instances {
			var targs []string
			for i := 0; i < inst.TypeArgs.Len(); i++ {
				targs = append(targs, typeString(inst.TypeArgs.At(i)))
			}
			f("instance %s %s [%s] %s", pos(id), id.Name, strings.Join(targs, ", "), typeString(inst.Type))
		}
		for n, s := range info.Scopes {
			f("scope %s %T %s", pos(n), n, strings.Join(s.Names(), ","))
		}
		for file, v := range info.FileVersions {
			f("fileversion %s %s", w.Rel(w.Fset.File(file.Pos()).Name()), v)
		}
		sort.Strings(facts)
		out[p.ID] = facts
	}
	return out
}

// endPosition describes where n ends. In syntax with errors, a node can
// end past the end of its file, where the position would otherwise
// resolve into whichever file happens to follow in the FileSet.
func (w *Workspace) endPosition(n ast.Node) string {
	f := w.Fset.File(n.Pos())
	if f == nil || int(n.End()) > f.Base()+f.Size() {
		return "<eof>"
	}
	return w.Position(n.End())
}

func typeString(t types.Type) string {
	if t == nil {
		return "<nil>"
	}
	return types.TypeString(t, nil)
}

func modeOf(tv types.TypeAndValue) string {
	switch {
	case tv.IsVoid():
		return "void"
	case tv.IsType():
		return "type"
	case tv.IsBuiltin():
		return "builtin"
	case tv.IsNil():
		return "nil"
	case tv.Value != nil:
		return "constant"
	case tv.Addressable():
		return "variable"
	case tv.Assignable():
		return "mapindex"
	case tv.HasOk():
		return "commaok"
	case tv.IsValue():
		return "value"
	}
	return "invalid"
}

func relPos(w *Workspace, pos string) string {
	if after, ok := strings.CutPrefix(pos, w.Root+"/"); ok {
		return after
	}
	return pos
}

// diffSorted returns the elements only in a and only in b, for sorted
// slices that may contain duplicates.
func diffSorted(a, b []string) (onlyA, onlyB []string) {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			i++
			j++
		case a[i] < b[j]:
			onlyA = append(onlyA, a[i])
			i++
		default:
			onlyB = append(onlyB, b[j])
			j++
		}
	}
	return append(onlyA, a[i:]...), append(onlyB, b[j:]...)
}
