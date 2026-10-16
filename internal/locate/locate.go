// Package locate turns model-friendly positions into AST nodes.
//
// Models are unreliable with byte offsets and only moderately reliable with
// columns, so identifiers are addressed by file + line + name, with an
// optional column to disambiguate.
package locate

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/packages"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// IdentQuery addresses one identifier.
type IdentQuery struct {
	File   string // relative to the workspace root, or absolute
	Line   int    // 1-based
	Column int    // 1-based byte column; 0 means "find Name on Line"
	Name   string // expected identifier name; required when Column is 0
}

// Ident finds the identifier described by q. Failures are *refactor.Refusal
// errors with messages that help the model correct its request.
func Ident(ws *workspace.Workspace, q IdentQuery) (*packages.Package, *ast.Ident, error) {
	if q.File == "" {
		return nil, nil, refactor.Refusef("file is required")
	}
	path := ws.Abs(q.File)
	pkg, file := ws.FileSyntax(path)
	if file == nil {
		return nil, nil, refactor.Refusef("file %s is not part of the loaded packages %v", ws.Rel(path), ws.Patterns)
	}
	tf := ws.Fset.File(file.Pos())
	if q.Line < 1 || q.Line > tf.LineCount() {
		return nil, nil, refactor.Refusef("line %d out of range: %s has %d lines", q.Line, ws.Rel(path), tf.LineCount())
	}

	if q.Column > 0 {
		id := identAt(tf, file, q.Line, q.Column)
		if id == nil {
			return nil, nil, refactor.Refusef("no identifier at %s:%d:%d%s", ws.Rel(path), q.Line, q.Column, hint(tf, file, q.Line, q.Name))
		}
		if q.Name != "" && id.Name != q.Name {
			return nil, nil, refactor.Refusef("identifier at %s:%d:%d is %q, not %q%s",
				ws.Rel(path), q.Line, q.Column, id.Name, q.Name, hint(tf, file, q.Line, q.Name))
		}
		return pkg, id, nil
	}

	if q.Name == "" {
		return nil, nil, refactor.Refusef("give either column or name to identify the symbol on line %d", q.Line)
	}
	matches := identsOnLine(tf, file, q.Line, q.Name)
	if len(matches) == 0 {
		return nil, nil, refactor.Refusef("no identifier %q on %s:%d", q.Name, ws.Rel(path), q.Line)
	}
	// Several occurrences are fine if they all denote the same object
	// (e.g. `x = x + 1`); otherwise ask for a column.
	first := objectOf(pkg.TypesInfo, matches[0])
	for _, m := range matches[1:] {
		if objectOf(pkg.TypesInfo, m) != first {
			var cols []int
			for _, m := range matches {
				cols = append(cols, tf.Position(m.Pos()).Column)
			}
			return nil, nil, refactor.Refusef("%q appears %d times on %s:%d referring to different symbols (columns %v); pass column to choose",
				q.Name, len(matches), ws.Rel(path), q.Line, cols)
		}
	}
	return pkg, matches[0], nil
}

func objectOf(info *types.Info, id *ast.Ident) types.Object {
	if info == nil {
		return nil
	}
	return info.ObjectOf(id)
}

func identAt(tf *token.File, file *ast.File, line, col int) *ast.Ident {
	var found *ast.Ident
	ast.Inspect(file, func(n ast.Node) bool {
		if found != nil || n == nil {
			return false
		}
		start, end := tf.Position(n.Pos()), tf.Position(n.End())
		if start.Line > line || end.Line < line {
			return false // does not cover the line
		}
		if id, ok := n.(*ast.Ident); ok && start.Line == line && start.Column <= col && col < end.Column {
			found = id
			return false
		}
		return true
	})
	return found
}

func identsOnLine(tf *token.File, file *ast.File, line int, name string) []*ast.Ident {
	var out []*ast.Ident
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		if tf.Position(n.Pos()).Line > line || tf.Position(n.End()).Line < line {
			return false
		}
		if id, ok := n.(*ast.Ident); ok && id.Name == name && tf.Position(id.Pos()).Line == line {
			out = append(out, id)
		}
		return true
	})
	return out
}

func hint(tf *token.File, file *ast.File, line int, name string) string {
	if name == "" {
		return ""
	}
	ids := identsOnLine(tf, file, line, name)
	if len(ids) == 0 {
		return ""
	}
	return fmt.Sprintf(" (%q is at column %d)", name, tf.Position(ids[0].Pos()).Column)
}
