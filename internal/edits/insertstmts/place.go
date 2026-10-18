package insertstmts

import (
	"go/ast"
	"go/token"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
)

// place is where new statements go: inserted at offset at, each line
// indented with indent, or, for an empty block written on one line,
// replacing the braces' inside (from at to end) with lines of their own.
type place struct {
	at, end int
	indent  string
	closing string    // the indentation of the closing brace, for an expanded block
	pos     token.Pos // a position in the scope the statements run in
	what    string    // for the summary
}

// elements calls f for every statement that is an element of a statement
// list (not a case clause), outermost first.
func elements(file *ast.File, f func(ast.Stmt) bool) {
	done := false
	ast.Inspect(file, func(n ast.Node) bool {
		if done || n == nil {
			return false
		}
		var list []ast.Stmt
		switch n := n.(type) {
		case *ast.BlockStmt:
			list = n.List
		case *ast.CaseClause:
			list = n.Body
		case *ast.CommClause:
			list = n.Body
		}
		for _, s := range list {
			switch s.(type) {
			case *ast.CaseClause, *ast.CommClause:
				continue
			}
			if !f(s) {
				done = true
				return false
			}
		}
		return true
	})
}

// before finds the outermost statement starting on line.
func before(tf *token.File, file *ast.File, src []byte, line int) (*place, error) {
	var found ast.Stmt
	elements(file, func(s ast.Stmt) bool {
		if tf.Line(s.Pos()) == line {
			found = s
			return false
		}
		return true
	})
	if found == nil {
		return nil, refactor.Refusef("no statement starts on line %d", line)
	}
	start := tf.Offset(tf.LineStart(line))
	if strings.TrimSpace(string(src[start:tf.Offset(found.Pos())])) != "" {
		return nil, refactor.Refusef("line %d has code before the statement that starts there; insert after that code instead", line)
	}
	return &place{at: start, end: start, indent: gocode.IndentOf(src, start), pos: found.Pos(), what: "before the statement on line"}, nil
}

// after finds the outermost statement ending on line.
func after(tf *token.File, file *ast.File, src []byte, line int) (*place, error) {
	var found ast.Stmt
	elements(file, func(s ast.Stmt) bool {
		if tf.Line(s.End()) == line {
			found = s
			return false
		}
		return true
	})
	if found == nil {
		return nil, refactor.Refusef("no statement ends on line %d", line)
	}
	end := nextLine(src, tf.Offset(found.End()))
	if rest := strings.TrimSpace(string(src[tf.Offset(found.End()):end])); rest != "" && !strings.HasPrefix(rest, "//") {
		return nil, refactor.Refusef("line %d has code after the statement that ends there", line)
	}
	start := tf.Offset(tf.LineStart(tf.Line(found.Pos())))
	return &place{at: end, end: end, indent: gocode.IndentOf(src, start), pos: found.End(), what: "after the statement on line"}, nil
}

// into finds the block whose opening brace (or the case clause whose
// colon) is on line, and the start of its statements.
func into(tf *token.File, file *ast.File, src []byte, line int) (*place, error) {
	var open, close token.Pos
	var first ast.Stmt
	ast.Inspect(file, func(n ast.Node) bool {
		if open.IsValid() || n == nil {
			return false
		}
		switch n := n.(type) {
		case *ast.BlockStmt:
			if tf.Line(n.Lbrace) == line {
				open, close = n.Lbrace, n.Rbrace
				if len(n.List) > 0 {
					first = n.List[0]
				}
			}
		case *ast.CaseClause:
			if tf.Line(n.Colon) == line {
				open = n.Colon
				if len(n.Body) > 0 {
					first = n.Body[0]
				}
			}
		case *ast.CommClause:
			if tf.Line(n.Colon) == line {
				open = n.Colon
				if len(n.Body) > 0 {
					first = n.Body[0]
				}
			}
		}
		return true
	})
	if !open.IsValid() {
		return nil, refactor.Refusef("no block opens on line %d (with { or a case's colon)", line)
	}
	lineStart := tf.Offset(tf.LineStart(line))
	lineIndent := gocode.IndentOf(src, lineStart)
	if close.IsValid() && tf.Line(close) == line {
		if first != nil {
			return nil, refactor.Refusef("the block on line %d is written on one line with statements in it; use replace_statements on that line", line)
		}
		return &place{at: tf.Offset(open) + 1, end: tf.Offset(close), indent: lineIndent + "\t", closing: lineIndent, pos: open + 1, what: "into the block on line"}, nil
	}
	at := nextLine(src, tf.Offset(open)+1)
	if rest := strings.TrimSpace(string(src[tf.Offset(open)+1 : at])); rest != "" && !strings.HasPrefix(rest, "//") {
		return nil, refactor.Refusef("line %d has code after the opening brace; insert before that statement instead", line)
	}
	indent := lineIndent + "\t"
	if first != nil {
		indent = gocode.IndentOf(src, tf.Offset(tf.LineStart(tf.Line(first.Pos()))))
	}
	return &place{at: at, end: at, indent: indent, pos: open + 1, what: "into the block on line"}, nil
}

// text is the content as it goes in at p.
func (p *place) text(content string, raw bool) string {
	body := gocode.Reindent(content, p.indent, raw) + "\n"
	if p.closing != "" { // an empty block on one line: the statements get lines of their own
		return "\n" + body + p.closing
	}
	return body
}

// nextLine returns the offset just past the newline that ends off's line.
func nextLine(src []byte, off int) int {
	if i := strings.IndexByte(string(src[off:]), '\n'); i >= 0 {
		return off + i + 1
	}
	return len(src)
}
