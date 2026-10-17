package locate

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// Tokens returns the token sequence of src, ignoring whitespace, comments
// and automatically inserted semicolons, so that "a+b" matches "a + b".
func Tokens(src []byte) ([]string, error) {
	var s scanner.Scanner
	fset := token.NewFileSet()
	var scanErr error
	s.Init(fset.AddFile("", -1, len(src)), src, func(_ token.Position, msg string) { scanErr = fmt.Errorf("%s", msg) }, 0)
	var out []string
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.SEMICOLON && lit == "\n" {
			continue
		}
		if lit != "" && tok != token.SEMICOLON {
			out = append(out, lit)
		} else {
			out = append(out, tok.String())
		}
	}
	return out, scanErr
}

// EqualTokens reports whether a and b are the same token sequence.
func EqualTokens(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TokensMatch reports whether the source of e, in the file tf whose
// contents are src, has the tokens want.
func TokensMatch(tf *token.File, src []byte, e ast.Expr, want []string) bool {
	got, err := Tokens(src[tf.Offset(e.Pos()):tf.Offset(e.End())])
	return err == nil && EqualTokens(got, want)
}

// FindExpr finds the expression whose tokens are want, starting on line
// (at column, if not 0) of file, whose source is src. With all, the first
// of several matches is returned instead of a refusal.
func FindExpr(ws *workspace.Workspace, file *ast.File, src []byte, want []string, line, column int, all bool) (ast.Expr, error) {
	tf := ws.Fset.File(file.Pos())
	var matches []ast.Expr
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		start, end := tf.Line(n.Pos()), tf.Line(n.End())
		if start > line || end < line {
			return false
		}
		if e, ok := n.(ast.Expr); ok && start == line && TokensMatch(tf, src, e, want) {
			if column == 0 || tf.Position(e.Pos()).Column == column {
				matches = append(matches, e)
			}
		}
		return true
	})
	where := fmt.Sprintf("%s:%d", ws.Rel(tf.Name()), line)
	text := strings.Join(want, " ")
	switch {
	case len(matches) == 1:
		return matches[0], nil
	case len(matches) > 1 && all:
		return matches[0], nil
	case len(matches) > 1:
		var cols []string
		for _, m := range matches {
			cols = append(cols, fmt.Sprint(tf.Position(m.Pos()).Column))
		}
		return nil, refactor.Refusef("the expression occurs %d times on %s (columns %s); pass column to choose one, or all_occurrences to replace them all",
			len(matches), where, strings.Join(cols, ", "))
	case column != 0:
		return nil, refactor.Refusef("no expression %q starts at %s:%d%s", text, where, column, lineHint(tf, src, line))
	}
	// Not on this line: say where it is, if anywhere.
	var lines []string
	ast.Inspect(file, func(n ast.Node) bool {
		if e, ok := n.(ast.Expr); ok && len(lines) < 5 && TokensMatch(tf, src, e, want) {
			lines = append(lines, fmt.Sprint(tf.Line(e.Pos())))
		}
		return len(lines) < 5
	})
	if len(lines) > 0 {
		return nil, refactor.Refusef("no expression %q starts on line %d, but it starts on line(s) %s of that file", text, line, strings.Join(lines, ", "))
	}
	return nil, refactor.Refusef("no expression %q starts on %s%s", text, where, lineHint(tf, src, line))
}

func lineHint(tf *token.File, src []byte, line int) string {
	s := src[tf.Offset(tf.LineStart(line)):]
	if i := strings.IndexByte(string(s), '\n'); i >= 0 {
		s = s[:i]
	}
	return fmt.Sprintf("; that line is: %s", strings.TrimSpace(string(s)))
}

// ExprTokens tokenizes the expression text the model supplied, after
// checking that it parses as an expression.
func ExprTokens(text string) ([]string, error) {
	text = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(text), ";"))
	if _, err := parser.ParseExpr(text); err != nil {
		return nil, refactor.Refusef("%q is not a Go expression: %v", text, err)
	}
	return Tokens([]byte(text))
}
