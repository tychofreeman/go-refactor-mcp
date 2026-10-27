package tsapi

import (
	"context"
	"path"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/ls"
	"github.com/buke/typescript-go-internal/v7/pkg/ls/lsutil"
	"github.com/buke/typescript-go-internal/v7/pkg/lsp/lsproto"
	"github.com/buke/typescript-go-internal/v7/pkg/tspath"
)

// Import is a name to import into a file: what the file at From
// (absolute) exports as Name; TypeOnly for a type.
type Import struct {
	Name, From string
	TypeOnly   bool
}

// AddImports returns the edits that import each of imports into file: into an import of the same module that the file has, or a new
// import declaration after its last one, in the file's quotes and
// semicolons.
func (p *Project) AddImports(file string, imports []Import) ([]Edit, string) {
	l, refusal := p.lined(file)
	if refusal != "" {
		return nil, refusal
	}
	var decls []*ast.Node
	for _, s := range l.f.Statements.Nodes {
		if ast.IsImportDeclaration(s) {
			decls = append(decls, s)
		}
	}
	quote, semi, ext := `"`, "", ""
	if l.semicolons() {
		semi = ";"
	}
	if len(decls) > 0 {
		quote = l.text[l.pos(decls[0].AsImportDeclaration().ModuleSpecifier):][:1]
	}
	specOf := func(d *ast.Node) string { return d.AsImportDeclaration().ModuleSpecifier.Text() }
	for _, d := range decls {
		if s := specOf(d); strings.HasPrefix(s, ".") && strings.HasSuffix(s, ".js") {
			ext = ".js"
		}
	}
	var order []string
	byModule := map[string][]Import{}
	for _, im := range imports {
		rel := relPath(path.Dir(tspath.NormalizePath(file)), tspath.NormalizePath(im.From))
		spec := strings.TrimSuffix(rel, path.Ext(rel)) + ext
		if !strings.HasPrefix(spec, ".") {
			spec = "./" + spec
		}
		if byModule[spec] == nil {
			order = append(order, spec)
		}
		byModule[spec] = append(byModule[spec], im)
	}
	var edits []Edit
	var fresh []string
	for _, spec := range order {
		ims := byModule[spec]
		names := func(inline bool) string {
			var out []string
			for _, im := range ims {
				if im.TypeOnly && inline {
					out = append(out, "type "+im.Name)
				} else {
					out = append(out, im.Name)
				}
			}
			return strings.Join(out, ", ")
		}
		added := false
		for _, d := range decls {
			clause := d.AsImportDeclaration().ImportClause
			if added || specOf(d) != spec || clause == nil || clause.IsTypeOnly() {
				continue
			}
			if nb := clause.AsImportClause().NamedBindings; nb != nil && ast.IsNamedImports(nb) {
				if els := nb.AsNamedImports().Elements.Nodes; len(els) > 0 {
					at := els[len(els)-1].End()
					edits = append(edits, Edit{File: file, Start: at, End: at, NewText: ", " + names(true)})
					added = true
				}
			}
		}
		if added {
			continue
		}
		all := true
		for _, im := range ims {
			all = all && im.TypeOnly
		}
		kw := "import"
		if all {
			kw = "import type"
		}
		fresh = append(fresh, kw+" { "+names(!all)+" } from "+quote+spec+quote+semi)
	}
	if len(fresh) > 0 {
		text := strings.Join(fresh, "\n")
		if len(decls) > 0 {
			at := l.end(l.line(decls[len(decls)-1].End() - 1))
			edits = append(edits, Edit{File: file, Start: at, End: at, NewText: "\n" + text})
		} else {
			edits = append(edits, Edit{File: file, Start: 0, End: 0, NewText: text + "\n\n"})
		}
	}
	return edits, ""
}

// semicolons reports whether the file ends statements with semicolons, as
// its first import does, or else its first statement not ending in a
// brace.
func (l *lined) semicolons() bool {
	for _, s := range l.f.Statements.Nodes {
		src := l.text[l.pos(s):s.End()]
		if ast.IsImportDeclaration(s) || !strings.HasSuffix(src, "}") {
			return strings.HasSuffix(src, ";")
		}
	}
	return true
}

// relPath is the path of target relative to dir, both absolute and
// slash-separated.
func relPath(dir, target string) string {
	d := strings.Split(strings.TrimSuffix(dir, "/"), "/")
	t := strings.Split(target, "/")
	i := 0
	for i < len(d) && i < len(t)-1 && d[i] == t[i] {
		i++
	}
	up := strings.Repeat("../", len(d)-i)
	return up + strings.Join(t[i:], "/")
}

// RemoveUnusedImports returns the edits that remove the imports of file
// that nothing uses, as the language service's organize
// imports does.
func (p *Project) RemoveUnusedImports(file string) []Edit {
	file = tspath.NormalizePath(file)
	prog := p.prog.GetProgram()
	f := prog.GetSourceFile(file)
	if f == nil {
		return nil
	}
	h := newHost(p.fs, prog)
	if !newLined(f).semicolons() {
		h.semicolons = lsutil.SemicolonPreferenceRemove
	}
	svc := ls.NewLanguageService(tspath.Path(p.Config), prog, h, file)
	var edits []Edit
	for name, tes := range svc.OrganizeImports(context.Background(), f, prog, lsproto.CodeActionKindSourceRemoveUnusedImports) {
		for _, te := range tes {
			edits = append(edits, Edit{File: name, Start: h.offset(name, te.Range.Start), End: h.offset(name, te.Range.End), NewText: te.NewText})
		}
	}
	return edits
}
