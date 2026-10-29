// Package inspect renders code for a model to read: a module overview, a
// file without its function bodies (each with simple complexity metrics in
// its place), and source with line numbers.
//
// Line numbers matter: every refactoring addresses code by line, so the
// model should read code through this view rather than guess.
package inspect

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// MaxLines is the largest number of source lines shown without an
// explicit range; a file longer than that without its bodies shows the
// outline and asks for a range.
const MaxLines = 400

// Query selects what to show. All fields are optional.
type Query struct {
	File      string `json:"file,omitempty"`
	Function  string `json:"function,omitempty"` // "Name" or "Type.Method"
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	// Targets reads several of the views above in one call, in place of
	// the fields above.
	Targets []Query `json:"targets,omitempty"`
	// Names reads declarations by name, found anywhere in the module,
	// in place of the fields above: "Name", "Type.Method", or
	// "pkg.Name" where the name is declared in several packages.
	Names []string `json:"names,omitempty"`
	// Uses lists every use of what each name names (Name, Type.Field,
	// Type.Method, pkg.Name), found by the type checker, under what is
	// done with it, in place of the fields above.
	Uses []string `json:"uses,omitempty"`
}

// Options set what a read may show.
type Options struct {
	// ByName shows source only for what a query names (function, names,
	// uses): a line range is refused.
	ByName bool
}

// part says how a view tells the model to read part of a file.
func (o Options) part() string {
	if o.ByName {
		return "function"
	}
	return "function or start_line/end_line"
}

// Run renders the view selected by q, with every view allowed.
func Run(ws *workspace.Workspace, q Query) (string, error) {
	return RunWith(ws, q, Options{})
}

// Module lists the module's packages and files with line counts.
func Module(ws *workspace.Workspace, o Options) string {
	type fileInfo struct {
		rel   string
		lines int
	}
	type pkgInfo struct {
		name  string
		files []fileInfo
	}
	dirs := map[string]*pkgInfo{}
	seen := map[string]bool{}
	for _, p := range ws.Pkgs {
		for _, f := range p.Syntax {
			tf := ws.Fset.File(f.Pos())
			if tf == nil || seen[tf.Name()] || !ws.Contains(tf.Name()) {
				continue
			}
			seen[tf.Name()] = true
			rel := ws.Rel(tf.Name())
			dir := filepath.ToSlash(filepath.Dir(rel))
			if dirs[dir] == nil {
				dirs[dir] = &pkgInfo{}
			}
			pi := dirs[dir]
			if !strings.HasSuffix(f.Name.Name, "_test") { // skip external test packages
				pi.name = f.Name.Name
			}
			pi.files = append(pi.files, fileInfo{rel, tf.LineCount()})
		}
	}
	var names []string
	for d := range dirs {
		names = append(names, d)
	}
	sort.Strings(names)

	var sb strings.Builder
	fmt.Fprintf(&sb, "module root: %s\n", ws.Root)
	total := 0
	sb.WriteString("\nPackages, one a line, with their files' line counts (and the package name where it is not the directory's):\n")
	for _, d := range names {
		pi := dirs[d]
		sort.Slice(pi.files, func(i, j int) bool { return pi.files[i].rel < pi.files[j].rel })
		files, lines := make([]string, len(pi.files)), 0
		for i, f := range pi.files {
			files[i] = fmt.Sprintf("%s %d", filepath.Base(f.rel), f.lines)
			lines += f.lines
		}
		total += lines
		name := "" // the package's name, when it is not the directory's
		if pi.name != path.Base(d) {
			name = "package " + pi.name + ", "
		}
		fmt.Fprintf(&sb, "%s (%s%d lines): %s\n", d, name, lines, strings.Join(files, ", "))
	}
	fmt.Fprintf(&sb, "\n%d packages, %d files, %d lines\n", len(names), len(seen), total)
	if errs := ws.Errors(); len(errs) > 0 {
		fmt.Fprintf(&sb, "\nthe module does not type-check (%d errors); refactorings are disabled until it does:\n  %s\n",
			len(errs), strings.Join(errs, "\n  "))
	}
	fmt.Fprintf(&sb, "\nUse inspect with file (and optionally %s), targets for several, or names for declarations by name, to read code.\n", o.part())
	return sb.String()
}

// Decl summarizes one top-level declaration.
type Decl struct {
	Kind               string // func, method, type, var, const
	Name               string // "Name" or "Type.Method"
	Signature          string
	Start, End         int    // lines, including the doc comment
	Complexity         int    // cyclomatic complexity (functions only)
	Nesting            int    // maximum control-flow nesting depth (functions only)
	Effects            string // functions: "no side effects", or "side effects: <why>"; "" when not known
	BodyStart, BodyEnd int    // functions: the lines inside the braces; 0 when there are fewer than 2
}

// File renders q.File without its function bodies, or the function or
// lines q asks for.
func File(ws *workspace.Workspace, q Query, o Options) (string, error) {
	if q.Function != "" {
		return declarations(ws, q.File, []string{q.Function})
	}
	path := ws.Abs(q.File)
	pkg, file := ws.FileSyntax(path)
	if file == nil {
		return "", refactor.Refusef("file %s is not part of the module's packages (call inspect without arguments to list files)", q.File)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	decls := Outline(ws.Fset, file)
	addEffects(ws, pkg, file, decls)
	lines := strings.SplitAfter(string(src), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%s  (package %s, %d lines)\n", ws.Rel(path), file.Name.Name, len(lines))

	start, end := 1, len(lines)
	switch {
	case q.StartLine != 0 || q.EndLine != 0:
		if o.ByName {
			return "", refactor.Refusef("lines by number are not shown here: read by name (function, names, or uses for where something is used)")
		}
		if q.StartLine > 0 {
			start = q.StartLine
		}
		if q.EndLine > 0 {
			end = q.EndLine
		}
		if start > end || start > len(lines) {
			return "", refactor.Refusef("invalid line range %d-%d; %s has %d lines", start, end, q.File, len(lines))
		}
		end = min(end, len(lines))
	default:
		sk := skeleton(lines, decls)
		if len(sk) > MaxLines {
			sb.WriteByte('\n')
			writeOutline(&sb, decls)
			fmt.Fprintf(&sb, "\nWithout its function bodies the file still has %d lines; pass %s to see source.\n", len(sk), o.part())
			return sb.String(), nil
		}
		fmt.Fprintf(&sb, "\nsource, function bodies elided (inspect with %s, to read one):\n", o.part())
		sb.WriteString(strings.Join(sk, ""))
		return sb.String(), nil
	}

	fmt.Fprintf(&sb, "\nsource (lines %d-%d):\n", start, end)
	width := len(fmt.Sprint(end))
	for i := start; i <= end; i++ {
		sb.WriteString(numbered(width, i, lines[i-1]))
	}
	return sb.String(), nil
}

// writeOutline writes the outline of decls: each declaration's line range
// and signature, with metrics for functions.
func writeOutline(sb *strings.Builder, decls []Decl) {
	sb.WriteString("outline:\n")
	for _, d := range decls {
		sb.WriteString(outlineLine(d))
	}
}

// outlineLine is d's line of the outline: its line range and signature,
// with metrics for functions.
func outlineLine(d Decl) string {
	f := facts(d)
	if f != "" {
		f = "   " + f
	}
	return fmt.Sprintf("  %4d-%-4d %s%s\n", d.Start, d.End, d.Signature, f)
}

// facts returns a function's length, metrics and effects in brackets, or
// "" for other declarations.
func facts(d Decl) string {
	if d.Kind != "func" && d.Kind != "method" {
		return ""
	}
	n := d.End - d.Start + 1
	facts := []string{fmt.Sprintf("%d line%s", n, textutil.Plural(n)), fmt.Sprintf("complexity %d", d.Complexity), fmt.Sprintf("nesting %d", d.Nesting)}
	if d.Effects != "" {
		facts = append(facts, d.Effects)
	}
	return "[" + strings.Join(facts, ", ") + "]"
}

// skeleton returns lines, numbered, with each function's body elided: in
// its place a line says which lines it takes and gives the function's
// facts, which follow a function written on one line.
func skeleton(lines []string, decls []Decl) []string {
	width := len(fmt.Sprint(len(lines)))
	pad := strings.Repeat(" ", width+6)
	byBody := map[int]Decl{} // by the first line of the body
	byEnd := map[int]Decl{}  // functions without body lines, by their last line
	for _, d := range decls {
		switch {
		case d.Kind != "func" && d.Kind != "method":
		case d.BodyStart > 0:
			byBody[d.BodyStart] = d
		default:
			byEnd[d.End] = d
		}
	}
	var out []string
	for l := 1; l <= len(lines); l++ {
		if d, ok := byBody[l]; ok {
			out = append(out, fmt.Sprintf("%s… lines %d-%d   %s\n", pad, d.BodyStart, d.BodyEnd, facts(d)))
			l = d.BodyEnd
			continue
		}
		out = append(out, numbered(width, l, lines[l-1]))
		if d, ok := byEnd[l]; ok {
			out = append(out, pad+facts(d)+"\n")
		}
	}
	return out
}

// numbered is line l of the source, numbered in a column width wide.
func numbered(width, l int, line string) string {
	return fmt.Sprintf("%*d  %s\n", width, l, strings.TrimRight(line, "\n"))
}

// targets renders each of q's targets in turn; one that is refused says
// so in its place.
func targets(ws *workspace.Workspace, q Query, o Options) (string, error) {
	if q.File != "" || q.Function != "" || q.StartLine != 0 || q.EndLine != 0 {
		return "", refactor.Refusef("give either targets or file (with function or start_line/end_line), not both")
	}
	var parts []string
	for i, t := range q.Targets {
		if len(t.Targets) > 0 {
			return "", refactor.Refusef("target %d has targets of its own; give one flat list", i+1)
		}
		out, err := RunWith(ws, t, o)
		switch {
		case refactor.IsRefusal(err):
			out = fmt.Sprintf("target %d: %v\n", i+1, err)
		case err != nil:
			return "", err
		}
		parts = append(parts, out)
	}
	return strings.Join(parts, "\n"), nil
}

// names renders the declarations q.Names names, each found in the module
// by its name, grouped by file in the order first asked for; one not
// found, or found in several places, says so in its place.
func names(ws *workspace.Workspace, q Query) (string, error) {
	if q.File != "" || q.Function != "" || q.StartLine != 0 || q.EndLine != 0 || len(q.Targets) > 0 {
		return "", refactor.Refusef("give names alone: each is found in the module by its name, without file")
	}
	type part struct {
		file   string // "" for a refusal, in text
		locals []string
		text   string
	}
	var parts []*part
	byFile := map[string]*part{}
	for _, n := range q.Names {
		m, err := resolve(ws, n)
		switch {
		case refactor.IsRefusal(err):
			parts = append(parts, &part{text: fmt.Sprintf("%s: %v\n", n, err)})
			continue
		case err != nil:
			return "", err
		}
		if p := byFile[m.file]; p != nil {
			p.locals = append(p.locals, m.local)
			continue
		}
		byFile[m.file] = &part{file: m.file, locals: []string{m.local}}
		parts = append(parts, byFile[m.file])
	}
	out := make([]string, len(parts))
	for i, p := range parts {
		if p.file == "" {
			out[i] = p.text
			continue
		}
		text, err := declarations(ws, p.file, p.locals)
		if err != nil {
			return "", err
		}
		out[i] = text
	}
	return strings.Join(out, "\n"), nil
}

// match is a declaration found by its name: the file, its package's
// name, and the name within the file (Name or Type.Method).
type match struct{ file, pkg, local string }

// resolve finds the one declaration in the module named name: Name or
// Type.Method, or either after its package's name (pkg.Name).
func resolve(ws *workspace.Workspace, name string) (match, error) {
	var found []match
	seen := map[string]bool{}
	for _, p := range ws.Pkgs {
		for _, f := range p.Syntax {
			tf := ws.Fset.File(f.Pos())
			if tf == nil || seen[tf.Name()] || !ws.Contains(tf.Name()) {
				continue
			}
			seen[tf.Name()] = true
			decls := Outline(ws.Fset, f)
			local := name
			if pkg, rest, ok := strings.Cut(name, "."); ok && pkg == f.Name.Name && find(decls, rest) != nil {
				local = rest
			}
			if find(decls, local) != nil {
				found = append(found, match{ws.Rel(tf.Name()), f.Name.Name, local})
			}
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].file < found[j].file })
	switch len(found) {
	case 0:
		return match{}, undeclared(ws, name)
	case 1:
		return found[0], nil
	}
	places := make([]string, len(found))
	for i, m := range found {
		places[i] = fmt.Sprintf("%s (package %s)", m.file, m.pkg)
	}
	return match{}, refactor.Refusef("declared in %s; name it more fully (pkg.Name, Type.Method)", strings.Join(places, ", "))
}

// undeclared is the refusal for name, which nothing in the module declares:
// whether a package (all of them, for a name without its package's) may
// declare it, or it is predeclared, or a file there imports a package by
// that name.
func undeclared(ws *workspace.Workspace, name string) error {
	where, local, pkg := "the module", name, ""
	if p, rest, ok := strings.Cut(name, "."); ok {
		for _, q := range ws.Pkgs {
			if q.Name == p {
				where, local, pkg = "package "+p, rest, p
				break
			}
		}
	}
	var importers []string
	for _, p := range ws.Pkgs {
		if pkg != "" && p.Name != pkg {
			continue
		}
		for _, f := range p.Syntax {
			tf := ws.Fset.File(f.Pos())
			if tf == nil || !ws.Contains(tf.Name()) || slices.Contains(importers, ws.Rel(tf.Name())) {
				continue
			}
			for _, imp := range f.Imports {
				if pn := importedName(p.TypesInfo, imp); pn == local {
					importers = append(importers, ws.Rel(tf.Name()))
				}
			}
		}
	}
	sort.Strings(importers)
	switch {
	case len(importers) > 0:
		verb := "imports"
		if len(importers) > 1 {
			verb = "import"
		}
		return refactor.Refusef("no declaration of that name in %s; but %s %s a package as %s, which a package-level %s would clash with", where, strings.Join(importers, ", "), verb, local, local)
	case types.Universe.Lookup(local) != nil:
		return refactor.Refusef("no declaration of that name in %s; it is predeclared, which a package-level %s would shadow", where, local)
	}
	return refactor.Refusef("no declaration of that name in %s; it is free to declare", where)
}

// importedName is the name imp binds in its file, "" for a blank or dot
// import.
func importedName(info *types.Info, imp *ast.ImportSpec) string {
	if imp.Name != nil {
		if imp.Name.Name == "_" || imp.Name.Name == "." {
			return ""
		}
		return imp.Name.Name
	}
	if pn, ok := info.Implicits[imp].(*types.PkgName); ok {
		return pn.Name()
	}
	return ""
}

// declarations renders file's header line, then the source of each
// declaration locals names (Name or Type.Method), in that order.
func declarations(ws *workspace.Workspace, file string, locals []string) (string, error) {
	path := ws.Abs(file)
	pkg, f := ws.FileSyntax(path)
	if f == nil {
		return "", refactor.Refusef("file %s is not part of the module's packages (call inspect without arguments to list files)", file)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	decls := Outline(ws.Fset, f)
	addEffects(ws, pkg, f, decls)
	lines := strings.SplitAfter(string(src), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%s  (package %s, %d lines)\n", ws.Rel(path), f.Name.Name, len(lines))
	for i, name := range locals {
		d := find(decls, name)
		if d == nil {
			return "", refactor.Refusef("no function %q in %s; inspect the file to see what is there", name, file)
		}
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(outlineLine(*d))
		fmt.Fprintf(&sb, "\nsource (lines %d-%d):\n", d.Start, d.End)
		width := len(fmt.Sprint(d.End))
		for l := d.Start; l <= d.End; l++ {
			sb.WriteString(numbered(width, l, lines[l-1]))
		}
	}
	return sb.String(), nil
}

func find(decls []Decl, name string) *Decl {
	for i := range decls {
		if decls[i].Name == name {
			return &decls[i]
		}
	}
	// Accept a bare method name if it is unambiguous.
	var match *Decl
	for i := range decls {
		if decls[i].Kind == "method" && strings.HasSuffix(decls[i].Name, "."+name) {
			if match != nil {
				return nil
			}
			match = &decls[i]
		}
	}
	return match
}

// Outline summarizes the top-level declarations of f in source order.
func Outline(fset *token.FileSet, f *ast.File) []Decl {
	var out []Decl
	line := func(p token.Pos) int { return fset.Position(p).Line }
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			start := d.Pos()
			if d.Doc != nil {
				start = d.Doc.Pos()
			}
			dd := Decl{Kind: "func", Name: d.Name.Name, Start: line(start), End: line(d.End())}
			if d.Recv != nil && len(d.Recv.List) > 0 {
				dd.Kind = "method"
				dd.Name = ReceiverTypeName(d.Recv.List[0].Type) + "." + d.Name.Name
			}
			dd.Signature = header(fset, d)
			if d.Body != nil {
				dd.Complexity, dd.Nesting = Metrics(d.Body)
				if lb, rb := line(d.Body.Lbrace), line(d.Body.Rbrace); rb-lb > 2 {
					dd.BodyStart, dd.BodyEnd = lb+1, rb-1
				}
			}
			out = append(out, dd)
		case *ast.GenDecl:
			if d.Tok == token.IMPORT {
				continue
			}
			start := d.Pos()
			if d.Doc != nil {
				start = d.Doc.Pos()
			}
			for _, spec := range d.Specs {
				dd := Decl{Kind: d.Tok.String(), Start: line(start), End: line(d.End())}
				if d.Lparen.IsValid() { // grouped: one entry per spec
					s, e := spec.Pos(), spec.End()
					switch sp := spec.(type) {
					case *ast.TypeSpec:
						if sp.Doc != nil {
							s = sp.Doc.Pos()
						}
					case *ast.ValueSpec:
						if sp.Doc != nil {
							s = sp.Doc.Pos()
						}
					}
					dd.Start, dd.End = line(s), line(e)
				}
				switch sp := spec.(type) {
				case *ast.TypeSpec:
					dd.Name = sp.Name.Name
					dd.Signature = typeHeader(fset, sp)
				case *ast.ValueSpec:
					var names []string
					for _, n := range sp.Names {
						names = append(names, n.Name)
					}
					dd.Name = strings.Join(names, ", ")
					dd.Signature = d.Tok.String() + " " + dd.Name
				}
				out = append(out, dd)
			}
		}
	}
	return out
}

// addEffects fills in Effects for the functions among decls, the outline
// of file, from the summaries of what calling each can do (see
// analysis.Effects).
func addEffects(ws *workspace.Workspace, pkg *packages.Package, file *ast.File, decls []Decl) {
	effects := ws.Effects()
	i := 0
	for _, d := range file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		for i < len(decls) && decls[i].Kind != "func" && decls[i].Kind != "method" {
			i++
		}
		if i == len(decls) {
			return
		}
		if fn, ok := pkg.TypesInfo.Defs[fd.Name].(*types.Func); ok && fd.Body != nil {
			if s := effects.Func(fn); s != nil {
				decls[i].Effects = "no side effects"
				if s.Effect != "" {
					decls[i].Effects = "side effects: " + s.Effect
				}
			}
		}
		i++
	}
}

// Metrics returns the cyclomatic complexity of a function body (1 + one per
// branch point) and its maximum nesting depth of control statements.
func Metrics(body *ast.BlockStmt) (complexity, nesting int) {
	complexity = 1
	var stack []ast.Node
	depth := 0
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if isControl(top) {
				depth--
			}
			return true
		}
		stack = append(stack, n)
		if isControl(n) {
			depth++
			nesting = max(nesting, depth)
		}
		switch n := n.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
			complexity++
		case *ast.CaseClause:
			if n.List != nil { // not default
				complexity++
			}
		case *ast.CommClause:
			if n.Comm != nil {
				complexity++
			}
		case *ast.BinaryExpr:
			if n.Op == token.LAND || n.Op == token.LOR {
				complexity++
			}
		case *ast.FuncLit:
			// A closure's control flow still counts toward its enclosing
			// function's complexity: it is code the reader has to follow.
		}
		return true
	})
	return complexity, nesting
}

func isControl(n ast.Node) bool {
	switch n.(type) {
	case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
		return true
	}
	return false
}

func header(fset *token.FileSet, d *ast.FuncDecl) string {
	h := &ast.FuncDecl{Recv: d.Recv, Name: d.Name, Type: d.Type}
	var buf bytes.Buffer
	printer.Fprint(&buf, fset, h)
	return strings.Join(strings.Fields(buf.String()), " ")
}

func ReceiverTypeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return ReceiverTypeName(t.X)
	case *ast.IndexExpr:
		return ReceiverTypeName(t.X)
	case *ast.IndexListExpr:
		return ReceiverTypeName(t.X)
	case *ast.Ident:
		return t.Name
	}
	return "?"
}

// typeHeader renders "type Name[T any] struct" (or "= Other" for aliases),
// abbreviating the type body to its kind.
func typeHeader(fset *token.FileSet, sp *ast.TypeSpec) string {
	h := &ast.TypeSpec{Name: sp.Name, TypeParams: sp.TypeParams, Assign: sp.Assign, Type: ast.NewIdent(typeKind(fset, sp.Type))}
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, h); err != nil {
		return "type " + sp.Name.Name
	}
	return "type " + strings.Join(strings.Fields(buf.String()), " ")
}

func typeKind(fset *token.FileSet, e ast.Expr) string {
	switch e.(type) {
	case *ast.StructType:
		return "struct"
	case *ast.InterfaceType:
		return "interface"
	}
	var buf bytes.Buffer
	printer.Fprint(&buf, fset, e)
	s := strings.Join(strings.Fields(buf.String()), " ")
	if len(s) > 60 {
		s = s[:57] + "..."
	}
	return s
}

// RunWith renders the view selected by q, as o allows: the module
// overview when File is empty, otherwise the file without its function
// bodies, or the function or lines asked for; or each of its targets, or
// each declaration it names.
func RunWith(ws *workspace.Workspace, q Query, o Options) (string, error) {
	if len(q.Uses) > 0 {
		return uses(ws, q)
	}
	if len(q.Names) > 0 {
		return names(ws, q)
	}
	if len(q.Targets) > 0 {
		return targets(ws, q, o)
	}
	if q.File == "" {
		if q.Function != "" || q.StartLine != 0 || q.EndLine != 0 {
			return "", refactor.Refusef("file is required when function or a line range is given")
		}
		return Module(ws, o), nil
	}
	return File(ws, q, o)
}
