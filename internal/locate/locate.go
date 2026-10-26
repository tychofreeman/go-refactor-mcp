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
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/tychofreeman/go-refactor-mcp/internal/inspect"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// IdentQuery addresses one identifier.
type IdentQuery struct {
	File   string // relative to the workspace root, or absolute
	Line   int    // 1-based; 0 means Name is a package-level symbol
	Column int    // 1-based byte column; 0 means "find Name on Line"
	Name   string // expected identifier name; required when Column is 0

	// Function, with Line 0, makes Name a local of that function of File
	// ("Name" or "Type.Method"): a variable, constant, parameter or
	// result declared in it.
	Function string
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
	if q.Line == 0 && q.Column == 0 && q.Function != "" {
		return local(ws, pkg, file, q)
	}
	if q.Line == 0 && q.Column == 0 {
		return symbol(pkg, q.Name)
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

// symbol finds the declaring identifier of a package-level name in pkg,
// or of a field or method written Type.Name.
func symbol(pkg *packages.Package, name string) (*packages.Package, *ast.Ident, error) {
	if name == "" {
		return nil, nil, refactor.Refusef("give name: a package-level symbol (Name, or Type.Method or Type.Field), or with line, the identifier on that line")
	}
	parts := strings.Split(name, ".")
	if len(parts) > 2 {
		return nil, nil, refactor.Refusef("%q is not a symbol: write Name, or Type.Name for a field or method", name)
	}
	obj := pkg.Types.Scope().Lookup(parts[0])
	if obj == nil {
		return nil, nil, refactor.Refusef("no package-level %q in package %s; for a local, give function, or the line where it is declared or used", parts[0], pkg.Types.Name())
	}
	if len(parts) == 2 {
		tn, ok := obj.(*types.TypeName)
		if !ok {
			return nil, nil, refactor.Refusef("%s is not a type, so %q names no field or method", parts[0], name)
		}
		member, index, _ := types.LookupFieldOrMethod(tn.Type(), true, pkg.Types, parts[1])
		if member == nil || len(index) != 1 {
			return nil, nil, refactor.Refusef("%s has no field or method %q declared on it", parts[0], parts[1])
		}
		obj = member
	}
	for id, def := range pkg.TypesInfo.Defs {
		if def == obj {
			return pkg, id, nil
		}
	}
	return nil, nil, refactor.Refusef("the declaration of %q is not in the loaded source", name)
}

// local finds the declaration of q.Name, a local of the function
// q.Function of file: a variable, constant, parameter or result; or,
// when it declares none of that name, the function itself, if q.Name
// names it (Total in Total, Run or T.Run in T.Run).
func local(ws *workspace.Workspace, pkg *packages.Package, file *ast.File, q IdentQuery) (*packages.Package, *ast.Ident, error) {
	fd := Func(file, q.Function)
	if fd == nil {
		return nil, nil, refactor.Refusef("no function %s in %s", q.Function, ws.Rel(ws.Abs(q.File)))
	}
	var found []*ast.Ident
	ast.Inspect(fd, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == q.Name {
			switch obj := pkg.TypesInfo.Defs[id].(type) {
			case *types.Var:
				if !obj.IsField() {
					found = append(found, id)
				}
			case *types.Const:
				found = append(found, id)
			}
		}
		return true
	})
	switch len(found) {
	case 0:
		if q.Name == q.Function || q.Name == fd.Name.Name {
			return pkg, fd.Name, nil // the function itself, which declares no local of its name
		}
		return nil, nil, refactor.Refusef("%s declares no %s", q.Function, q.Name)
	case 1:
		return pkg, found[0], nil
	}
	var lines []string
	for _, id := range found {
		lines = append(lines, strconv.Itoa(ws.Fset.Position(id.Pos()).Line))
	}
	return nil, nil, refactor.Refusef("%s is declared %d times in %s (lines %s); give the line of the one you mean", q.Name, len(found), q.Function, strings.Join(lines, ", "))
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

// Func returns the function of file named name, or for "Type.Method"
// the method, named as inspect's outline names them.
func Func(file *ast.File, name string) *ast.FuncDecl {
	for _, d := range file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if FuncName(fd) == name {
			return fd
		}
	}
	return nil
}

// FuncName names fd as inspect's outline does: Name, or Type.Method for a
// method.
func FuncName(fd *ast.FuncDecl) string {
	n := fd.Name.Name
	if fd.Recv != nil && len(fd.Recv.List) > 0 {
		n = inspect.ReceiverTypeName(fd.Recv.List[0].Type) + "." + n
	}
	return n
}

// CaseBody returns the lines of the statements of the case of a switch
// in fd whose value is value as written ("\"book\""), or "default" for the
// default case, for a tool that addresses a range of lines.
func CaseBody(tf *token.File, src []byte, fd *ast.FuncDecl, value string) (first, last int, err error) {
	var want []string
	if value != "default" {
		if want, err = ExprTokens(value); err != nil {
			return 0, 0, err
		}
	}
	name := FuncName(fd)
	var found []*ast.CaseClause
	var cases []string
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		cc, ok := n.(*ast.CaseClause)
		if !ok {
			return true
		}
		line := tf.Line(cc.Pos())
		if cc.List == nil {
			cases = append(cases, fmt.Sprintf("default (line %d)", line))
			if want == nil {
				found = append(found, cc)
			}
		}
		for _, e := range cc.List {
			cases = append(cases, fmt.Sprintf("%s (line %d)", src[tf.Offset(e.Pos()):tf.Offset(e.End())], line))
			if want != nil && TokensMatch(tf, src, e, want) {
				found = append(found, cc)
			}
		}
		return true
	})
	switch {
	case len(found) == 0 && len(cases) == 0:
		return 0, 0, refactor.Refusef("%s has no switch", name)
	case len(found) == 0:
		// A bare word names the string case of that text: book for "book".
		if token.IsIdentifier(value) {
			// Its own refusal, unless it found no case either.
			if first, last, err := CaseBody(tf, src, fd, strconv.Quote(value)); err == nil || !strings.HasPrefix(err.Error(), "no case ") {
				return first, last, err
			}
		}
		return 0, 0, refactor.Refusef("no case %s in a switch of %s; its cases are %s", value, name, strings.Join(cases, ", "))
	case len(found) > 1:
		var lines []string
		for _, cc := range found {
			lines = append(lines, strconv.Itoa(tf.Line(cc.Pos())))
		}
		return 0, 0, refactor.Refusef("%s has a case %s on lines %s; give start_line and end_line of the one you mean", name, value, strings.Join(lines, ", "))
	}
	cc := found[0]
	line := tf.Line(cc.Pos())
	if len(cc.List) > 1 {
		var others []string
		for _, e := range cc.List {
			if !TokensMatch(tf, src, e, want) {
				others = append(others, string(src[tf.Offset(e.Pos()):tf.Offset(e.End())]))
			}
		}
		return 0, 0, refactor.Refusef("case %s on line %d shares its body with %s; give each value a case of its own first (prepare_switch_for_types splits them), or give start_line and end_line for the shared body", value, line, strings.Join(others, ", "))
	}
	if len(cc.Body) == 0 {
		return 0, 0, refactor.Refusef("case %s on line %d of %s has no statements", value, line, name)
	}
	first, last = tf.Line(cc.Body[0].Pos()), tf.Line(cc.Body[len(cc.Body)-1].End())
	if first == tf.Line(cc.Colon) {
		return 0, 0, refactor.Refusef("case %s on line %d has its statements on the case's own line; give start_line and end_line once they have lines of their own", value, line)
	}
	return first, last, nil
}

// Signature is a function whose signature a line range starts on: the
// range selects the function, not the statements of its body.
type Signature struct {
	Name string // as FuncName gives it
	// Text says so, for a refusal: "lines 5-12 include the signature of
	// T.Run on line 6".
	Text string
	// First and Last are the lines of the body's statements, 0 if it has
	// none or they share a line with a brace of the body.
	First, Last int
	Empty       bool // the body has no statements
}

// SignatureAt returns the function whose doc comment or signature holds
// line first of the range first-last, when the range reaches its func
// line, or nil if there is none.
func SignatureAt(tf *token.File, file *ast.File, first, last int) *Signature {
	line := func(p token.Pos) int { return tf.Line(p) }
	for _, d := range file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		from, to := line(fd.Pos()), line(fd.Body.Lbrace)
		start := from
		if fd.Doc != nil {
			start = line(fd.Doc.Pos())
		}
		if first < start || first > to || last < from {
			continue
		}
		s := &Signature{Name: FuncName(fd), Empty: len(fd.Body.List) == 0}
		lines := fmt.Sprintf("lines %d-%d include", first, last)
		if first == last {
			lines = fmt.Sprintf("line %d includes", first)
		}
		sig := fmt.Sprintf(" on lines %d-%d", from, to)
		if from == to {
			sig = fmt.Sprintf(" on line %d", from)
		}
		if first == last && first == from {
			sig = ""
		}
		s.Text = fmt.Sprintf("%s the signature of %s%s", lines, s.Name, sig)
		if !s.Empty {
			a, b := line(fd.Body.List[0].Pos()), line(fd.Body.List[len(fd.Body.List)-1].End())
			if a > to && b < line(fd.Body.Rbrace) {
				s.First, s.Last = a, b
			}
		}
		return s
	}
	return nil
}
