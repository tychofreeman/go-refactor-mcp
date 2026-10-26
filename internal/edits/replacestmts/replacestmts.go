// Package replacestmts implements replace_statements: an edit that
// replaces whole statements of one block with new Go statements.
package replacestmts

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/scanner"
	"go/token"
	"go/types"
	"os"
	"slices"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
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
	return `Replace whole statements of one block with new Go statements: an edit, to write a function's body (a stub becomes the implementation) or change what statements do. With function "Cart.Total" and content return len(c.Items), Total's whole body becomes that statement.
Give start_line and end_line (inclusive, as inspect numbers them) covering whole statements of one block, comments and blank lines included; or function for the whole body (even one on the signature's line), which stays valid after earlier steps. content is re-indented to the block. Empty content over whole case clauses deletes them.
Packages content names are imported (imports picks one when several fit, such as rand); imports only the replaced statements used are removed.`
}

func (*ReplaceStatements) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":       {"type": "string",  "description": "Relative to the workspace root."},
    "function":   {"type": "string",  "description": "Instead of the lines: the function (Name or Type.Method) whose body to replace."},
    "case":       {"type": "string",  "description": "With function: that case's body, by value (\"\\\"fern\\\"\") or default."},
    "start_line": {"type": "integer", "description": "First line (1-based)."},
    "end_line":   {"type": "integer", "description": "Last line (inclusive)."},
    "content":    {"type": "string",  "description": "The new statements."},
    "imports":    {"type": "array", "items": {"type": "string"}, "description": "Optional: import paths for ambiguous names (math/rand)."}
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
	Case      string   `json:"case,omitempty"`
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
	if a.Function != "" && (a.StartLine != 0 || a.EndLine != 0) {
		return nil, refactor.Refusef("give either function or start_line and end_line, not both")
	}
	tf := ws.Fset.File(file.Pos())
	switch {
	case a.Case != "" && a.Function == "":
		return nil, refactor.Refusef("give function with case: the function with the switch")
	case a.Case != "":
		fd := locate.Func(file, a.Function)
		if fd == nil || fd.Body == nil {
			return nil, refactor.Refusef("no function %s in %s; give Name, or Type.Method for a method", a.Function, a.File)
		}
		if a.StartLine, a.EndLine, err = locate.CaseBody(tf, src, fd, a.Case, false); err != nil {
			return nil, err
		}
	case a.Function != "":
		return replaceBody(ws, pkg, file, src, a)
	}
	if a.StartLine < 1 || a.EndLine < a.StartLine || a.EndLine > tf.LineCount() {
		return nil, refactor.Refusef("lines %d-%d are not a range of lines of %s, which has %d", a.StartLine, a.EndLine, a.File, tf.LineCount())
	}
	if strings.TrimSpace(a.Content) == "" {
		if err := checkLive(ws, pkg, file, a.StartLine, a.EndLine); err != nil {
			return nil, err
		}
		if clauses, list := caseClauses(tf, file, src, a.StartLine, a.EndLine); clauses != nil {
			return deleteClauses(ws, pkg, file, src, a, clauses, list)
		}
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
	removed := gocode.RemoveImports(pkg, file, adder, gocode.Between(stmts[0].Pos(), stmts[len(stmts)-1].End()), content)

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
	fd := locate.Func(file, a.Function)
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
	removed := gocode.RemoveImports(pkg, file, adder, gocode.Between(fd.Body.Lbrace, fd.Body.Rbrace), content)

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

// caseClauses returns the clauses of one switch or select that lines
// first-last hold whole, with nothing else on them but comments, and the
// list of clauses they are in; nil if the lines hold anything else.
func caseClauses(tf *token.File, file *ast.File, src []byte, first, last int) (clauses, list []ast.Stmt) {
	line := func(p token.Pos) int { return tf.Line(p) }
	ast.Inspect(file, func(n ast.Node) bool {
		var body *ast.BlockStmt
		switch n := n.(type) {
		case *ast.SwitchStmt:
			body = n.Body
		case *ast.TypeSwitchStmt:
			body = n.Body
		case *ast.SelectStmt:
			body = n.Body
		}
		if body == nil || clauses != nil {
			return clauses == nil
		}
		for _, s := range body.List {
			if line(s.Pos()) >= first && line(s.End()) <= last {
				clauses = append(clauses, s)
			}
		}
		if clauses != nil {
			list = body.List
		}
		return clauses == nil
	})
	if clauses == nil {
		return nil, nil
	}
	end := len(src)
	if last < tf.LineCount() {
		end = tf.Offset(tf.LineStart(last + 1))
	}
	pos := tf.Offset(tf.LineStart(first))
	for _, s := range clauses {
		if onlyComments(src[pos:tf.Offset(s.Pos())], first) != nil {
			return nil, nil
		}
		pos = tf.Offset(s.End())
	}
	if onlyComments(src[pos:end], first) != nil {
		return nil, nil
	}
	return clauses, list
}

// checkLive refuses emptying or deleting, with lines first-last, a case
// of a switch on a struct field that the field may still be given a
// value of: the case is then not dead, and what it does would change.
func checkLive(ws *workspace.Workspace, pkg *packages.Package, file *ast.File, first, last int) error {
	tf := ws.Fset.File(file.Pos())
	line := func(p token.Pos) int { return tf.Line(p) }
	var err error
	ast.Inspect(file, func(n ast.Node) bool {
		sw, ok := n.(*ast.SwitchStmt)
		if !ok || err != nil {
			return err == nil
		}
		sel, ok := ast.Unparen(sw.Tag).(*ast.SelectorExpr)
		if !ok {
			return true
		}
		s := pkg.TypesInfo.Selections[sel]
		if s == nil || s.Kind() != types.FieldVal {
			return true
		}
		var writes []analysis.Write
		for _, st := range sw.Body.List {
			cc := st.(*ast.CaseClause)
			whole := line(cc.Pos()) >= first && line(cc.End()) <= last
			body := len(cc.Body) > 0 && line(cc.Body[0].Pos()) >= first && line(cc.Body[len(cc.Body)-1].End()) <= last
			if !whole && !body {
				continue
			}
			if writes == nil {
				writes = analysis.FieldWrites(ws.Fset, ws.Pkgs, s.Obj().(*types.Var))
			}
			for _, v := range cc.List {
				tv := pkg.TypesInfo.Types[v]
				if tv.Value == nil {
					continue
				}
				var may []string
				for _, w := range writes {
					if w.MayEqual(tv.Value, tv.Type) {
						may = append(may, ws.Position(w.Pos)+": "+describeWrite(w))
					}
				}
				if len(may) > 0 {
					field := types.TypeString(s.Recv(), types.RelativeTo(pkg.Types))
					err = refactor.Refusef("the case %s on line %d may still run: %s.%s is given values that can equal it, so emptying or deleting it changes what they do:\n  %s\nGive each of them another value first (narrowing the field then deletes the cases no value can match), or give the statements the case should run instead.", types.ExprString(v), line(cc.Pos()), strings.TrimPrefix(field, "*"), sel.Sel.Name, strings.Join(may, "\n  "))
					return false
				}
			}
		}
		return true
	})
	return err
}

// describeWrite says what w writes: the value, with its type unless it
// is a constant, or how it is written.
func describeWrite(w analysis.Write) string {
	if w.Value == nil {
		return w.Why
	}
	if tv := w.Info.Types[w.Value]; tv.Value == nil {
		return types.ExprString(w.Value) + ", of type " + types.TypeString(tv.Type, nil)
	}
	return types.ExprString(w.Value)
}

// deleteClauses deletes lines a.StartLine-a.EndLine, which hold clauses,
// whole clauses of list, unless the clause before them falls through
// into them.
func deleteClauses(ws *workspace.Workspace, pkg *packages.Package, file *ast.File, src []byte, a args, clauses, list []ast.Stmt) (*refactor.Plan, error) {
	tf := ws.Fset.File(file.Pos())
	if i := slices.Index(list, clauses[0]); i > 0 {
		if cc, ok := list[i-1].(*ast.CaseClause); ok && len(cc.Body) > 0 {
			if b, ok := cc.Body[len(cc.Body)-1].(*ast.BranchStmt); ok && b.Tok == token.FALLTHROUGH {
				return nil, refactor.Refusef("the case on line %d falls through into the case on line %d, so deleting it would change what the first one runs", tf.Line(cc.Pos()), tf.Line(clauses[0].Pos()))
			}
		}
	}
	end := len(src)
	if a.EndLine < tf.LineCount() {
		end = tf.Offset(tf.LineStart(a.EndLine + 1))
	}
	none, err := gocode.ParseStatements("")
	if err != nil {
		return nil, err
	}
	adder := imports.New(ws, pkg, file, src)
	removed := gocode.RemoveImports(pkg, file, adder, gocode.Between(clauses[0].Pos(), clauses[len(clauses)-1].End()), none)
	path := ws.Abs(a.File)
	plan := &refactor.Plan{
		Summary: fmt.Sprintf("deleted lines %d-%d (%d case clause%s) of %s", a.StartLine, a.EndLine, len(clauses), textutil.Plural(len(clauses)), a.File),
		Edits:   []edit.FileEdit{{Path: path, Edits: append([]edit.TextEdit{{Start: tf.Offset(tf.LineStart(a.StartLine)), End: end}}, adder.Edits()...)}},
	}
	if len(removed) > 0 {
		plan.Notes = append(plan.Notes, gocode.ImportNote("removed", removed)+", which only the deleted clauses used")
	}
	return plan, nil
}

// selectStatements returns the statements lines first-last cover: whole
// statements of one block, with nothing else on those lines but comments.
func selectStatements(tf *token.File, file *ast.File, src []byte, first, last int) ([]ast.Stmt, error) {
	if s := locate.SignatureAt(tf, file, first, last); s != nil {
		return nil, signatureRefusal(s)
	}
	line := func(p token.Pos) int { return tf.Line(p) }
	cut, inside := findCut(file, line, first, last)
	if cut != nil {
		what := analysis.Describe(cut)
		what = "the " + what[strings.IndexByte(what, ' ')+1:] // "an if statement" -> "the if statement"
		return nil, refactor.Refusef("lines %d-%d cut %s on lines %d-%d; select whole statements%s", first, last, what, line(cut.Pos()), line(cut.End()), wholeRange(file, line, first, last))
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

// signatureRefusal refuses lines that start on the signature of s's
// function, pointing to function, which replaces the whole body.
func signatureRefusal(s *locate.Signature) error {
	if s.First == 0 {
		return refactor.Refusef("%s; give function %q to replace its body", s.Text, s.Name)
	}
	return refactor.Refusef("%s; give function %q to replace its whole body, or start_line %d, end_line %d for its statements", s.Text, s.Name, s.First, s.Last)
}

// wholeRange suggests, for a refusal of lines first-last that cut a
// statement, the lines of whole statements of one block that cover them:
// the range grown to each statement it cuts, or, for a whole case clause,
// the clause's statements. It is "" if there are none.
func wholeRange(file *ast.File, line func(token.Pos) int, first, last int) string {
	for {
		cut, inside := findCut(file, line, first, last)
		if cut == nil {
			if len(inside) == 0 {
				return ""
			}
			for _, s := range inside[1:] {
				if listOf(file, s) != listOf(file, inside[0]) {
					return ""
				}
			}
			return fmt.Sprintf(", e.g. start_line %d, end_line %d", first, last)
		}
		a, b := line(cut.Pos()), line(cut.End())
		if a >= first && b <= last {
			// Only a whole clause is cut while within the lines.
			var body []ast.Stmt
			switch c := cut.(type) {
			case *ast.CaseClause:
				body = c.Body
			case *ast.CommClause:
				body = c.Body
			}
			if a != first || b != last || len(body) == 0 {
				return ""
			}
			return fmt.Sprintf(", e.g. start_line %d, end_line %d (the clause's statements; its case header cannot be replaced, but empty content deletes the whole clause)", line(body[0].Pos()), line(body[len(body)-1].End()))
		}
		first, last = min(first, a), max(last, b)
	}
}

// findCut returns the first statement lines first-last cut, if any, and
// the outermost statements within them.
func findCut(file *ast.File, line func(p token.Pos) int, first int, last int) (ast.Stmt, []ast.Stmt) {
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
	return cut, inside
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
