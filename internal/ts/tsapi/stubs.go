package tsapi

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/checker"
	"github.com/buke/typescript-go-internal/v7/pkg/tspath"
)

// Stub is a declaration of a name a file calls and nothing declares,
// typed from the call: a function, or a member of a class or interface.
type Stub struct {
	Name string
	File string // absolute: the file it goes in
	// Owner is the class or interface it goes in, for a member; "" for a
	// function at the top level.
	Owner string
	Text  string // the declaration, indented with two spaces a step
}

// Stubs returns a stub for each of names that the file at path calls and
// nothing declares: a function of the file; an exported function of the
// module the file imports it from; or a method of the class (a signature
// of the interface) it is called on. It refuses a name the file does not
// call ("calls no undeclared function name"), and one whose result the
// call does not tell ("cannot tell the results of name from ..."): the
// engine leaves such a name out and asks for the rest.
func (p *Project) Stubs(ctx context.Context, path string, names []string) ([]Stub, string) {
	l, refusal := p.lined(path)
	if refusal != "" {
		return nil, refusal
	}
	c, done := p.prog.GetProgram().GetTypeCheckerForFile(ctx, l.f)
	defer done()
	var stubs []Stub
	for _, name := range names {
		s, why := l.stub(c, name)
		if why != "" {
			return nil, why
		}
		stubs = append(stubs, s)
	}
	return stubs, ""
}

// stub returns the stub of name, from its first call in the file.
func (l *lined) stub(c *checker.Checker, name string) (Stub, string) {
	rel := tspath.GetBaseFileName(l.f.FileName())
	s := Stub{Name: name, File: l.f.FileName()}
	exported := false
	if from, ok := l.importedFrom(c, name); ok {
		if from == "" {
			return Stub{}, fmt.Sprintf("%s is imported from a module outside the solution, where no stub can go", name)
		}
		s.File, exported = from, true
	}
	var call *ast.Node
	method := false
	var visit func(n *ast.Node) bool
	visit = func(n *ast.Node) bool {
		if call != nil {
			return true
		}
		if ast.IsCallExpression(n) {
			callee := n.Expression()
			switch {
			case ast.IsIdentifier(callee) && callee.Text() == name:
				call = n
			case ast.IsPropertyAccessExpression(callee) && callee.Name().Text() == name && c.GetSymbolAtLocation(callee.Name()) == nil:
				call, method = n, true
			}
		}
		return n.ForEachChild(visit)
	}
	visit(l.f.AsNode())
	if call == nil {
		return Stub{}, fmt.Sprintf("%s calls no undeclared function %s", rel, name)
	}
	result, ok := resultType(c, call)
	if !ok {
		return Stub{}, fmt.Sprintf("cannot tell the results of %s from how %s uses it: give its call a type (const x: T = %s(...)), or declare it", name, rel, name)
	}
	params := parameters(c, call)
	sig := fmt.Sprintf("%s(%s): %s", name, params, result)
	body := " {\n  throw new Error(\"not implemented\");\n}"
	if !method {
		s.Text = "function " + sig + body
		if exported {
			s.Text = "export " + s.Text
		}
		return s, ""
	}
	t := c.GetTypeAtLocation(call.Expression().Expression())
	sym := t.Symbol()
	if sym == nil {
		return Stub{}, fmt.Sprintf("%s is called on a value whose type is not a class or interface of the solution", name)
	}
	for _, d := range sym.Declarations {
		f := ast.GetSourceFileOfNode(d)
		if f == nil || f.IsDeclarationFile || strings.Contains(f.FileName(), "/node_modules/") {
			continue
		}
		switch {
		case ast.IsClassDeclaration(d):
			s.File, s.Owner, s.Text = f.FileName(), sym.Name, sig+body
			return s, ""
		case ast.IsInterfaceDeclaration(d):
			s.File, s.Owner, s.Text = f.FileName(), sym.Name, sig+";"
			return s, ""
		}
	}
	return Stub{}, fmt.Sprintf("%s is called on %s, which is not a class or interface declared in the solution", name, c.TypeToString(t))
}

// importedFrom reports whether the file imports name, and from which file
// of the solution ("" for a module outside it).
func (l *lined) importedFrom(c *checker.Checker, name string) (string, bool) {
	for _, s := range l.f.Statements.Nodes {
		if !ast.IsImportDeclaration(s) {
			continue
		}
		decl := s.AsImportDeclaration()
		clause := decl.ImportClause
		if clause == nil {
			continue
		}
		nb := clause.AsImportClause().NamedBindings
		if nb == nil || !ast.IsNamedImports(nb) {
			continue
		}
		for _, el := range nb.AsNamedImports().Elements.Nodes {
			if el.Name().Text() != name {
				continue
			}
			sym := c.GetSymbolAtLocation(decl.ModuleSpecifier)
			if sym == nil {
				return "", true
			}
			for _, d := range sym.Declarations {
				if ast.IsSourceFile(d) {
					f := d.AsSourceFile()
					if f.IsDeclarationFile || strings.Contains(f.FileName(), "/node_modules/") {
						return "", true
					}
					return f.FileName(), true
				}
			}
			return "", true
		}
	}
	return "", false
}

// resultType is the type a call's result is used as: what its place
// expects (unknown where it takes any value), or void for a call that is
// a statement.
func resultType(c *checker.Checker, call *ast.Node) (string, bool) {
	parent := call.Parent
	for parent != nil && ast.IsParenthesizedExpression(parent) {
		parent = parent.Parent
	}
	if parent != nil && ast.IsExpressionStatement(parent) {
		return "void", true
	}
	t := c.GetContextualType(call, checker.ContextFlagsNone)
	if t == nil {
		return "", false
	}
	s := c.TypeToString(t)
	if s == "any" || s == "unknown" {
		return "unknown", true
	}
	return s, true
}

// Functions returns the names of the functions content declares at its
// top level with a body, or of the methods when content is members of a
// class; none when content does not parse.
func Functions(content string, members bool) []string {
	text := content
	if members {
		text = "class __Content {\n" + content + "\n}\n"
	}
	f := parseText(text)
	if len(f.Diagnostics()) > 0 {
		return nil
	}
	nodes := f.Statements.Nodes
	if members {
		if len(nodes) != 1 || !ast.IsClassDeclaration(nodes[0]) {
			return nil
		}
		nodes = nodes[0].Members()
	}
	var out []string
	for _, n := range nodes {
		if (ast.IsFunctionDeclaration(n) || ast.IsMethodDeclaration(n)) && n.Name() != nil && n.Body() != nil {
			out = append(out, n.Name().Text())
		}
	}
	return out
}

// parameters writes the parameters of a stub from a call's arguments:
// each named after the argument when it is a name, or else its type,
// typed by the argument's type, widened from a literal.
func parameters(c *checker.Checker, call *ast.Node) string {
	used := map[string]bool{}
	var out []string
	for i, arg := range call.Arguments() {
		t := c.GetWidenedType(c.GetBaseTypeOfLiteralType(c.GetTypeAtLocation(arg)))
		typ := c.TypeToString(t)
		name := ""
		switch {
		case ast.IsIdentifier(arg):
			name = arg.Text()
		case ast.IsPropertyAccessExpression(arg):
			name = arg.Name().Text()
		default:
			name = nameOfType(typ, i)
		}
		base := name
		for n := 2; used[name]; n++ {
			name = fmt.Sprintf("%s%d", base, n)
		}
		used[name] = true
		out = append(out, name+": "+typ)
	}
	return strings.Join(out, ", ")
}

// nameOfType is a parameter name for a value of type typ: its name in
// lower camel case, or a letter for a primitive.
func nameOfType(typ string, i int) string {
	switch typ {
	case "number":
		return "n"
	case "string":
		return "s"
	case "boolean":
		return "b"
	}
	r := []rune(typ)
	if len(r) == 0 || !unicode.IsLetter(r[0]) || strings.ContainsAny(typ, "<>[]|&{}() ") {
		return fmt.Sprintf("arg%d", i)
	}
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// ImportedFrom reports whether the file at path imports name, and from
// which file of the solution ("" for a module outside it).
func (p *Project) ImportedFrom(ctx context.Context, path, name string) (string, bool) {
	l, refusal := p.lined(path)
	if refusal != "" {
		return "", false
	}
	c, done := p.prog.GetProgram().GetTypeCheckerForFile(ctx, l.f)
	defer done()
	return l.importedFrom(c, name)
}
