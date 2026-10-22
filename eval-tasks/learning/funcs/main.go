// Command funcs prints, for each function and method under a directory,
// its file, name and the lines of its body, as JSON lines.
package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	root := os.Args[1]
	fset := token.NewFileSet()
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			name := fd.Name.Name
			if fd.Recv != nil {
				t := fd.Recv.List[0].Type
				if s, ok := t.(*ast.StarExpr); ok {
					t = s.X
				}
				if id, ok := t.(*ast.Ident); ok {
					name = id.Name + "." + name
				}
			}
			json.NewEncoder(os.Stdout).Encode(map[string]any{"file": rel, "name": name,
				"start": fset.Position(fd.Pos()).Line, "end": fset.Position(fd.End()).Line,
				"body_start": fset.Position(fd.Body.Lbrace).Line})
		}
		return nil
	})
}
