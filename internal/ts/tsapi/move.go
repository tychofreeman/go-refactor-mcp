package tsapi

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/checker"
	"github.com/buke/typescript-go-internal/v7/pkg/ls"
	"github.com/buke/typescript-go-internal/v7/pkg/ls/lsconv"
	"github.com/buke/typescript-go-internal/v7/pkg/tspath"
)

// FileRename returns the edits the language service makes when the file
// or directory at oldPath moves to newPath: the module specifiers that
// refer to it, its own relative imports, and the config's file lists.
// Edits to a moved file are at its old path.
func (p *Project) FileRename(ctx context.Context, oldPath, newPath string) []Edit {
	oldPath, newPath = tspath.NormalizePath(oldPath), tspath.NormalizePath(newPath)
	prog := p.prog.GetProgram()
	h := newHost(p.fs, prog)
	svc := ls.NewLanguageService(tspath.Path(p.Config), prog, h, p.Config)
	var edits []Edit
	for _, ch := range svc.GetEditsForFileRename(ctx, lsconv.FileNameToDocumentURI(oldPath), lsconv.FileNameToDocumentURI(newPath)) {
		if ch.TextDocumentEdit == nil {
			continue
		}
		file := ch.TextDocumentEdit.TextDocument.Uri.FileName()
		for _, e := range ch.TextDocumentEdit.Edits {
			if te := e.TextEdit; te != nil {
				edits = append(edits, Edit{File: file, Start: h.offset(file, te.Range.Start), End: h.offset(file, te.Range.End), NewText: te.NewText})
			}
		}
	}
	return edits
}

// moduleSpecifiers calls fn with each module specifier of the file: of
// imports, exports, import types, import() and require().
func moduleSpecifiers(f *ast.SourceFile, fn func(spec, owner *ast.Node)) {
	var visit func(n *ast.Node) bool
	visit = func(n *ast.Node) bool {
		switch {
		case ast.IsImportDeclaration(n) || ast.IsJSImportDeclaration(n):
			fn(n.AsImportDeclaration().ModuleSpecifier, n)
		case ast.IsExportDeclaration(n) && n.AsExportDeclaration().ModuleSpecifier != nil:
			fn(n.AsExportDeclaration().ModuleSpecifier, n)
		case ast.IsImportEqualsDeclaration(n) && ast.IsExternalModuleReference(n.AsImportEqualsDeclaration().ModuleReference):
			fn(n.AsImportEqualsDeclaration().ModuleReference.Expression(), n)
		case ast.IsCallExpression(n) && len(n.Arguments()) == 1 && ast.IsStringLiteralLike(n.Arguments()[0]) &&
			(n.Expression().Kind == ast.KindImportKeyword || ast.IsIdentifier(n.Expression()) && n.Expression().Text() == "require"):
			fn(n.Arguments()[0], n)
		case ast.IsImportTypeNode(n):
			if arg := n.AsImportTypeNode().Argument; arg != nil && ast.IsLiteralTypeNode(arg) {
				fn(arg.AsLiteralTypeNode().Literal, n)
			}
		}
		n.ForEachChild(visit)
		return false
	}
	visit(f.AsNode())
}

// fileOf is the file a module specifier refers to, or "".
func fileOf(c *checker.Checker, spec *ast.Node) string {
	if s := c.GetSymbolAtLocation(spec); s != nil && s.ValueDeclaration != nil && ast.IsSourceFile(s.ValueDeclaration) {
		return s.ValueDeclaration.AsSourceFile().FileName()
	}
	return ""
}

// Importers returns where files outside dir (a file, or a directory with
// what is under it) refer to a file in it, as the file's path and line:
// "/repo/src/a.ts:3".
func (p *Project) Importers(ctx context.Context, dir string) []string {
	dir = tspath.NormalizePath(dir)
	inside := func(f string) bool { return f == dir || strings.HasPrefix(f, dir+"/") }
	prog := p.prog.GetProgram()
	c, done := prog.GetTypeChecker(ctx)
	defer done()
	var out []string
	for _, f := range prog.GetSourceFiles() {
		if f.IsDeclarationFile || inside(f.FileName()) {
			continue
		}
		l := newLined(f)
		moduleSpecifiers(f, func(spec, _ *ast.Node) {
			if t := fileOf(c, spec); t != "" && inside(t) {
				out = append(out, fmt.Sprintf("%s:%d", f.FileName(), l.line(l.pos(spec))))
			}
		})
	}
	return out
}

// topLevel returns the top-level statements of the file that declare
// name: a function with its overloads, a class, interface, type alias,
// enum, or a variable statement.
func (l *lined) topLevel(name string) []*ast.Node {
	var out []*ast.Node
	for _, s := range l.f.Statements.Nodes {
		switch {
		case ast.IsInterfaceDeclaration(s), ast.IsTypeAliasDeclaration(s), ast.IsFunctionDeclaration(s), ast.IsClassDeclaration(s), ast.IsEnumDeclaration(s), ast.IsModuleDeclaration(s):
			if s.Name() != nil && s.Name().Text() == name {
				out = append(out, s)
			}
		case ast.IsVariableStatement(s):
			if slices.ContainsFunc(declared(s), func(n *ast.Node) bool { return n.Text() == name }) {
				out = append(out, s)
			}
		}
	}
	return out
}

// topNames returns the names the top-level statement s declares.
func topNames(s *ast.Node) []*ast.Node {
	switch {
	case ast.IsInterfaceDeclaration(s), ast.IsTypeAliasDeclaration(s), ast.IsModuleDeclaration(s):
		if s.Name() != nil {
			return []*ast.Node{s.Name()}
		}
		return nil
	}
	return declared(s)
}

// importLine is an import of what the file at from exports as names,
// written for a file in dir: default, namespace or named.
type importLine struct {
	spec     string // the module specifier
	named    []string
	def, ns  string
	typeOnly bool
}

func (im importLine) text(quote, semi string) string {
	var parts []string
	if im.def != "" {
		parts = append(parts, im.def)
	}
	if im.ns != "" {
		parts = append(parts, "* as "+im.ns)
	}
	if len(im.named) > 0 {
		parts = append(parts, "{ "+strings.Join(im.named, ", ")+" }")
	}
	kw := "import "
	if im.typeOnly {
		kw = "import type "
	}
	return kw + strings.Join(parts, ", ") + " from " + quote + im.spec + quote + semi
}

// respec returns the module specifier spec, written in the file at from,
// as written in the file at to: a relative one rewritten, a package's
// kept.
func respec(spec, from, to string) string {
	if !strings.HasPrefix(spec, ".") {
		return spec
	}
	abs := path.Join(path.Dir(from), spec)
	rel := relPath(path.Dir(to), abs)
	if !strings.HasPrefix(rel, ".") {
		rel = "./" + rel
	}
	return rel
}

// specFor returns the module specifier of the file at target for a file
// at from, in the style of the file's own relative imports (with .js,
// with .ts or without an extension).
func (l *lined) specFor(target string) string {
	ext := ""
	moduleSpecifiers(l.f, func(spec, _ *ast.Node) {
		if s := spec.Text(); strings.HasPrefix(s, ".") && (strings.HasSuffix(s, ".js") || strings.HasSuffix(s, ".ts")) {
			ext = path.Ext(s)
		}
	})
	rel := relPath(path.Dir(l.f.FileName()), target)
	spec := strings.TrimSuffix(rel, path.Ext(rel)) + ext
	if !strings.HasPrefix(spec, ".") {
		spec = "./" + spec
	}
	return spec
}

// MoveDeclaration returns the edits that move the top-level declaration
// name (named on line at column of file when line is given, else declared
// at the top of file), with the declarations also names, to the file to,
// which is created when it does not exist: imports follow the code, and
// files that imported it from its old file import it from the new one.
// withDeps also moves the declarations of the old file it uses, and what
// they use. created says whether to is new.
func (p *Project) MoveDeclaration(ctx context.Context, file string, line int, name string, column int, to string, also []string, withDeps bool) (edits []Edit, created bool, refusal string) {
	l, why := p.lined(file)
	if why != "" {
		return nil, false, why
	}
	to = tspath.NormalizePath(to)
	prog := p.prog.GetProgram()
	c, done := prog.GetTypeChecker(ctx)
	defer done()
	if line > 0 {
		if line > l.count() {
			return nil, false, fmt.Sprintf("line %d out of range: the file has %d lines", line, l.count())
		}
		var decl *ast.Node
		identifiers(l.f.AsNode(), func(id *ast.Node) {
			start := l.pos(id)
			if decl == nil && id.Text() == name && l.line(start) == line && (column == 0 || start-l.start(line)+1 == column) {
				if s := symbolOf(c, id); s != nil && len(s.Declarations) > 0 {
					decl = s.Declarations[0]
				}
			}
		})
		if decl == nil {
			return nil, false, fmt.Sprintf("no name %s on line %d: %s", name, line, strings.TrimSpace(l.text[l.start(line):l.end(line)]))
		}
		l = newLined(ast.GetSourceFileOfNode(decl))
		if l.f.IsDeclarationFile {
			return nil, false, fmt.Sprintf("%s is declared in %s, not in the solution's code", name, l.f.FileName())
		}
	}
	from := l.f.FileName()
	if from == to {
		return nil, false, fmt.Sprintf("%s is already in %s", name, path.Base(to))
	}
	// The statements that move.
	var moving []*ast.Node
	names := map[string]bool{}
	add := func(n string) string {
		stmts := l.topLevel(n)
		if len(stmts) == 0 {
			return fmt.Sprintf("%s declares no %s at its top level", path.Base(from), n)
		}
		for _, s := range stmts {
			if slices.Contains(moving, s) {
				continue
			}
			switch {
			case ast.IsModuleDeclaration(s):
				return fmt.Sprintf("%s is a namespace; move its contents instead", n)
			case ast.HasSyntacticModifier(s, ast.ModifierFlagsDefault):
				return fmt.Sprintf("%s is the file's default export", n)
			case ast.IsVariableStatement(s) && len(declared(s)) > 1:
				return fmt.Sprintf("%s is declared with %s in one statement (%s); split it first", n, joinNames(declared(s)), l.where(s))
			case ast.IsInterfaceDeclaration(s) && len(l.topLevel(n)) > 1 && !ast.IsInterfaceDeclaration(l.topLevel(n)[0]):
				return fmt.Sprintf("%s is declared more than once (%s)", n, l.where(s))
			}
			moving = append(moving, s)
		}
		names[n] = true
		return ""
	}
	if why := add(name); why != "" {
		return nil, false, why
	}
	for _, a := range also {
		if why := add(a); why != "" {
			return nil, false, why
		}
	}
	in := func(n *ast.Node) bool {
		return ast.GetSourceFileOfNode(n) == l.f && slices.ContainsFunc(moving, func(s *ast.Node) bool { return n.Pos() >= s.Pos() && n.End() <= s.End() })
	}
	// What the moving code uses from its file: declarations that stay,
	// and imports.
	type use struct {
		decl *ast.Node // the top-level statement, or the import's specifier
		id   *ast.Node
	}
	for {
		var stay []use
		for _, s := range moving {
			identifiers(s, func(id *ast.Node) {
				if ast.IsDeclarationName(id) {
					return
				}
				sym := c.GetSymbolAtLocation(id)
				if id.Parent != nil && ast.IsShorthandPropertyAssignment(id.Parent) {
					sym = c.GetShorthandAssignmentValueSymbol(id.Parent)
				}
				if sym == nil || len(sym.Declarations) == 0 {
					return
				}
				d := sym.Declarations[0]
				if ast.GetSourceFileOfNode(d) != l.f || in(d) {
					return
				}
				for x := d; x != nil; x = x.Parent {
					if x.Parent == l.f.AsNode() {
						if !ast.IsImportDeclaration(x) && !slices.ContainsFunc(stay, func(u use) bool { return u.decl == x }) {
							stay = append(stay, use{x, id})
						}
						break
					}
				}
			})
		}
		if len(stay) == 0 || !withDeps {
			for _, u := range stay {
				if !ast.HasSyntacticModifier(u.decl, ast.ModifierFlagsExport) {
					return nil, false, fmt.Sprintf("%s uses %s (%s), which stays in %s and is not exported; move it too (also, or with_dependencies), or export it first", l.part(moving[0]), u.id.Text(), l.where(u.id), path.Base(from))
				}
			}
			break
		}
		for _, u := range stay {
			for _, n := range topNames(u.decl) {
				if why := add(n.Text()); why != "" {
					return nil, false, why
				}
			}
		}
	}
	slices.SortFunc(moving, func(a, b *ast.Node) int { return a.Pos() - b.Pos() })
	var movedSyms []*ast.Symbol
	for _, s := range moving {
		for _, n := range topNames(s) {
			if sym := c.GetSymbolAtLocation(n); sym != nil && !slices.Contains(movedSyms, sym) {
				movedSyms = append(movedSyms, sym)
			}
		}
	}

	// The destination: its text, and the names it declares already.
	dest := prog.GetSourceFile(to)
	var dl *lined
	if dest != nil {
		dl = newLined(dest)
		for n := range names {
			if len(dl.topLevel(n)) > 0 {
				return nil, false, fmt.Sprintf("%s already declares %s (%s)", path.Base(to), n, dl.where(dl.topLevel(n)[0]))
			}
		}
	} else if p.fileExists(to) {
		return nil, false, fmt.Sprintf("%s exists but is in no project of the solution", to)
	}
	created = dest == nil

	// What stays in the old file and uses moved code must import it; the
	// moved code is then exported.
	usedOutside := map[*ast.Symbol]bool{}
	for _, f := range prog.GetSourceFiles() {
		if f.IsDeclarationFile {
			continue
		}
		identifiers(f.AsNode(), func(id *ast.Node) {
			if ast.IsDeclarationName(id) || f == l.f && in(id) {
				return
			}
			if s := symbolOf(c, id); slices.Contains(movedSyms, s) && !(dest != nil && f == dest) {
				if ast.IsImportSpecifier(id.Parent) || ast.IsExportSpecifier(id.Parent) {
					return // an import's name; its file is handled below
				}
				usedOutside[s] = true
			}
		})
	}
	// The moved text.
	var moved strings.Builder
	quote, semi := l.quote(), l.semi()
	for i, s := range moving {
		a, b, ok := l.lineRange(s)
		if !ok {
			return nil, false, fmt.Sprintf("%s shares its lines with other code", l.part(s))
		}
		text := l.text[a:b]
		exported := ast.HasSyntacticModifier(s, ast.ModifierFlagsExport)
		need := false
		for _, n := range topNames(s) {
			sym := c.GetSymbolAtLocation(n)
			need = need || usedOutside[sym]
		}
		if need && !exported {
			at := l.pos(s) - a
			text = text[:at] + "export " + text[at:]
		}
		if i > 0 {
			moved.WriteString("\n")
		}
		moved.WriteString(strings.TrimRight(text, "\n") + "\n")
		a, b = l.withBlank(a, b)
		edits = append(edits, Edit{File: from, Start: a, End: b})
	}

	// Imports the moved code needs at its new place: its file's imports
	// it uses, and declarations that stay, which are exported.
	var needs []importLine
	addNeed := func(spec, n string, def, ns, typeOnly bool) {
		for i := range needs {
			if needs[i].spec == spec && needs[i].typeOnly == typeOnly && !def && !ns && needs[i].def == "" && needs[i].ns == "" {
				if !slices.Contains(needs[i].named, n) {
					needs[i].named = append(needs[i].named, n)
				}
				return
			}
		}
		im := importLine{spec: spec, typeOnly: typeOnly}
		switch {
		case def:
			im.def = n
		case ns:
			im.ns = n
		default:
			im.named = []string{n}
		}
		needs = append(needs, im)
	}
	identifiers(l.f.AsNode(), func(id *ast.Node) {
		if ast.IsDeclarationName(id) || ast.IsImportSpecifier(id.Parent) {
			return
		}
		sym := c.GetSymbolAtLocation(id)
		if id.Parent != nil && ast.IsShorthandPropertyAssignment(id.Parent) {
			sym = c.GetShorthandAssignmentValueSymbol(id.Parent)
		}
		if sym == nil || len(sym.Declarations) == 0 {
			return
		}
		d := sym.Declarations[0]
		if ast.GetSourceFileOfNode(d) != l.f {
			return
		}
		isImport := ast.IsImportSpecifier(d) || ast.IsImportClause(d) || ast.IsNamespaceImport(d)
		if !in(id) {
			return
		}
		switch {
		case isImport:
			decl := d
			for !ast.IsImportDeclaration(decl) {
				decl = decl.Parent
			}
			spec := respec(decl.AsImportDeclaration().ModuleSpecifier.Text(), from, to)
			typeOnly := decl.AsImportDeclaration().ImportClause.IsTypeOnly() || ast.IsImportSpecifier(d) && d.AsImportSpecifier().IsTypeOnly
			switch {
			case ast.IsImportClause(d):
				addNeed(spec, id.Text(), true, false, typeOnly)
			case ast.IsNamespaceImport(d):
				addNeed(spec, id.Text(), false, true, typeOnly)
			default:
				n := id.Text()
				if pn := d.AsImportSpecifier().PropertyName; pn != nil {
					n = pn.Text() + " as " + id.Text()
				}
				addNeed(spec, n, false, false, typeOnly)
			}
		case !slices.Contains(movedSyms, c.GetSymbolAtLocation(id)) && !in(d):
			addNeed(newLinedSpec(to, from, l), id.Text(), false, false, sym.Flags&ast.SymbolFlagsValue == 0)
		}
	})
	// The old file imports what it still uses of the moved code.
	var back []string
	for _, sym := range movedSyms {
		used := false
		identifiers(l.f.AsNode(), func(id *ast.Node) {
			if !used && !in(id) && !ast.IsDeclarationName(id) && symbolOf(c, id) == sym {
				used = true
			}
		})
		if used {
			back = append(back, sym.Name)
		}
	}
	// Imports of the old file that only the moved code used go.
	edits = append(edits, dropImports(c, l, in, len(back) == 0)...)
	if len(back) > 0 {
		im := importLine{spec: l.specFor(to), named: back}
		at, text := l.importAt()
		edits = append(edits, Edit{File: from, Start: at, End: at, NewText: text(im.text(quote, semi))})
	}
	// Other files that import the moved code from the old file import it
	// from the new one.
	for _, f := range prog.GetSourceFiles() {
		if f.IsDeclarationFile || f == l.f {
			continue
		}
		fl := newLined(f)
		var why string
		var fresh []importLine
		moduleSpecifiers(f, func(spec, owner *ast.Node) {
			if why != "" || fileOf(c, spec) != from {
				return
			}
			switch {
			case ast.IsImportDeclaration(owner) && owner.AsImportDeclaration().ImportClause != nil:
				clause := owner.AsImportDeclaration().ImportClause
				nb := clause.AsImportClause().NamedBindings
				if nb != nil && ast.IsNamespaceImport(nb) {
					if uses(c, f.AsNode(), movedSyms) != nil {
						why = fmt.Sprintf("%s imports %s as a namespace and uses what moves; import it by name first", fl.where(owner), path.Base(from))
					}
					return
				}
				if nb == nil {
					return
				}
				list := nb.AsNamedImports().Elements
				var moves []int
				for i, el := range list.Nodes {
					if slices.Contains(movedSyms, symbolOf(c, el.Name())) {
						moves = append(moves, i)
					}
				}
				switch {
				case len(moves) == 0:
				case f == dest:
					// Its uses of a name it imported under an alias take the
					// declaration's own name.
					for _, i := range moves {
						el := list.Nodes[i]
						pn := el.PropertyName()
						if pn == nil || pn.Text() == el.Name().Text() {
							continue
						}
						alias := c.GetSymbolAtLocation(el.Name())
						identifiers(f.AsNode(), func(id *ast.Node) {
							if id == el.Name() {
								return
							}
							s := c.GetSymbolAtLocation(id)
							shorthand := id.Parent != nil && ast.IsShorthandPropertyAssignment(id.Parent) && id.Parent.Name() == id
							if shorthand {
								s = c.GetShorthandAssignmentValueSymbol(id.Parent)
							}
							if s != alias {
								return
							}
							text := pn.Text()
							if shorthand {
								text = id.Text() + ": " + text
							}
							edits = append(edits, Edit{File: f.FileName(), Start: fl.pos(id), End: id.End(), NewText: text})
						})
					}
					if len(moves) == len(list.Nodes) && clause.Name() == nil {
						if a, b, ok := fl.lineRange(owner); ok {
							imports := 0
							for _, s := range f.Statements.Nodes {
								if ast.IsImportDeclaration(s) {
									imports++
								}
							}
							if imports == 1 { // the last import: the blank line after it too
								a, b = fl.withBlank(a, b)
							}
							edits = append(edits, Edit{File: f.FileName(), Start: a, End: b})
						}
					} else {
						for i := len(moves) - 1; i >= 0; i-- {
							edits = append(edits, fl.removeAt(list, moves[i]))
						}
					}
				case len(moves) == len(list.Nodes) && clause.Name() == nil:
					edits = append(edits, Edit{File: f.FileName(), Start: fl.pos(spec) + 1, End: spec.End() - 1, NewText: fl.specFor(to)})
				default:
					var named []string
					for i := len(moves) - 1; i >= 0; i-- {
						el := list.Nodes[moves[i]]
						named = append([]string{fl.source(el)}, named...)
						edits = append(edits, fl.removeAt(list, moves[i]))
					}
					fresh = append(fresh, importLine{spec: fl.specFor(to), named: named, typeOnly: clause.IsTypeOnly()})
				}
			case ast.IsExportDeclaration(owner):
				ed := owner.AsExportDeclaration()
				if ed.ExportClause == nil {
					why = fmt.Sprintf("%s re-exports everything of %s, which would then not have what moves", fl.where(owner), path.Base(from))
					return
				}
				list := ed.ExportClause.AsNamedExports().Elements
				n := 0
				for _, el := range list.Nodes {
					if names[el.PropertyNameOrName().Text()] {
						n++
					}
				}
				switch {
				case n == 0:
				case n == len(list.Nodes):
					edits = append(edits, Edit{File: f.FileName(), Start: fl.pos(spec) + 1, End: spec.End() - 1, NewText: fl.specFor(to)})
				default:
					why = fmt.Sprintf("%s re-exports some of what moves and some of what stays; split it first", fl.where(owner))
				}
			default:
				if !ast.IsImportDeclaration(owner) {
					why = fmt.Sprintf("%s refers to %s by import() or require(), which this does not follow", fl.where(owner), path.Base(from))
				}
			}
		})
		if why != "" {
			return nil, false, why
		}
		if len(fresh) > 0 {
			fq, fs := fl.quote(), fl.semi()
			var texts []string
			for _, im := range fresh {
				texts = append(texts, im.text(fq, fs))
			}
			at, text := fl.importAt()
			edits = append(edits, Edit{File: f.FileName(), Start: at, End: at, NewText: text(strings.Join(texts, "\n"))})
		}
	}
	// The new place.
	var header []string
	if dest != nil {
		quote, semi = dl.quote(), dl.semi()
	}
	for _, im := range needs {
		if dest != nil {
			// What the file imports already from that module.
			im.named = slices.DeleteFunc(im.named, func(n string) bool { return dl.imports(c, im.spec, n) })
			if len(im.named) == 0 && im.def == "" && im.ns == "" {
				continue
			}
		}
		header = append(header, im.text(quote, semi))
	}
	if dest == nil {
		text := moved.String()
		if len(header) > 0 {
			text = strings.Join(header, "\n") + "\n\n" + text
		}
		edits = append(edits, Edit{File: to, NewText: text})
		return edits, true, ""
	}
	if len(header) > 0 {
		at, text := dl.importAt()
		edits = append(edits, Edit{File: to, Start: at, End: at, NewText: text(strings.Join(header, "\n"))})
	}
	end := len(dl.text)
	sep := "\n"
	if !strings.HasSuffix(dl.text, "\n") {
		sep = "\n\n"
	} else if strings.TrimSpace(dl.text) == "" {
		sep = ""
	}
	edits = append(edits, Edit{File: to, Start: end, End: end, NewText: sep + moved.String()})
	return edits, false, ""
}

// newLinedSpec is the specifier for the file at target from a file at
// to, in the style of l's relative imports.
func newLinedSpec(to, target string, l *lined) string {
	ext := ""
	moduleSpecifiers(l.f, func(spec, _ *ast.Node) {
		if s := spec.Text(); strings.HasPrefix(s, ".") && strings.HasSuffix(s, ".js") {
			ext = ".js"
		}
	})
	rel := relPath(path.Dir(to), target)
	spec := strings.TrimSuffix(rel, path.Ext(rel)) + ext
	if !strings.HasPrefix(spec, ".") {
		spec = "./" + spec
	}
	return spec
}

// importAt returns where a new import goes in the file, at the start of
// the line after its last import (where removing that import's lines
// ends) or at its top, and how to write one there.
func (l *lined) importAt() (int, func(string) string) {
	var last *ast.Node
	for _, s := range l.f.Statements.Nodes {
		if ast.IsImportDeclaration(s) {
			last = s
		}
	}
	if last == nil {
		return 0, func(s string) string { return s + "\n\n" }
	}
	line := l.line(last.End() - 1)
	if line == l.count() && !strings.HasSuffix(l.text, "\n") {
		return len(l.text), func(s string) string { return "\n" + s }
	}
	return l.start(line + 1), func(s string) string { return s + "\n" }
}

// imports reports whether the file imports name from the module spec.
func (l *lined) imports(c *checker.Checker, spec, name string) bool {
	for _, s := range l.f.Statements.Nodes {
		if !ast.IsImportDeclaration(s) || s.AsImportDeclaration().ModuleSpecifier.Text() != spec || s.AsImportDeclaration().ImportClause == nil {
			continue
		}
		if nb := s.AsImportDeclaration().ImportClause.AsImportClause().NamedBindings; nb != nil && ast.IsNamedImports(nb) {
			for _, el := range nb.AsNamedImports().Elements.Nodes {
				if el.Name().Text() == name {
					return true
				}
			}
		}
	}
	return false
}

func joinNames(ns []*ast.Node) string {
	var out []string
	for _, n := range ns {
		out = append(out, n.Text())
	}
	return strings.Join(out, ", ")
}

// fileExists reports whether a file is on disk.
func (p *Project) fileExists(path string) bool {
	_, ok := p.fs.ReadFile(path)
	return ok
}
