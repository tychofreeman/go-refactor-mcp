package addcase

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// An ifChain is an if statement with the else ifs that follow it: its
// branches, as a switch has cases.
type ifChain struct {
	ifs   []*ast.IfStmt  // the first if, then each else if
	final *ast.BlockStmt // the final else, if there is one
}

// findIfChain returns the chain with a branch that starts on line, and
// that branch's index (len(ifs) for the final else).
func findIfChain(tf *token.File, file *ast.File, line int) (c ifChain, k int, ok bool) {
	ast.Inspect(file, func(n ast.Node) bool {
		head, isIf := n.(*ast.IfStmt)
		if ok || !isIf {
			return !ok
		}
		// Inspect meets the first if of a chain before its else ifs.
		var ch ifChain
		for cur := head; ; {
			ch.ifs = append(ch.ifs, cur)
			next, more := cur.Else.(*ast.IfStmt)
			if !more {
				ch.final, _ = cur.Else.(*ast.BlockStmt)
				break
			}
			cur = next
		}
		for i, s := range ch.ifs {
			if tf.Line(s.Pos()) == line {
				c, k, ok = ch, i, true
			}
		}
		if !ok && ch.final != nil && tf.Line(ch.final.Lbrace) == line {
			c, k, ok = ch, len(ch.ifs), true
		}
		return !ok
	})
	return c, k, ok
}

// addBranches adds content, else ifs and perhaps a final else, to the
// chain c: at its end (before its else) for the line of its first if, or
// before or after branch k.
func addBranches(ws *workspace.Workspace, pkg *packages.Package, file *ast.File, tf *token.File, rel string, c ifChain, k int, where, content string, imps []string) (*refactor.Plan, error) {
	path := ws.Abs(rel)
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "if ") || strings.HasPrefix(content, "if(") {
		content = "else " + content
	}
	if !strings.HasPrefix(content, "else") {
		return nil, refactor.Refusef("content must be branches of an if: else if cond { ... }, each, and perhaps a last else { ... }")
	}
	code, err := gocode.ParseStatements("if false {\n} " + content)
	if err != nil {
		return nil, refactor.Refusef("content must be branches of an if (else if cond { ... }, or else { ... }): %v", err)
	}
	st, isIf := code.Body.List[0].(*ast.IfStmt)
	if len(code.Body.List) != 1 || !isIf {
		return nil, refactor.Refusef("content must be branches of an if only, but more code follows them")
	}
	n, final := 0, false
	for e := st.Else; e != nil; n++ {
		if next, more := e.(*ast.IfStmt); more {
			e = next.Else
		} else {
			e, final = nil, true
		}
	}

	head, last := c.ifs[0], len(c.ifs)-1
	line := tf.Line(head.Pos())
	end := func(i int) int { return tf.Offset(c.ifs[i].Body.Rbrace) + 1 }
	lastElse := func() error {
		if c.final != nil {
			return refactor.Refusef("the if on line %d already ends with an else (line %d); an else goes last, once", line, tf.Line(c.final.Lbrace))
		}
		return refactor.Refusef("an else goes last: add it at the line of the if, %d", line)
	}
	at, first := 0, false
	what := "at the end of"
	switch {
	case where == "" && k == 0:
		if final && c.final != nil {
			return nil, lastElse()
		}
		at = end(last)
	case where == "after" || where == "":
		if k == len(c.ifs) {
			return nil, refactor.Refusef("nothing comes after the final else on line %d; give where before", tf.Line(c.final.Lbrace))
		}
		if final && (k != last || c.final != nil) {
			return nil, lastElse()
		}
		at, what = end(k), fmt.Sprintf("after the branch on line %d of", tf.Line(c.ifs[k].Pos()))
	case where == "before":
		if final {
			return nil, lastElse()
		}
		branch := head.Pos()
		switch {
		case k == 0:
			at, first = tf.Offset(head.Pos()), true
		case k < len(c.ifs):
			at, branch = end(k-1), c.ifs[k].Pos()
		default:
			at, branch = end(k-1), c.final.Lbrace
		}
		what = fmt.Sprintf("before the branch on line %d of", tf.Line(branch))
	default:
		return nil, refactor.Refusef("where must be before or after, not %q", where)
	}

	adder := imports.New(ws, pkg, file, src)
	added, err := gocode.AddImports(ws, pkg, file, adder, head.Pos(), code, imps)
	if err != nil {
		return nil, err
	}
	text := strings.TrimLeft(gocode.Reindent(content, gocode.IndentOf(src, tf.Offset(tf.LineStart(line))), code.Raw), " \t")
	if first {
		// The new branch comes first: it is the if, the old first one an else if.
		text = strings.TrimSpace(strings.TrimPrefix(text, "else")) + " else "
	} else {
		text = " " + text
	}
	branches := "branch"
	if n != 1 {
		branches += "es"
	}
	plan := &refactor.Plan{
		Summary:  fmt.Sprintf("added %d %s %s the if on line %d of %s", n, branches, what, line, rel),
		Edits:    []edit.FileEdit{{Path: path, Edits: append([]edit.TextEdit{{Start: at, End: at, NewText: text}}, adder.Edits()...)}},
		Supplied: content,
	}
	if len(added) > 0 {
		plan.Notes = append(plan.Notes, gocode.ImportNote("added", added))
	}
	return plan, nil
}
