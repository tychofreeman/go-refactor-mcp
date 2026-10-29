package replacestmts

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// hole is the line of wrap's template where the statements go.
const hole = "$body"

// placeholder stands for the hole when the template is parsed: an empty
// block, as long as the hole so that offsets agree.
const placeholder = "{   }"

// wrapStatements puts stmts, lines a.StartLine-a.EndLine, as they are at
// the line $body of a.Wrap, a template of statements around them: an if,
// a loop, a closure passed to a call.
func wrapStatements(ws *workspace.Workspace, pkg *packages.Package, file *ast.File, src []byte, a args, stmts []ast.Stmt) (*refactor.Plan, error) {
	lines := strings.Split(strings.Trim(a.Wrap, "\n"), "\n")
	at := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, hole) })
	if strings.Count(a.Wrap, hole) != 1 || strings.TrimSpace(lines[at]) != hole {
		return nil, refactor.Refusef("wrap must have %s on a line of its own, once, where the statements go: \"if ok {\\n%s\\n}\"", hole, hole)
	}
	code, err := gocode.ParseStatements(strings.Replace(a.Wrap, hole, placeholder, 1))
	if err != nil {
		return nil, err
	}
	if err := checkUsedAfter(ws, pkg, stmts); err != nil {
		return nil, err
	}
	tf := ws.Fset.File(file.Pos())
	start := tf.Offset(tf.LineStart(a.StartLine))
	end := len(src)
	if a.EndLine < tf.LineCount() {
		end = tf.Offset(tf.LineStart(a.EndLine + 1))
	}
	indent := gocode.IndentOf(src, tf.Offset(tf.LineStart(tf.Line(stmts[0].Pos()))))
	var out []string
	for _, l := range strings.Split(gocode.Reindent(a.Wrap, indent, code.Raw), "\n") {
		if strings.TrimSpace(l) != hole {
			out = append(out, l)
			continue
		}
		inner := l[:len(l)-len(strings.TrimLeft(l, " \t"))]
		out = append(out, strings.TrimSuffix(textutil.Reindent(string(src[start:end]), indent, inner), "\n"))
	}
	adder := imports.New(ws, pkg, file, src)
	added, err := gocode.AddImports(ws, pkg, file, adder, stmts[0].Pos(), code, a.Imports)
	if err != nil {
		return nil, err
	}
	plan := &refactor.Plan{
		Summary: fmt.Sprintf("wrapped lines %d-%d (%d statement%s) of %s", a.StartLine, a.EndLine, len(stmts), textutil.Plural(len(stmts)), a.File),
		Edits:   []edit.FileEdit{{Path: ws.Abs(a.File), Edits: append([]edit.TextEdit{{Start: start, End: end, NewText: strings.Join(out, "\n") + "\n"}}, adder.Edits()...)}},
		Notes:   wrapNotes(ws, code, stmts),
	}
	if len(added) > 0 {
		plan.Notes = append(plan.Notes, gocode.ImportNote("added", added))
	}
	return plan, nil
}

// checkUsedAfter refuses statements that declare a name used after them,
// which, wrapped, would be out of scope there.
func checkUsedAfter(ws *workspace.Workspace, pkg *packages.Package, stmts []ast.Stmt) error {
	from, to := stmts[0].Pos(), stmts[len(stmts)-1].End()
	declared := map[types.Object]bool{}
	for id, o := range pkg.TypesInfo.Defs {
		if o != nil && from <= id.Pos() && id.Pos() < to {
			declared[o] = true
		}
	}
	var first *ast.Ident
	for id, o := range pkg.TypesInfo.Uses {
		if declared[o] && id.Pos() >= to && (first == nil || id.Pos() < first.Pos()) {
			first = id
		}
	}
	if first != nil {
		return refactor.Refusef("%s is declared in the statements and used after them (at %s); wrapped, it would be out of scope there, so declare it before them first", first.Name, ws.Position(first.Pos()))
	}
	return nil
}

// wrapNotes says what the statements' return, defer, break and continue
// do once wrapped: inside a function literal they act on the literal,
// inside a loop or switch on that.
func wrapNotes(ws *workspace.Workspace, code *gocode.Code, stmts []ast.Stmt) []string {
	var outer []ast.Node // the template's nodes around the hole, outermost first
	var stack []ast.Node
	ast.Inspect(code.Body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if b, ok := n.(*ast.BlockStmt); ok && outer == nil && len(b.List) == 0 && int(b.Rbrace-b.Lbrace) == len(placeholder)-1 {
			outer = slices.Clone(stack)
		}
		stack = append(stack, n)
		return true
	})
	closure, loop, sw := false, false, false
	for _, n := range outer {
		switch n.(type) {
		case *ast.FuncLit:
			closure, loop, sw = true, false, false
		case *ast.ForStmt, *ast.RangeStmt:
			loop = true
		case *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
			sw = true
		}
	}
	var notes []string
	note := func(n ast.Node, what string) {
		notes = append(notes, fmt.Sprintf("%s at line %d %s", what, ws.Fset.Position(n.Pos()).Line, map[string]string{
			"return":   "now leaves the function literal, not the function",
			"defer":    "now runs when the function literal returns",
			"break":    "now leaves the new loop or switch",
			"continue": "now continues the new loop",
		}[what]))
	}
	var visit func(n ast.Node, inLoop, inSwitch bool) bool
	visit = func(n ast.Node, inLoop, inSwitch bool) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ForStmt, *ast.RangeStmt:
			ast.Inspect(n, func(c ast.Node) bool { return c == n || c == nil || visit(c, true, inSwitch) })
			return false
		case *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
			ast.Inspect(n, func(c ast.Node) bool { return c == n || c == nil || visit(c, inLoop, true) })
			return false
		case *ast.ReturnStmt:
			if closure {
				note(n, "return")
			}
		case *ast.DeferStmt:
			if closure {
				note(n, "defer")
			}
		case *ast.BranchStmt:
			switch {
			case n.Label != nil:
			case n.Tok == token.BREAK && !inLoop && !inSwitch && (loop || sw):
				note(n, "break")
			case n.Tok == token.CONTINUE && !inLoop && loop:
				note(n, "continue")
			}
		}
		return true
	}
	for _, s := range stmts {
		ast.Inspect(s, func(c ast.Node) bool { return c == nil || visit(c, false, false) })
	}
	return notes
}
