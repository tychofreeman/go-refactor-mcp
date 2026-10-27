// Package addcase implements add_case, an edit: it may change behavior.
package addcase

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"strconv"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/site"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// AddCase is the "add_case" edit.
type AddCase struct{}

// New returns the add_case edit.
func New() *AddCase { return &AddCase{} }

func (*AddCase) Name() string { return "add_case" }

func (*AddCase) Description() string {
	return `Add case clauses to a switch or type switch: an edit, to handle a new value or type. At the line of switch kind {, content case "sms": return sendSMS(to) adds that clause at the end of the switch (before a default clause that ends it); at the line of one of its case clauses, where "before" or "after" (the default) puts them next to it. content is one or more clauses, each "case x:" or "default:" with its statements, re-indented; packages it names are imported. A duplicate case is refused with the compiler's error. For the statements inside a clause, use insert_statements or replace_statements.
Branches of an if, likewise: at the line of if p.Height > 10 {, content else if p.Height > 5 { return 2 } goes at the end of its chain (before its else); at the line of an else if or else, before or after it. else { ... } goes last, once.`
}

func (*AddCase) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":    {"type": "string",  "description": "Path of the file, relative to the workspace root."},
    "line":    {"type": "integer", "description": "1-based line of the switch or if (they go at its end), or of one of its case clauses or branches."},
    "where":   {"type": "string",  "enum": ["before", "after"], "description": "For the line of a case clause or branch: before or after it (default after)."},
    "content": {"type": "string",  "description": "One or more clauses, each \"case x:\" or \"default:\" with its statements; or branches, else if c { ... } or else { ... }."},
    "imports": {"type": "array", "items": {"type": "string"}, "description": "Optional: import paths to use for package names the content uses, when the name alone is ambiguous."}
  },
  "required": ["file", "line", "content"],
  "additionalProperties": false
}`)
}

func (*AddCase) ChangesBehavior() {}

// Plan checks preconditions and computes the edits.
func (*AddCase) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a struct {
		File    string   `json:"file"`
		Line    int      `json:"line"`
		Where   string   `json:"where,omitempty"`
		Content string   `json:"content"`
		Imports []string `json:"imports,omitempty"`
	}
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	opened, err := site.At(ws, path)
	if err != nil {
		return nil, err
	}
	pkg, file, src := opened.Pkg, opened.File, opened.Src
	tf := opened.TF

	// The switch on the line, or the switch of the case clause on it.
	var sw ast.Stmt
	var body *ast.BlockStmt
	var clause *ast.CaseClause
	ast.Inspect(file, func(n ast.Node) bool {
		if sw != nil {
			return false
		}
		var b *ast.BlockStmt
		switch n := n.(type) {
		case *ast.SwitchStmt:
			b = n.Body
		case *ast.TypeSwitchStmt:
			b = n.Body
		default:
			return true
		}
		if tf.Line(n.Pos()) == a.Line {
			sw, body = n.(ast.Stmt), b
			return false
		}
		for _, s := range b.List {
			if cc := s.(*ast.CaseClause); tf.Line(cc.Pos()) == a.Line {
				sw, body, clause = n.(ast.Stmt), b, cc
				return false
			}
		}
		return true
	})
	if sw == nil {
		if c, k, ok := findIfChain(tf, file, a.Line); ok {
			return addBranches(ws, pkg, file, tf, a.File, c, k, a.Where, a.Content, a.Imports)
		}
		return nil, refactor.Refusef("no switch statement, case clause or branch of an if starts on line %d of %s", a.Line, a.File)
	}

	lineStart := func(line int) int { return tf.Offset(tf.LineStart(line)) }
	// above is where clauses go to come before the clause starting on
	// line: above the comment lines just over it too, which are about it.
	above := func(line int) int {
		for line > 1 && strings.HasPrefix(strings.TrimSpace(string(src[lineStart(line-1):lineStart(line)])), "//") {
			line--
		}
		return lineStart(line)
	}
	var at int
	what := "at the end of"
	switch {
	case clause == nil:
		if a.Where != "" {
			return nil, refactor.Refusef("where is for a line of a case clause; for the line of the switch, the clauses go at its end")
		}
		if tf.Line(body.Rbrace) == tf.Line(body.Lbrace) {
			return nil, refactor.Refusef("the switch on line %d is written on one line; add a clause to it with replace_statements", a.Line)
		}
		at = lineStart(tf.Line(body.Rbrace))
		if d := trailingDefault(body); d != nil {
			// A default clause stays last, as it is usually written.
			at, what = above(tf.Line(d.Pos())), "before the default clause of"
		}
	case a.Where == "before":
		at, what = above(a.Line), "before the clause on line "+strconv.Itoa(a.Line)+" of"
	case a.Where == "after" || a.Where == "":
		// Just after the clause's last line, so that a comment above the
		// next clause stays with it.
		at, what = lineStart(tf.Line(clause.End())+1), "after the clause on line "+strconv.Itoa(a.Line)+" of"
	default:
		return nil, refactor.Refusef("where must be before or after, not %q", a.Where)
	}

	code, err := gocode.ParseStatements("switch {\n" + a.Content + "\n}")
	if err != nil {
		return nil, refactor.Refusef("content must be case clauses (\"case x:\" or \"default:\", each with its statements): %v", err)
	}
	if len(code.Body.List) != 1 { // a "}" in the content closed the switch
		return nil, refactor.Refusef("content must be case clauses only (\"case x:\" or \"default:\", each with its statements), but a \"}\" in it closes the switch and more code follows")
	}
	n := len(code.Body.List[0].(*ast.SwitchStmt).Body.List)
	if n == 0 {
		return nil, refactor.Refusef("content has no case clauses")
	}
	adder := imports.New(ws, pkg, file, src)
	added, err := gocode.AddImports(ws, pkg, file, adder, body.Lbrace, code, a.Imports)
	if err != nil {
		return nil, err
	}
	indent := gocode.IndentOf(src, lineStart(tf.Line(sw.Pos())))
	text := gocode.Reindent(strings.Trim(a.Content, "\n"), indent, code.Raw) + "\n"
	plan := &refactor.Plan{
		Summary:  fmt.Sprintf("added %d case clause%s %s the switch on line %d of %s", n, textutil.Plural(n), what, tf.Line(sw.Pos()), a.File),
		Edits:    []edit.FileEdit{{Path: path, Edits: append([]edit.TextEdit{{Start: at, End: at, NewText: text}}, adder.Edits()...)}},
		Supplied: a.Content,
	}
	if len(added) > 0 {
		plan.Notes = append(plan.Notes, gocode.ImportNote("added", added))
	}
	return plan, nil
}

// trailingDefault is the default clause of the switch body when it is the
// last clause, or nil.
func trailingDefault(body *ast.BlockStmt) *ast.CaseClause {
	if len(body.List) == 0 {
		return nil
	}
	if c, ok := body.List[len(body.List)-1].(*ast.CaseClause); ok && c.List == nil {
		return c
	}
	return nil
}
