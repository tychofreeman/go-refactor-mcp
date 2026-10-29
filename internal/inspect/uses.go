package inspect

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// roles are what a use does with what it names, in the order shown.
var roles = []string{"switch on it", "compared", "asserted", "a type switch case", "converted to", "made by a literal", "written", "passed as an interface", "called", "read"}

// use is one use of an object: where, and the code that shows what is
// done with it.
type use struct {
	file string
	line int
	text string
}

// uses renders, for each name in q.Uses, every use in the module of what
// it names, found by the type checker, under what is done with it.
func uses(ws *workspace.Workspace, q Query) (string, error) {
	if q.File != "" || q.Function != "" || q.StartLine != 0 || q.EndLine != 0 || len(q.Targets) > 0 || len(q.Names) > 0 {
		return "", refactor.Refusef("give uses alone: each is found in the module by its name, without file")
	}
	out := make([]string, len(q.Uses))
	for i, n := range q.Uses {
		o, desc, err := object(ws, n)
		switch {
		case refactor.IsRefusal(err):
			out[i] = fmt.Sprintf("%s: %v\n", n, err)
			continue
		case err != nil:
			return "", err
		}
		out[i] = usesOf(ws, n, desc, o)
	}
	return strings.Join(out, "\n"), nil
}

// object finds the one object of the module named name: a package-level
// Name, a field or method Type.Name, or either after its package's name;
// and describes it ("field of pkg.Type").
func object(ws *workspace.Workspace, name string) (types.Object, string, error) {
	type found struct {
		o    types.Object
		desc string
	}
	byKey := map[string]found{}
	for _, p := range ws.Pkgs {
		if p.Types == nil {
			continue
		}
		local := name
		if pkg, rest, ok := strings.Cut(name, "."); ok && pkg == p.Types.Name() {
			if o, _ := lookup(p.Types, rest); o != nil {
				local = rest
			}
		}
		o, desc := lookup(p.Types, local)
		if o == nil || !ws.Contains(ws.Fset.Position(o.Pos()).Filename) {
			continue
		}
		byKey[analysis.ObjectKey(ws.Fset, o)] = found{o, desc}
	}
	if len(byKey) == 0 {
		return nil, "", refactor.Refusef("no declaration of that name in the module")
	}
	var places []string
	for _, f := range byKey {
		if len(byKey) == 1 {
			return f.o, f.desc, nil
		}
		places = append(places, fmt.Sprintf("%s (package %s)", ws.Rel(ws.Fset.Position(f.o.Pos()).Filename), f.o.Pkg().Name()))
	}
	sort.Strings(places)
	return nil, "", refactor.Refusef("declared in %s; name it more fully (pkg.Name, Type.Method)", strings.Join(places, ", "))
}

// lookup finds Name or Type.Name in pkg, and describes it.
func lookup(pkg *types.Package, name string) (types.Object, string) {
	typ, member, ok := strings.Cut(name, ".")
	o := pkg.Scope().Lookup(typ)
	if o == nil {
		return nil, ""
	}
	if !ok {
		return o, analysis.KindOf(o) + " of package " + pkg.Name()
	}
	if _, isType := o.(*types.TypeName); !isType {
		return nil, ""
	}
	m, _, _ := types.LookupFieldOrMethod(o.Type(), true, pkg, member)
	if m == nil {
		return nil, ""
	}
	kind := "method"
	if _, isVar := m.(*types.Var); isVar {
		kind = "field"
	}
	return m, kind + " of " + pkg.Name() + "." + typ
}

// usesOf renders the uses of o, grouped by role; a use in a file that
// two variants of a package share is listed once.
func usesOf(ws *workspace.Workspace, name, desc string, o types.Object) string {
	key := analysis.ObjectKey(ws.Fset, o)
	seen := map[token.Pos]bool{}
	byRole := map[string][]use{}
	n := 0
	for _, p := range ws.Pkgs {
		for _, f := range p.Syntax {
			tf := ws.Fset.File(f.Pos())
			if tf == nil || !ws.Contains(tf.Name()) {
				continue
			}
			var stack []ast.Node
			ast.Inspect(f, func(node ast.Node) bool {
				if node == nil {
					stack = stack[:len(stack)-1]
					return false
				}
				stack = append(stack, node)
				id, ok := node.(*ast.Ident)
				if !ok || seen[id.Pos()] {
					return true
				}
				if u := p.TypesInfo.Uses[id]; u == nil || analysis.ObjectKey(ws.Fset, u) != key {
					return true
				}
				seen[id.Pos()] = true
				n++
				role, text := classify(ws.Fset, p.TypesInfo, stack)
				byRole[role] = append(byRole[role], use{ws.Rel(tf.Name()), ws.Fset.Position(id.Pos()).Line, text})
				return true
			})
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s, %s): ", name, desc, types.TypeString(o.Type(), types.RelativeTo(o.Pkg())))
	if n == 0 {
		b.WriteString("no uses\n")
		return b.String()
	}
	fmt.Fprintf(&b, "%d uses\n", n)
	for _, role := range roles {
		us := byRole[role]
		if len(us) == 0 {
			continue
		}
		sort.Slice(us, func(i, j int) bool {
			if us[i].file != us[j].file {
				return us[i].file < us[j].file
			}
			return us[i].line < us[j].line
		})
		fmt.Fprintf(&b, "%s (%d):\n", role, len(us))
		for _, u := range us {
			fmt.Fprintf(&b, "  %s:%d  %s\n", u.file, u.line, u.text)
		}
	}
	return b.String()
}

// classify says what the use at the end of stack does with what it
// names, and gives the code that shows it.
func classify(fset *token.FileSet, info *types.Info, stack []ast.Node) (string, string) {
	i := len(stack) - 1
	var e ast.Expr = stack[i].(*ast.Ident)
	if sel, ok := stack[i-1].(*ast.SelectorExpr); ok && sel.Sel == e {
		i--
		e = sel
	}
	for {
		if p, ok := stack[i-1].(*ast.ParenExpr); ok {
			i--
			e = p
			continue
		}
		break
	}
	if _, isType := info.Uses[stack[len(stack)-1].(*ast.Ident)].(*types.TypeName); isType {
		if role, text := classifyType(fset, info, stack, i, e); role != "" {
			return role, text
		}
	}
	switch parent := stack[i-1].(type) {
	case *ast.SwitchStmt:
		if parent.Tag == e {
			var values []string
			for _, s := range parent.Body.List {
				cc := s.(*ast.CaseClause)
				if cc.List == nil {
					values = append(values, "default")
				}
				for _, v := range cc.List {
					values = append(values, line(fset, v))
				}
			}
			return "switch on it", "switch " + line(fset, e) + ": " + strings.Join(values, ", ")
		}
	case *ast.BinaryExpr:
		if parent.Op == token.EQL || parent.Op == token.NEQ {
			return "compared", line(fset, parent)
		}
	case *ast.TypeAssertExpr:
		return "asserted", line(fset, parent)
	case *ast.KeyValueExpr:
		if parent.Key == e {
			return "written", line(fset, parent)
		}
	case *ast.AssignStmt:
		for _, l := range parent.Lhs {
			if l == e {
				return "written", line(fset, parent)
			}
		}
	case *ast.IncDecStmt:
		return "written", line(fset, parent)
	case *ast.CallExpr:
		if parent.Fun == e {
			return "called", line(fset, parent)
		}
		if sig, ok := info.TypeOf(parent.Fun).(*types.Signature); ok && types.IsInterface(paramType(sig, parent, e)) {
			return "passed as an interface", line(fset, parent)
		}
	}
	for j := i - 1; j >= 0; j-- {
		if s, ok := stack[j].(ast.Stmt); ok {
			return "read", line(fset, s)
		}
	}
	return "read", line(fset, e)
}

// classifyType says what the use e of a type, at stack[i], does with it
// where that is particular to types: a case of a type switch, a
// conversion, a composite literal; "" for the other uses.
func classifyType(fset *token.FileSet, info *types.Info, stack []ast.Node, i int, e ast.Expr) (string, string) {
	switch parent := stack[i-1].(type) {
	case *ast.CaseClause:
		if i >= 3 {
			if ts, ok := stack[i-3].(*ast.TypeSwitchStmt); ok {
				return "a type switch case", "switch " + line(fset, ts.Assign) + ": case " + line(fset, e)
			}
		}
	case *ast.CallExpr:
		if parent.Fun == e && len(parent.Args) == 1 {
			return "converted to", line(fset, parent)
		}
	case *ast.CompositeLit:
		if parent.Type == e {
			return "made by a literal", line(fset, parent)
		}
	}
	return "", ""
}

// paramType is the type of the parameter that arg is passed for in call.
func paramType(sig *types.Signature, call *ast.CallExpr, arg ast.Expr) types.Type {
	params := sig.Params()
	for k, a := range call.Args {
		if a != arg || params.Len() == 0 {
			continue
		}
		if sig.Variadic() && k >= params.Len()-1 {
			if s, ok := params.At(params.Len() - 1).Type().(*types.Slice); ok && !call.Ellipsis.IsValid() {
				return s.Elem()
			}
			return params.At(params.Len() - 1).Type()
		}
		if k < params.Len() {
			return params.At(k).Type()
		}
	}
	return nil
}

// line prints node, or the first line of it, without its trailing brace.
func line(fset *token.FileSet, node ast.Node) string {
	var buf bytes.Buffer
	printer.Fprint(&buf, fset, node)
	s, _, _ := strings.Cut(buf.String(), "\n")
	return strings.TrimSuffix(strings.TrimSpace(s), " {")
}
