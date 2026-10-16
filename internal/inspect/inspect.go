// Package inspect renders code for a model to read: a module overview, a
// per-file outline with simple complexity metrics, and source with line
// numbers.
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
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cwfreeman/gorefactor/internal/refactor"
	"github.com/cwfreeman/gorefactor/internal/workspace"
)

// MaxLines is the largest number of source lines shown without an
// explicit range; longer files show the outline and ask for a range.
const MaxLines = 400

// Query selects what to show. All fields are optional.
type Query struct {
	File      string `json:"file,omitempty"`
	Function  string `json:"function,omitempty"` // "Name" or "Type.Method"
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
}

// Run renders the view selected by q: the module overview when File is
// empty, otherwise the file's outline plus numbered source.
func Run(ws *workspace.Workspace, q Query) (string, error) {
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
	for _, d := range names {
		pi := dirs[d]
		sort.Slice(pi.files, func(i, j int) bool { return pi.files[i].rel < pi.files[j].rel })
		fmt.Fprintf(&sb, "\n%s/  (package %s)\n", d, pi.name)
		for _, f := range pi.files {
			fmt.Fprintf(&sb, "  %-40s %5d lines\n", filepath.Base(f.rel), f.lines)
			total += f.lines
		}
	}
	fmt.Fprintf(&sb, "\n%d packages, %d files, %d lines\n", len(names), len(seen), total)
	if errs := ws.Errors(); len(errs) > 0 {
		fmt.Fprintf(&sb, "\nthe module does not type-check (%d errors); refactorings are disabled until it does:\n  %s\n",
			len(errs), strings.Join(errs, "\n  "))
	}
	sb.WriteString("\nUse inspect with file (and optionally function or start_line/end_line) to read code.\n")
	return sb.String()
}

// Decl summarizes one top-level declaration.
type Decl struct {
	Kind       string // func, method, type, var, const
	Name       string // "Name" or "Type.Method"
	Signature  string
	Start, End int // lines, including the doc comment
	Complexity int // cyclomatic complexity (functions only)
	Nesting    int // maximum control-flow nesting depth (functions only)
}

// File renders the outline of q.File and its source (or the requested part).
func File(ws *workspace.Workspace, q Query) (string, error) {
	path := ws.Abs(q.File)
	_, file := ws.FileSyntax(path)
	if file == nil {
		return "", refactor.Refusef("file %s is not part of the module's packages (call inspect without arguments to list files)", q.File)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	decls := Outline(ws.Fset, file)
	lines := strings.SplitAfter(string(src), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%s  (package %s, %d lines)\n\n", ws.Rel(path), file.Name.Name, len(lines))
	sb.WriteString("outline:\n")
	for _, d := range decls {
		metrics := ""
		if d.Kind == "func" || d.Kind == "method" {
			n := d.End - d.Start + 1
			metrics = fmt.Sprintf("   [%d line%s, complexity %d, nesting %d]", n, plural(n), d.Complexity, d.Nesting)
		}
		fmt.Fprintf(&sb, "  %4d-%-4d %s%s\n", d.Start, d.End, d.Signature, metrics)
	}

	start, end := 1, len(lines)
	switch {
	case q.Function != "":
		d := find(decls, q.Function)
		if d == nil {
			return "", refactor.Refusef("no function %q in %s; the outline lists what is there", q.Function, q.File)
		}
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
	case len(lines) > MaxLines:
		fmt.Fprintf(&sb, "\nThe file has %d lines; pass function or start_line/end_line to see source.\n", len(lines))
		return sb.String(), nil
	}

	fmt.Fprintf(&sb, "\nsource (lines %d-%d):\n", start, end)
	width := len(fmt.Sprint(end))
	for i := start; i <= end; i++ {
		fmt.Fprintf(&sb, "%*d  %s", width, i, strings.TrimRight(lines[i-1], "\n"))
		sb.WriteByte('\n')
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
				dd.Name = recvTypeName(d.Recv.List[0].Type) + "." + d.Name.Name
			}
			dd.Signature = header(fset, d)
			if d.Body != nil {
				dd.Complexity, dd.Nesting = Metrics(d.Body)
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

func recvTypeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return recvTypeName(t.X)
	case *ast.IndexExpr:
		return recvTypeName(t.X)
	case *ast.IndexListExpr:
		return recvTypeName(t.X)
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

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
