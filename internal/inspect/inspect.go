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
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
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
}

// Run renders the view selected by q: the module overview when File is
// empty, otherwise the file without its function bodies, or the function
// or lines asked for; or each of its targets.
func Run(ws *workspace.Workspace, q Query) (string, error) {
	if len(q.Targets) > 0 {
		return targets(ws, q)
	}
	if q.File == "" {
		if q.Function != "" || q.StartLine != 0 || q.EndLine != 0 {
			return "", refactor.Refusef("file is required when function or a line range is given")
		}
		return Module(ws), nil
	}
	return File(ws, q)
}

// Module lists the module's packages and files with line counts.
func Module(ws *workspace.Workspace) string {
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
	sb.WriteString("\nUse inspect with file (and optionally function or start_line/end_line), or targets for several, to read code.\n")
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
func File(ws *workspace.Workspace, q Query) (string, error) {
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
	case q.Function != "":
		d := find(decls, q.Function)
		if d == nil {
			return "", refactor.Refusef("no function %q in %s; inspect the file to see what is there", q.Function, q.File)
		}
		sb.WriteString(outlineLine(*d))
		start, end = d.Start, d.End
	case q.StartLine != 0 || q.EndLine != 0:
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
			fmt.Fprintf(&sb, "\nWithout its function bodies the file still has %d lines; pass function or start_line/end_line to see source.\n", len(sk))
			return sb.String(), nil
		}
		sb.WriteString("\nsource, function bodies elided (inspect with function, or start_line/end_line, to read one):\n")
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
func targets(ws *workspace.Workspace, q Query) (string, error) {
	if q.File != "" || q.Function != "" || q.StartLine != 0 || q.EndLine != 0 {
		return "", refactor.Refusef("give either targets or file (with function or start_line/end_line), not both")
	}
	var parts []string
	for i, t := range q.Targets {
		if len(t.Targets) > 0 {
			return "", refactor.Refusef("target %d has targets of its own; give one flat list", i+1)
		}
		out, err := Run(ws, t)
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
	effects := analysis.NewEffects()
	for _, p := range ws.Pkgs {
		effects.AddPackage(p.Syntax, p.TypesInfo)
	}
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
