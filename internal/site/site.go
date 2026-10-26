package site

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
	"golang.org/x/tools/go/packages"
)

// Package site holds where a refactoring works: a file of a package of
// the workspace, with its source, and what reads and quotes code there.

// Site is a file of the workspace a refactoring works in.
type Site struct {
	WS   *workspace.Workspace
	Pkg  *packages.Package
	Info *types.Info
	File *ast.File
	TF   *token.File
	Src  []byte
}

// Off returns the offset of p in the file's source.
func (s *Site) Off(p token.Pos) int { return s.TF.Offset(p) }

// Text returns the source of n.
func (s *Site) Text(n ast.Node) string { return string(s.Src[s.Off(n.Pos()):s.Off(n.End())]) }

// Quote returns the source of n for a message: on one line, in
// backticks, cut to about sixty characters.
func (s *Site) Quote(n ast.Node) string {
	t := strings.Join(strings.Fields(s.Text(n)), " ")
	if len(t) > 60 {
		t = t[:57] + "..."
	}
	return "`" + t + "`"
}

// Position returns file:line:column for p, relative to the workspace.
func (s *Site) Position(p token.Pos) string { return s.WS.Position(p) }

// DeclaredAt returns where o is declared, for a message.
func DeclaredAt(ws *workspace.Workspace, o types.Object) string {
	if o.Pkg() == nil || !o.Pos().IsValid() {
		return "universe scope (predeclared)"
	}
	return ws.Position(o.Pos())
}

// At returns the site of the file at path, which the workspace has
// loaded: its package (the first variant that has it), syntax, source
// and type information.
func At(ws *workspace.Workspace, path string) (*Site, error) {
	pkg, file := ws.FileSyntax(path)
	if file == nil {
		return nil, fmt.Errorf("%s is not a Go file of the workspace", ws.Rel(path))
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return &Site{WS: ws, Pkg: pkg, Info: pkg.TypesInfo, File: file, TF: ws.Fset.File(file.Pos()), Src: src}, nil
}
