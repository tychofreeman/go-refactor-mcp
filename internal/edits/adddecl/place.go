package adddecl

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
)

// recvTypeName returns the name of the type a method's receiver is (T for
// t T, *T and *T[P]).
func recvTypeName(fd *ast.FuncDecl) string {
	e := fd.Recv.List[0].Type
	for {
		switch x := ast.Unparen(e).(type) {
		case *ast.StarExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.IndexListExpr:
			e = x.X
		case *ast.Ident:
			return x.Name
		default:
			return ""
		}
	}
}

// places returns the insertions that put content's declarations in file:
// a function or method that replaces a stub of the file goes where the
// stub was, a method after the last method of its type in the file or
// else after the type, and anything else at the end. Declarations that go
// to one place stay together, in their order.
func places(tf *token.File, file *ast.File, src []byte, content string) ([]edit.TextEdit, error) {
	const clause = "package p\n"
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", clause+content, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	ctf := fset.File(f.Pos())
	lineStart := func(d ast.Decl) int {
		pos := d.Pos()
		if doc := gocode.DocOf(d); doc != nil {
			pos = doc.Pos()
		}
		return ctf.Offset(ctf.LineStart(ctf.Line(pos))) - len(clause)
	}
	var order []int
	parts := map[int][]string{}
	pres := map[int]string{}
	for i, d := range f.Decls {
		start, end := 0, len(content)
		if i > 0 {
			start = lineStart(d)
		}
		if i+1 < len(f.Decls) {
			end = lineStart(f.Decls[i+1])
		}
		at, pre := place(tf, file, src, d)
		if _, ok := parts[at]; !ok {
			order = append(order, at)
			pres[at] = pre
		}
		parts[at] = append(parts[at], strings.Trim(content[start:end], "\n"))
	}
	var edits []edit.TextEdit
	for _, at := range order {
		edits = append(edits, edit.TextEdit{Start: at, End: at, NewText: pres[at] + strings.Join(parts[at], "\n\n") + "\n"})
	}
	return edits, nil
}

// place returns the offset in src where d goes, and the text before it
// there (see places).
func place(tf *token.File, file *ast.File, src []byte, d ast.Decl) (int, string) {
	if fd, ok := d.(*ast.FuncDecl); ok {
		var after ast.Decl
		for _, fileDecl := range file.Decls {
			switch x := fileDecl.(type) {
			case *ast.FuncDecl:
				if isStub(x) && funcKey(x) == funcKey(fd) {
					end := tf.Offset(x.End())
					if end < len(src) && src[end] == '\n' {
						end++
					}
					return end, "\n"
				}
				if fd.Recv != nil && x.Recv != nil && recvTypeName(x) == recvTypeName(fd) {
					after = x
				}
			case *ast.GenDecl:
				if fd.Recv != nil && after == nil && declaresType(x, recvTypeName(fd)) {
					after = x
				}
			}
		}
		if after != nil {
			return textutil.NextLine(src, tf.Offset(after.End())), "\n"
		}
	}
	if len(src) > 0 && src[len(src)-1] != '\n' {
		return len(src), "\n\n"
	}
	return len(src), "\n"
}

// declaresType reports whether g declares the type name.
func declaresType(g *ast.GenDecl, name string) bool {
	for _, s := range g.Specs {
		if ts, ok := s.(*ast.TypeSpec); ok && ts.Name.Name == name {
			return true
		}
	}
	return false
}

// placeBefore returns the insertion that puts content just before the
// declaration named: a refactoring that composes add_declaration places
// what it makes next to what it came from.
func placeBefore(tf *token.File, file *ast.File, src []byte, rel, name, content string) ([]edit.TextEdit, error) {
	d := declNamed(file, name)
	if d == nil {
		return nil, refactor.Refusef("%s has no top-level declaration %s", rel, name)
	}
	start := d.Pos()
	if doc := gocode.DocOf(d); doc != nil {
		start = doc.Pos()
	}
	at := tf.Offset(tf.LineStart(tf.Line(start)))
	return []edit.TextEdit{{Start: at, End: at, NewText: strings.Trim(content, "\n") + "\n\n"}}, nil
}

// declNamed returns the file's top-level declaration declaring name, or
// for "T.M" the method M of type T.
func declNamed(file *ast.File, name string) ast.Decl {
	if typ, method, ok := strings.Cut(name, "."); ok {
		for _, d := range file.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv != nil && fd.Name.Name == method && recvTypeName(fd) == typ {
				return fd
			}
		}
		return nil
	}
	for _, d := range file.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil && d.Name.Name == name {
				return d
			}
		case *ast.GenDecl:
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.TypeSpec:
					if s.Name.Name == name {
						return d
					}
				case *ast.ValueSpec:
					for _, n := range s.Names {
						if n.Name == name {
							return d
						}
					}
				}
			}
		}
	}
	return nil
}
