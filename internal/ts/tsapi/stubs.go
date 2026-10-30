package tsapi

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/checker"
	"github.com/buke/typescript-go-internal/v7/pkg/scanner"
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
	// Before is, for a class the file itself makes, the offset of the
	// top-level statement that first makes it, where the class must go
	// (classes are not hoisted); -1 for the end of the file.
	Before int
}

// Stubs returns a stub for each of names that the file at path uses and
// nothing declares: a function it calls, of the file or an exported one of
// the module the file imports it from; a class it makes with new, there
// too; a method or property of the class (a signature or property of the
// interface) it calls or reads on. results gives the result type of a
// function or method, or the type of a property, by name, where its use
// does not tell it. It refuses a name the file does not use so ("calls no
// undeclared function name"), and one whose type the use does not tell
// ("cannot tell the results of name from ..."): the engine leaves such a
// name out and asks for the rest.
func (p *Project) Stubs(ctx context.Context, path string, names []string, results map[string]string) ([]Stub, string) {
	l, refusal := p.lined(path)
	if refusal != "" {
		return nil, refusal
	}
	c, done := p.prog.GetProgram().GetTypeCheckerForFile(ctx, l.f)
	defer done()
	var stubs []Stub
	for _, name := range names {
		s, why := l.stub(c, name, results[name])
		if why != "" {
			return nil, why
		}
		stubs = append(stubs, s)
	}
	return stubs, ""
}

// stub returns the stub of name, from its first use in the file: a call,
// else a new, else a property read or written; result, if not "", is its
// type.
func (l *lined) stub(c *checker.Checker, name, result string) (Stub, string) {
	rel := tspath.GetBaseFileName(l.f.FileName())
	s := Stub{Name: name, File: l.f.FileName(), Before: -1}
	exported := false
	if from, ok := l.importedFrom(c, name); ok {
		if from == "" {
			return Stub{}, fmt.Sprintf("%s is imported from a module outside the solution, where no stub can go", name)
		}
		s.File, exported = from, true
	}
	var call, made, prop *ast.Node
	method := false
	var visit func(n *ast.Node) bool
	visit = func(n *ast.Node) bool {
		switch {
		case call == nil && ast.IsCallExpression(n):
			callee := n.Expression()
			switch {
			case ast.IsIdentifier(callee) && callee.Text() == name:
				call = n
			case ast.IsPropertyAccessExpression(callee) && callee.Name().Text() == name && c.GetSymbolAtLocation(callee.Name()) == nil:
				call, method = n, true
			}
		case made == nil && ast.IsNewExpression(n) && ast.IsIdentifier(n.Expression()) && n.Expression().Text() == name:
			made = n
		case prop == nil && ast.IsPropertyAccessExpression(n) && n.Name().Text() == name && !isCallee(n) && c.GetSymbolAtLocation(n.Name()) == nil:
			prop = n
		}
		return n.ForEachChild(visit)
	}
	visit(l.f.AsNode())
	export := func(text string) string {
		if exported {
			return "export " + text
		}
		return text
	}
	switch {
	case call == nil && made != nil:
		s.Text = export("class " + name + " {}")
		if params := parameters(c, made); params != "" {
			s.Text = export("class " + name + " {\n  constructor(" + params + ") {}\n}")
		}
		if !exported {
			top := made
			for top.Parent != nil && !ast.IsSourceFile(top.Parent) {
				top = top.Parent
			}
			s.Before = l.start(l.line(scanner.GetTokenPosOfNode(top, l.f, true)))
		}
		return s, ""
	case call == nil && prop != nil:
		typ := result
		if typ == "" {
			var ok bool
			if typ, ok = propertyType(c, prop); !ok {
				return Stub{}, fmt.Sprintf("cannot tell the type of %s from how %s uses it: give it a place of a type (const x: T = v.%s), or give results", name, rel, name)
			}
		}
		file, owner, class, why := l.owner(c, prop.Expression(), name)
		if why != "" {
			return Stub{}, why
		}
		s.File, s.Owner, s.Text = file, owner, name+": "+typ+";"
		if class {
			s.Text = name + "!: " + typ + ";"
		}
		return s, ""
	case call == nil:
		return Stub{}, fmt.Sprintf("%s calls no undeclared function %s", rel, name)
	}
	if result == "" {
		var ok bool
		if result, ok = resultType(c, call); !ok {
			return Stub{}, fmt.Sprintf("cannot tell the results of %s from how %s uses it: give its call a type (const x: T = %s(...)), or declare it", name, rel, name)
		}
	}
	sig := fmt.Sprintf("%s(%s): %s", name, parameters(c, call), result)
	body := " {\n  throw new Error(\"not implemented\");\n}"
	if !method {
		s.Text = export("function " + sig + body)
		return s, ""
	}
	file, owner, class, why := l.owner(c, call.Expression().Expression(), name)
	if why != "" {
		return Stub{}, why
	}
	s.File, s.Owner, s.Text = file, owner, sig+";"
	if class {
		s.Text = sig + body
	}
	return s, ""
}

// owner returns the class or interface of the solution that the value of
// expr is, which a member name used on it goes in, its file, and whether
// it is a class; or why there is none.
func (l *lined) owner(c *checker.Checker, expr *ast.Node, name string) (file, owner string, class bool, why string) {
	t := c.GetTypeAtLocation(expr)
	sym := t.Symbol()
	if sym == nil {
		return "", "", false, fmt.Sprintf("%s is used on a value whose type is not a class or interface of the solution", name)
	}
	for _, d := range sym.Declarations {
		f := ast.GetSourceFileOfNode(d)
		if f == nil || f.IsDeclarationFile || strings.Contains(f.FileName(), "/node_modules/") {
			continue
		}
		switch {
		case ast.IsClassDeclaration(d):
			return f.FileName(), sym.Name, true, ""
		case ast.IsInterfaceDeclaration(d):
			return f.FileName(), sym.Name, false, ""
		}
	}
	return "", "", false, fmt.Sprintf("%s is used on %s, which is not a class or interface declared in the solution", name, c.TypeToString(t))
}

// isCallee reports whether n is the function a call calls.
func isCallee(n *ast.Node) bool {
	return n.Parent != nil && ast.IsCallExpression(n.Parent) && n.Parent.Expression() == n
}

// propertyType is the type of a property from its use: what is assigned
// to it, widened from a literal, or what its place expects (unknown where
// it takes any value).
func propertyType(c *checker.Checker, prop *ast.Node) (string, bool) {
	if p := prop.Parent; p != nil && ast.IsBinaryExpression(p) && p.AsBinaryExpression().OperatorToken.Kind == ast.KindEqualsToken && p.AsBinaryExpression().Left == prop {
		t := c.GetWidenedType(c.GetBaseTypeOfLiteralType(c.GetTypeAtLocation(p.AsBinaryExpression().Right)))
		return c.TypeToString(t), true
	}
	return resultType(c, prop)
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
