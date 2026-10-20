package adddecl

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"strconv"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/inspect"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
	"golang.org/x/tools/go/packages"
)

// replacedStubs finds, for each function and method decls declare, a stub
// of it in pkg: a declaration of the same name (and receiver type) whose
// body is only panic("not implemented"), as create_from_usage writes. A
// model that writes a call first and then the function finds the stub
// already there. It returns the edits that remove the stubs, with their doc
// comments, and a note on each.
func replacedStubs(ws *workspace.Workspace, pkg *packages.Package, decls []ast.Decl) ([]edit.FileEdit, []string, error) {
	adding := map[string]bool{}
	for _, d := range decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			adding[funcKey(fd)] = true
		}
	}
	var fes []edit.FileEdit
	var notes []string
	for _, f := range pkg.Syntax {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || !adding[funcKey(fd)] || !isStub(fd) {
				continue
			}
			pos := ws.Fset.Position(fd.Pos())
			src, err := os.ReadFile(pos.Filename)
			if err != nil {
				return nil, nil, err
			}
			tf := ws.Fset.File(fd.Pos())
			first := fd.Pos()
			if fd.Doc != nil {
				first = fd.Doc.Pos()
			}
			start := tf.Offset(tf.LineStart(tf.Line(first)))
			if start >= 2 && src[start-1] == '\n' && src[start-2] == '\n' {
				start-- // the blank line before it
			}
			end := tf.Offset(fd.End())
			if end < len(src) && src[end] == '\n' {
				end++
			}
			fes = append(fes, edit.FileEdit{Path: pos.Filename, Edits: []edit.TextEdit{{Start: start, End: end}}})
			notes = append(notes, fmt.Sprintf("replaced the stub of %s (panic(\"not implemented\")) at %s:%d", funcKey(fd), ws.Rel(pos.Filename), pos.Line))
		}
	}
	return fes, notes, nil
}

// funcKey names fd as Name, or Type.Name for a method.
func funcKey(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	return inspect.ReceiverTypeName(fd.Recv.List[0].Type) + "." + fd.Name.Name
}

// isStub reports whether fd's body is only panic("not implemented").
func isStub(fd *ast.FuncDecl) bool {
	if fd.Body == nil || len(fd.Body.List) != 1 {
		return false
	}
	es, ok := fd.Body.List[0].(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := es.X.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	if id, ok := call.Fun.(*ast.Ident); !ok || id.Name != "panic" {
		return false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	s, err := strconv.Unquote(lit.Value)
	return err == nil && s == "not implemented"
}
