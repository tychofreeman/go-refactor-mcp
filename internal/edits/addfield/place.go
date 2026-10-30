package addfield

import (
	"go/ast"
	"go/token"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
)

// structOf finds the struct type declared for the type name at pos.
func structOf(file *ast.File, pos token.Pos) (*ast.StructType, bool) {
	var st *ast.StructType
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name.Pos() != pos {
			return !found
		}
		found = true
		st, _ = ts.Type.(*ast.StructType)
		return false
	})
	return st, found
}

// fieldNamed returns the field declaring name (by its embedded type's name
// for an embedded field).
func fieldNamed(st *ast.StructType, name string) *ast.Field {
	for _, f := range st.Fields.List {
		if fieldDeclares(f, name) {
			return f
		}
	}
	return nil
}

// fieldDeclares reports whether f declares name (by its embedded type's
// name for an embedded field).
func fieldDeclares(f *ast.Field, name string) bool {
	if len(f.Names) == 0 {
		return embeddedName(f.Type) == name
	}
	for _, n := range f.Names {
		if n.Name == name {
			return true
		}
	}
	return false
}

func embeddedName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return embeddedName(t.X)
	case *ast.SelectorExpr:
		return t.Sel.Name
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return embeddedName(t.X)
	case *ast.IndexListExpr:
		return embeddedName(t.X)
	}
	return ""
}

// insertEdit places the fields' text in the struct: at the end, or just
// after or before a field; a struct on one line is opened up into lines.
func insertEdit(tf *token.File, st *ast.StructType, src []byte, text, typeName, after, before string) (edit.TextEdit, error) {
	off := func(p token.Pos) int { return tf.Offset(p) }
	open, close := st.Fields.Opening, st.Fields.Closing
	declIndent := gocode.IndentOf(src, off(tf.LineStart(tf.Line(st.Pos()))))
	indent := declIndent + "\t"
	if tf.Line(open) == tf.Line(close) {
		if after != "" || before != "" {
			if name := after + before; fieldNamed(st, name) == nil {
				return edit.TextEdit{}, refactor.Refusef("%s has no field %s", typeName, name)
			}
		}
		var lines []string
		placed := false
		add := func() {
			if !placed {
				lines = append(lines, gocode.Reindent(text, indent, false))
				placed = true
			}
		}
		for _, f := range st.Fields.List {
			if before != "" && fieldDeclares(f, before) {
				add()
			}
			lines = append(lines, indent+string(src[off(f.Pos()):off(f.End())]))
			if after != "" && fieldDeclares(f, after) {
				add()
			}
		}
		add()
		return edit.TextEdit{Start: off(open), End: off(close) + 1, NewText: "{\n" + strings.Join(lines, "\n") + "\n" + declIndent + "}"}, nil
	}
	body := gocode.Reindent(text, indent, false) + "\n"
	switch {
	case after != "":
		f := fieldNamed(st, after)
		if f == nil {
			return edit.TextEdit{}, refactor.Refusef("%s has no field %s", typeName, after)
		}
		at := textutil.NextLine(src, off(f.End()))
		return edit.TextEdit{Start: at, End: at, NewText: body}, nil
	case before != "":
		f := fieldNamed(st, before)
		if f == nil {
			return edit.TextEdit{}, refactor.Refusef("%s has no field %s", typeName, before)
		}
		start := f.Pos()
		if f.Doc != nil {
			start = f.Doc.Pos()
		}
		at := off(tf.LineStart(tf.Line(start)))
		return edit.TextEdit{Start: at, End: at, NewText: body}, nil
	}
	at := off(tf.LineStart(tf.Line(close)))
	return edit.TextEdit{Start: at, End: at, NewText: body}, nil
}
