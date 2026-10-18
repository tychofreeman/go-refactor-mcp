// Package replacestmts implements replace_statements: an edit that
// replaces whole statements of one block with new Go statements.
package replacestmts

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"golang.org/x/tools/go/packages"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// ReplaceStatements is the "replace_statements" edit.
type ReplaceStatements struct{}

// New returns the replace_statements edit.
func New() *ReplaceStatements { return &ReplaceStatements{} }

func (*ReplaceStatements) Name() string { return "replace_statements" }

func (*ReplaceStatements) Description() string {
	return `Replace whole statements of one block with new Go statements. This is an edit, not a refactoring: it may change behavior.
Use it to write a function's body (a stub's "return nil, errors.New(...)" becomes the implementation), or to change what some statements do; use the refactorings to restructure code without changing it.
Identify the statements by file, start_line and end_line (inclusive, as inspect numbers them); the lines must cover whole statements of one block, and comments and blank lines in them are replaced too. content is the new statements, without the enclosing braces; it is re-indented to the block.
Packages the new code names are imported: one another file of the package imports under that name, else a workspace package, else a standard library package of that name; when several fit (rand), list the import path to use in imports. Imports only the replaced statements used are removed.
Like every step, the result must type-check, or it is refused with the errors and nothing changes.`
}

func (*ReplaceStatements) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":       {"type": "string",  "description": "Path of the file, relative to the workspace root."},
    "start_line": {"type": "integer", "description": "1-based first line of the statements to replace."},
    "end_line":   {"type": "integer", "description": "1-based last line of the statements to replace (inclusive)."},
    "content":    {"type": "string",  "description": "The new statements, as Go source, without enclosing braces."},
    "imports":    {"type": "array", "items": {"type": "string"}, "description": "Optional: import paths to use for package names the content uses, when the name alone is ambiguous (e.g. math/rand)."}
  },
  "required": ["file", "start_line", "end_line", "content"],
  "additionalProperties": false
}`)
}

// ChangesBehavior marks this as an edit.
func (*ReplaceStatements) ChangesBehavior() {}

type args struct {
	File      string   `json:"file"`
	StartLine int      `json:"start_line"`
	EndLine   int      `json:"end_line"`
	Content   string   `json:"content"`
	Imports   []string `json:"imports,omitempty"`
}

// Plan checks preconditions and computes the edits.
func (*ReplaceStatements) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	pkg, file := ws.FileSyntax(path)
	if file == nil {
		return nil, refactor.Refusef("file %s is not part of the loaded packages", a.File)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	tf := ws.Fset.File(file.Pos())
	if a.StartLine < 1 || a.EndLine < a.StartLine || a.EndLine > tf.LineCount() {
		return nil, refactor.Refusef("lines %d-%d are not a range of lines of %s, which has %d", a.StartLine, a.EndLine, a.File, tf.LineCount())
	}
	stmts, err := selectStatements(tf, file, src, a.StartLine, a.EndLine)
	if err != nil {
		return nil, err
	}
	content, err := parseContent(a.Content)
	if err != nil {
		return nil, err
	}

	start := tf.Offset(tf.LineStart(a.StartLine))
	end := len(src)
	if a.EndLine < tf.LineCount() {
		end = tf.Offset(tf.LineStart(a.EndLine + 1))
	}
	adder := imports.New(ws, pkg, file, src)
	added, err := addImports(ws, pkg, file, adder, stmts[0].Pos(), content, a.Imports)
	if err != nil {
		return nil, err
	}
	removed := removeImports(pkg, file, adder, stmts[0].Pos(), stmts[len(stmts)-1].End(), content)

	text := ""
	if strings.TrimSpace(a.Content) != "" {
		text = reindent(a.Content, indentOf(src, tf.Offset(tf.LineStart(tf.Line(stmts[0].Pos())))), content.raw) + "\n"
	}
	plan := &refactor.Plan{
		Summary: fmt.Sprintf("replaced lines %d-%d (%d statement%s) of %s", a.StartLine, a.EndLine, len(stmts), textutil.Plural(len(stmts)), a.File),
		Edits:   []edit.FileEdit{{Path: path, Edits: append([]edit.TextEdit{{Start: start, End: end, NewText: text}}, adder.Edits()...)}},
	}
	if len(added) > 0 {
		plan.Notes = append(plan.Notes, "added import "+strings.Join(quoted(added), ", "))
	}
	if len(removed) > 0 {
		plan.Notes = append(plan.Notes, "removed import "+strings.Join(quoted(removed), ", ")+", which only the replaced statements used")
	}
	return plan, nil
}

// selectStatements returns the statements lines first-last cover: whole
// statements of one block, with nothing else on those lines but comments.
func selectStatements(tf *token.File, file *ast.File, src []byte, first, last int) ([]ast.Stmt, error) {
	line := func(p token.Pos) int { return tf.Line(p) }
	var cut ast.Stmt
	var inside []ast.Stmt // the outermost statements within the lines
	ast.Inspect(file, func(n ast.Node) bool {
		s, ok := n.(ast.Stmt)
		if !ok || n == nil {
			return true
		}
		a, b := line(s.Pos()), line(s.End())
		switch {
		case b < first || a > last:
			return false
		case a >= first && b <= last:
			inside = append(inside, s)
			return false
		case a <= first && b >= last:
			if _, block := s.(*ast.BlockStmt); !block && (a == first || b == last) && cut == nil {
				if !(a == first && b == last) {
					cut = s
				}
			}
			return true
		default:
			if cut == nil {
				cut = s
			}
			return true
		}
	})
	if cut != nil {
		what := analysis.Describe(cut)
		what = "the " + what[strings.IndexByte(what, ' ')+1:] // "an if statement" -> "the if statement"
		return nil, refactor.Refusef("lines %d-%d cut %s on lines %d-%d; select whole statements", first, last, what, line(cut.Pos()), line(cut.End()))
	}
	if len(inside) == 0 {
		return nil, refactor.Refusef("lines %d-%d hold no whole statement", first, last)
	}
	// They must all be statements of one list.
	parent := listOf(file, inside[0])
	for _, s := range inside[1:] {
		if listOf(file, s) != parent {
			return nil, refactor.Refusef("lines %d-%d cover statements of different blocks", first, last)
		}
	}
	// Nothing else on the lines but comments.
	start := tf.Offset(tf.LineStart(first))
	end := len(src)
	if last < tf.LineCount() {
		end = tf.Offset(tf.LineStart(last + 1))
	}
	pos := start
	for _, s := range inside {
		if err := onlyComments(src[pos:tf.Offset(s.Pos())], first); err != nil {
			return nil, err
		}
		pos = tf.Offset(s.End())
	}
	if err := onlyComments(src[pos:end], first); err != nil {
		return nil, err
	}
	return inside, nil
}

// listOf returns the statement list holding s (by its owner).
func listOf(file *ast.File, s ast.Stmt) ast.Node {
	var owner ast.Node
	ast.Inspect(file, func(n ast.Node) bool {
		var list []ast.Stmt
		switch n := n.(type) {
		case *ast.BlockStmt:
			list = n.List
		case *ast.CaseClause:
			list = n.Body
		case *ast.CommClause:
			list = n.Body
		}
		for _, x := range list {
			if x == s {
				owner = n
			}
		}
		return owner == nil
	})
	return owner
}

func onlyComments(text []byte, line int) error {
	var s scanner.Scanner
	fset := token.NewFileSet()
	s.Init(fset.AddFile("", -1, len(text)), text, nil, scanner.ScanComments)
	for {
		_, tok, lit := s.Scan()
		switch {
		case tok == token.EOF:
			return nil
		case tok == token.COMMENT, tok == token.SEMICOLON && lit == "\n":
		default:
			return refactor.Refusef("the lines hold code outside whole statements (%s); select whole statements", strings.TrimSpace(tok.String()+" "+lit))
		}
	}
}

// content is the parsed new statements.
type content struct {
	body *ast.BlockStmt
	raw  bool // a raw string spans lines, so the text must not be re-indented
}

func parseContent(text string) (*content, error) {
	src := "package p\nfunc _() {\n" + text + "\n}\n"
	f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.ParseComments)
	if err != nil {
		return nil, refactor.Refusef("content is not a list of Go statements: %v", err)
	}
	c := &content{body: f.Decls[0].(*ast.FuncDecl).Body}
	ast.Inspect(c.body, func(n ast.Node) bool {
		if b, ok := n.(*ast.BasicLit); ok && strings.HasPrefix(b.Value, "`") && strings.Contains(b.Value, "\n") {
			c.raw = true
		}
		return true
	})
	return c, nil
}

// declared returns the names the content declares anywhere in it, which
// a selector's operand may then be, rather than a package.
func (c *content) declared() map[string]bool {
	names := map[string]bool{}
	add := func(e ast.Expr) {
		if id, ok := e.(*ast.Ident); ok {
			names[id.Name] = true
		}
	}
	ast.Inspect(c.body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if n.Tok == token.DEFINE {
				for _, l := range n.Lhs {
					add(l)
				}
			}
		case *ast.ValueSpec:
			for _, id := range n.Names {
				add(id)
			}
		case *ast.TypeSpec:
			add(n.Name)
		case *ast.Field:
			for _, id := range n.Names {
				add(id)
			}
		case *ast.RangeStmt:
			if n.Tok == token.DEFINE {
				add(n.Key)
				if n.Value != nil {
					add(n.Value)
				}
			}
		case *ast.LabeledStmt:
			add(n.Label)
		}
		return true
	})
	return names
}

// packageRefs returns the names the content uses as selector operands
// that it does not declare, which may be packages.
func (c *content) packageRefs() []string {
	declared := c.declared()
	seen := map[string]bool{}
	var out []string
	ast.Inspect(c.body, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && !declared[id.Name] && !seen[id.Name] {
				seen[id.Name] = true
				out = append(out, id.Name)
			}
		}
		return true
	})
	return out
}

// addImports imports the packages the content names that are not visible
// at pos, and returns their paths.
func addImports(ws *workspace.Workspace, pkg *packages.Package, file *ast.File, adder *imports.Adder, pos token.Pos, c *content, chosen []string) ([]string, error) {
	scope := pkg.Types.Scope().Innermost(pos)
	var added []string
	for _, name := range c.packageRefs() {
		if scope != nil {
			if _, o := scope.LookupParent(name, pos); o != nil {
				continue // a package already imported, or something else in scope
			}
		}
		path, err := resolve(ws, pkg, name, chosen)
		if err != nil {
			return nil, err
		}
		got, err := adder.Name(types.NewPackage(path, name))
		if err != nil {
			return nil, refactor.Refusef("cannot import %s here: %v", path, err)
		}
		if got != name {
			return nil, refactor.Refusef("the content uses %s for %s, but that name is taken in the file", name, path)
		}
		added = append(added, path)
	}
	return added, nil
}

// resolve finds the package the content means by name: one of chosen,
// else what another file of the package imports as name, else a workspace
// package of that name, else a standard library one; it must be unique.
func resolve(ws *workspace.Workspace, pkg *packages.Package, name string, chosen []string) (string, error) {
	for _, p := range chosen {
		if importName(p) == name {
			return p, nil
		}
	}
	try := []func() []string{
		func() []string { // another file of the package
			set := map[string]bool{}
			for _, p := range ws.Pkgs {
				if p.PkgPath != pkg.PkgPath || p.TypesInfo == nil {
					continue
				}
				for _, o := range p.TypesInfo.Defs {
					if pn, ok := o.(*types.PkgName); ok && pn.Name() == name {
						set[pn.Imported().Path()] = true
					}
				}
				for _, o := range p.TypesInfo.Implicits {
					if pn, ok := o.(*types.PkgName); ok && pn.Name() == name {
						set[pn.Imported().Path()] = true
					}
				}
			}
			return keys(set)
		},
		func() []string { // a workspace package
			set := map[string]bool{}
			for _, p := range ws.Pkgs {
				if p.Name == name && p.PkgPath != pkg.PkgPath && !strings.HasSuffix(p.PkgPath, "_test") {
					set[p.PkgPath] = true
				}
			}
			return keys(set)
		},
		func() []string { return stdPackages()[name] },
	}
	for _, f := range try {
		switch paths := f(); len(paths) {
		case 0:
		case 1:
			return paths[0], nil
		default:
			return "", refactor.Refusef("%s could be %s; name the one to import in imports", name, joinOr(paths))
		}
	}
	return "", refactor.Refusef("the content uses %s, which is not in scope there and no package of that name was found; declare it, or name the package to import in imports", name)
}

var versionSuffix = regexp.MustCompile(`^v[0-9]+$`)

// importName is the name a package at path is usually imported as.
func importName(path string) string {
	base := imports.PathBase(path)
	if versionSuffix.MatchString(base) {
		return imports.PathBase(strings.TrimSuffix(path, "/"+base))
	}
	return base
}

var (
	stdOnce sync.Once
	stdMap  map[string][]string
)

// stdPackages maps package names to the standard library's import paths.
func stdPackages() map[string][]string {
	stdOnce.Do(func() {
		stdMap = map[string][]string{}
		root := filepath.Join(build.Default.GOROOT, "src")
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			name := d.Name()
			rel, _ := filepath.Rel(root, p)
			if rel != "." && (name == "internal" || name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || rel == "cmd") {
				return filepath.SkipDir
			}
			if rel == "." || !hasGoFiles(p) {
				return nil
			}
			path := filepath.ToSlash(rel)
			stdMap[importName(path)] = append(stdMap[importName(path)], path)
			return nil
		})
		for _, ps := range stdMap {
			sort.Strings(ps)
		}
	})
	return stdMap
}

func hasGoFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if n := e.Name(); strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") {
			return true
		}
	}
	return false
}

// removeImports removes the file's imports that only the replaced range
// [from, to) used and the content does not, and returns their paths.
func removeImports(pkg *packages.Package, file *ast.File, adder *imports.Adder, from, to token.Pos, c *content) []string {
	refs := map[string]bool{}
	for _, n := range c.packageRefs() {
		refs[n] = true
	}
	var removed []string
	for _, imp := range file.Imports {
		pn := pkgNameOf(pkg.TypesInfo, imp)
		if pn == nil || refs[pn.Name()] {
			continue
		}
		inside, outside := false, false
		for id, o := range pkg.TypesInfo.Uses {
			if o != pn || id.Pos() < file.FileStart || id.Pos() >= file.FileEnd {
				continue
			}
			if from <= id.Pos() && id.Pos() < to {
				inside = true
			} else {
				outside = true
			}
		}
		if inside && !outside {
			adder.Remove(imp)
			removed = append(removed, pn.Imported().Path())
		}
	}
	return removed
}

func pkgNameOf(info *types.Info, imp *ast.ImportSpec) *types.PkgName {
	var o types.Object
	if imp.Name != nil {
		o = info.Defs[imp.Name]
	} else {
		o = info.Implicits[imp]
	}
	pn, _ := o.(*types.PkgName)
	if pn != nil && (pn.Name() == "_" || pn.Name() == ".") {
		return nil
	}
	return pn
}

// reindent removes the content's common indentation and indents each line
// with indent, unless a raw string spans lines (its text must not change).
func reindent(text, indent string, raw bool) string {
	lines := strings.Split(strings.Trim(text, "\n"), "\n")
	if raw {
		return indent + strings.TrimLeft(strings.Join(lines, "\n"), " \t")
	}
	common, first := "", true
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		lead := l[:len(l)-len(strings.TrimLeft(l, " \t"))]
		if first {
			common, first = lead, false
		}
		for !strings.HasPrefix(lead, common) {
			common = common[:len(common)-1]
		}
	}
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			lines[i] = ""
			continue
		}
		lines[i] = indent + strings.TrimPrefix(l, common)
	}
	return strings.Join(lines, "\n")
}

// indentOf returns the whitespace that starts the line at offset start.
func indentOf(src []byte, start int) string {
	i := start
	for i < len(src) && (src[i] == ' ' || src[i] == '\t') {
		i++
	}
	return string(src[start:i])
}

func keys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func joinOr(xs []string) string {
	if len(xs) <= 1 {
		return strings.Join(xs, "")
	}
	return strings.Join(xs[:len(xs)-1], ", ") + " or " + xs[len(xs)-1]
}

func quoted(paths []string) []string {
	var out []string
	for _, p := range paths {
		out = append(out, fmt.Sprintf("%q", p))
	}
	return out
}
