// Package addcase implements add_case, an edit: it may change behavior.
package addcase

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"os"
	"strconv"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// AddCase is the "add_case" edit.
type AddCase struct{}

// New returns the add_case edit.
func New() *AddCase { return &AddCase{} }

func (*AddCase) Name() string { return "add_case" }

func (*AddCase) Description() string {
	return `Add case clauses to a switch or type switch. This is an edit, not a refactoring: it may change behavior.
Use it to handle a new value or type; use insert_statements or replace_statements for the statements inside a clause.
Identify the place by file and line: the line of the switch, to add the clauses at its end, or of one of its case clauses, with where "before" or "after" it (default after). content is one or more clauses, each "case x:" or "default:" followed by its statements; it is re-indented to the switch.
Packages the new code names are imported, as replace_statements does (imports chooses when a name is ambiguous). Like every step, the result must type-check (a duplicate case is refused with the compiler's error), or nothing changes.`
}

func (*AddCase) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":    {"type": "string",  "description": "Path of the file, relative to the workspace root."},
    "line":    {"type": "integer", "description": "1-based line of the switch (the clauses go at its end), or of one of its case clauses."},
    "where":   {"type": "string",  "enum": ["before", "after"], "description": "For the line of a case clause: before or after it (default after)."},
    "content": {"type": "string",  "description": "One or more clauses, each \"case x:\" or \"default:\" with its statements."},
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
	pkg, file := ws.FileSyntax(path)
	if file == nil {
		return nil, refactor.Refusef("file %s is not part of the loaded packages", a.File)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	tf := ws.Fset.File(file.Pos())

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
		return nil, refactor.Refusef("no switch statement or case clause starts on line %d of %s", a.Line, a.File)
	}

	lineStart := func(line int) int { return tf.Offset(tf.LineStart(line)) }
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
	case a.Where == "before":
		// Before the comment lines just above the clause too: they are
		// about it.
		first := a.Line
		for first > 1 && strings.HasPrefix(strings.TrimSpace(string(src[lineStart(first-1):lineStart(first)])), "//") {
			first--
		}
		at, what = lineStart(first), "before the clause on line "+strconv.Itoa(a.Line)+" of"
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
