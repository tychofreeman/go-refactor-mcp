package adddecl

import (
	"go/ast"
	"go/token"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
)

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

// kindOf names what declNamed looks for: a method for "T.M", otherwise a
// top-level declaration.
func kindOf(name string) string {
	if strings.Contains(name, ".") {
		return "method"
	}
	return "top-level declaration"
}

// offset finds where the new declarations go and the text around them:
// at the end of the file, or just after or before the declaration named.
func offset(tf *token.File, file *ast.File, src []byte, rel, after, before string) (at int, pre, post string, err error) {
	switch {
	case after != "" && before != "":
		return 0, "", "", refactor.Refusef("give after or before, not both")
	case after != "":
		d := declNamed(file, after)
		if d == nil {
			return 0, "", "", refactor.Refusef("%s has no %s %s", rel, kindOf(after), after)
		}
		return nextLine(src, tf.Offset(d.End())), "\n", "", nil
	case before != "":
		d := declNamed(file, before)
		if d == nil {
			return 0, "", "", refactor.Refusef("%s has no %s %s", rel, kindOf(before), before)
		}
		start := d.Pos()
		if doc := docOf(d); doc != nil {
			start = doc.Pos()
		}
		return tf.Offset(tf.LineStart(tf.Line(start))), "", "\n", nil
	}
	if len(src) > 0 && src[len(src)-1] != '\n' {
		return len(src), "\n\n", "", nil
	}
	return len(src), "\n", "", nil
}

func docOf(d ast.Decl) *ast.CommentGroup {
	switch d := d.(type) {
	case *ast.FuncDecl:
		return d.Doc
	case *ast.GenDecl:
		return d.Doc
	}
	return nil
}

// nextLine returns the offset just past the newline that ends off's line.
func nextLine(src []byte, off int) int {
	if i := strings.IndexByte(string(src[off:]), '\n'); i >= 0 {
		return off + i + 1
	}
	return len(src)
}
