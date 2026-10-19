// Package replacestmts implements replace_statements: an edit that
// replaces whole statements of one block with new Go statements.
package replacestmts

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/scanner"
	"go/token"
	"os"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/inspect"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
	"golang.org/x/tools/go/packages"
)

// ReplaceStatements is the "replace_statements" edit.
type ReplaceStatements struct{}

// New returns the replace_statements edit.
func New() *ReplaceStatements { return &ReplaceStatements{} }

func (*ReplaceStatements) Name() string { return "replace_statements" }

func (*ReplaceStatements) Description() string {
	return `Replace whole statements of one block with new Go statements. This is an edit, not a refactoring: it may change behavior.
Use it to write a function's body (a stub's "return nil, errors.New(...)" becomes the implementation), or to change what some statements do; use the refactorings to restructure code without changing it.
Identify the statements by file, start_line and end_line (inclusive, as inspect numbers them); the lines must cover whole statements of one block, and comments and blank lines in them are replaced too. Or give function ("Name", or "Type.Method" for a method) instead of the lines to replace the whole body: also one written on the same line as the signature, which lines cannot select; a name stays valid after earlier steps of a batch. content is the new statements, without the enclosing braces; it is re-indented to the block.
Packages the new code names are imported: one another file of the package imports under that name, else a workspace package, else a standard library package of that name; when several fit (rand), list the import path to use in imports. Imports only the replaced statements used are removed.
Like every step, the result must type-check, or it is refused with the errors and nothing changes.`
}

func (*ReplaceStatements) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":       {"type": "string",  "description": "Path of the file, relative to the workspace root."},
    "function":   {"type": "string",  "description": "Instead of start_line and end_line: a function (\"Name\") or method (\"Type.Method\") of file whose whole body content replaces."},
    "start_line": {"type": "integer", "description": "1-based first line of the statements to replace."},
    "end_line":   {"type": "integer", "description": "1-based last line of the statements to replace (inclusive)."},
    "content":    {"type": "string",  "description": "The new statements, as Go source, without enclosing braces."},
    "imports":    {"type": "array", "items": {"type": "string"}, "description": "Optional: import paths to use for package names the content uses, when the name alone is ambiguous (e.g. math/rand)."}
  },
  "required": ["file", "content"],
  "additionalProperties": false
}`)
}

// ChangesBehavior marks this as an edit.
func (*ReplaceStatements) ChangesBehavior() {}

type args struct {
	File      string   `json:"file"`
	Function  string   `json:"function,omitempty"`
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
	if a.Function != "" {
		if a.StartLine != 0 || a.EndLine != 0 {
			return nil, refactor.Refusef("give either function or start_line and end_line, not both")
		}
		return replaceBody(ws, pkg, file, src, a)
	}
	tf := ws.Fset.File(file.Pos())
	if a.StartLine < 1 || a.EndLine < a.StartLine || a.EndLine > tf.LineCount() {
		return nil, refactor.Refusef("lines %d-%d are not a range of lines of %s, which has %d", a.StartLine, a.EndLine, a.File, tf.LineCount())
	}
	stmts, err := selectStatements(tf, file, src, a.StartLine, a.EndLine)
	if err != nil {
		return nil, err
	}
	content, err := gocode.ParseStatements(a.Content)
	if err != nil {
		return nil, err
	}

	start := tf.Offset(tf.LineStart(a.StartLine))
	end := len(src)
	if a.EndLine < tf.LineCount() {
		end = tf.Offset(tf.LineStart(a.EndLine + 1))
	}
	adder := imports.New(ws, pkg, file, src)
	added, err := gocode.AddImports(ws, pkg, file, adder, stmts[0].Pos(), content, a.Imports)
	if err != nil {
		return nil, err
	}
	removed := gocode.RemoveImports(pkg, file, adder, stmts[0].Pos(), stmts[len(stmts)-1].End(), content)

	text := ""
	if strings.TrimSpace(a.Content) != "" {
		text = gocode.Reindent(a.Content, gocode.IndentOf(src, tf.Offset(tf.LineStart(tf.Line(stmts[0].Pos())))), content.Raw) + "\n"
	}
	plan := &refactor.Plan{
		Summary:  fmt.Sprintf("replaced lines %d-%d (%d statement%s) of %s", a.StartLine, a.EndLine, len(stmts), textutil.Plural(len(stmts)), a.File),
		Edits:    []edit.FileEdit{{Path: path, Edits: append([]edit.TextEdit{{Start: start, End: end, NewText: text}}, adder.Edits()...)}},
		Supplied: a.Content,
	}
	if len(added) > 0 {
		plan.Notes = append(plan.Notes, gocode.ImportNote("added", added))
	}
	if len(removed) > 0 {
		plan.Notes = append(plan.Notes, gocode.ImportNote("removed", removed)+", which only the replaced statements used")
	}
	return plan, nil
}

// replaceBody replaces the whole body of the function a.Function names
// ("Name" or "Type.Method") with a.Content: also a body on the same line
// as the function's signature, which a range of lines cannot select.
func replaceBody(ws *workspace.Workspace, pkg *packages.Package, file *ast.File, src []byte, a args) (*refactor.Plan, error) {
	fd := findFunc(file, a.Function)
	if fd == nil || fd.Body == nil {
		return nil, refactor.Refusef("no function %s in %s; give Name, or Type.Method for a method", a.Function, a.File)
	}
	content, err := gocode.ParseStatements(a.Content)
	if err != nil {
		return nil, err
	}
	tf := ws.Fset.File(file.Pos())
	adder := imports.New(ws, pkg, file, src)
	added, err := gocode.AddImports(ws, pkg, file, adder, fd.Body.Lbrace, content, a.Imports)
	if err != nil {
		return nil, err
	}
	removed := gocode.RemoveImports(pkg, file, adder, fd.Body.Lbrace, fd.Body.Rbrace, content)

	indent := gocode.IndentOf(src, tf.Offset(tf.LineStart(tf.Line(fd.Pos()))))
	text := "\n"
	if strings.TrimSpace(a.Content) != "" {
		text += gocode.Reindent(a.Content, indent+"\t", content.Raw) + "\n"
	}
	text += indent
	body := edit.TextEdit{Start: tf.Offset(fd.Body.Lbrace) + 1, End: tf.Offset(fd.Body.Rbrace), NewText: text}
	plan := &refactor.Plan{
		Summary:  fmt.Sprintf("replaced the body of %s in %s", a.Function, a.File),
		Edits:    []edit.FileEdit{{Path: ws.Abs(a.File), Edits: append([]edit.TextEdit{body}, adder.Edits()...)}},
		Supplied: a.Content,
	}
	if len(added) > 0 {
		plan.Notes = append(plan.Notes, gocode.ImportNote("added", added))
	}
	if len(removed) > 0 {
		plan.Notes = append(plan.Notes, gocode.ImportNote("removed", removed)+", which only the replaced body used")
	}
	return plan, nil
}

// findFunc returns the function of file named name, or for "Type.Method"
// the method, named as inspect's outline names them.
func findFunc(file *ast.File, name string) *ast.FuncDecl {
	for _, d := range file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		n := fd.Name.Name
		if fd.Recv != nil && len(fd.Recv.List) > 0 {
			n = inspect.ReceiverTypeName(fd.Recv.List[0].Type) + "." + n
		}
		if n == name {
			return fd
		}
	}
	return nil
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
		_, clause := s.(*ast.CaseClause)
		if _, comm := s.(*ast.CommClause); comm {
			clause = true
		}
		switch {
		case b < first || a > last:
			return false
		case a >= first && b <= last && clause:
			// A whole clause is not a statement of a block: its "case x:"
			// header would be replaced too.
			if cut == nil {
				cut = s
			}
			return false
		case a >= first && b <= last:
			inside = append(inside, s)
			return false
		case a <= first && b >= last:
			// A clause ends with its last statement, so selecting its body
			// to the end does not cut it, as long as the header is left out.
			_, block := s.(*ast.BlockStmt)
			container := block || (clause && a < first)
			if !container && (a == first || b == last) && cut == nil {
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
