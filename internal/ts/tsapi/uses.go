package tsapi

import (
	"context"
	"fmt"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
)

// UseRoles are what a use does with what it names, in the order shown.
var UseRoles = []string{"switch on it", "compared", "written", "called", "made with new", "used as a type", "read"}

// Use is one use of a declaration: where it is, what it does with what it
// names (one of UseRoles), and the line it is on.
type Use struct {
	File       string // absolute
	Line, Pos  int
	Role, Text string
}

// UsesOf returns every use in the project of the declaration called name,
// "Name" at the top level of a file or "Class.member", with what it is
// ("property of Plant in src/garden.ts"). found is false when the project
// declares no such name; why says where, when it declares more than one.
func (p *Project) UsesOf(ctx context.Context, name string, rel func(string) string) (desc string, uses []Use, found bool, why string) {
	c, done := p.prog.GetProgram().GetTypeChecker(ctx)
	defer done()
	var decls []*ast.Node
	var files []string
	for _, f := range p.prog.GetProgram().GetSourceFiles() {
		if f.IsDeclarationFile || strings.Contains(f.FileName(), "/node_modules/") {
			continue
		}
		if nm := declarationNamed(f, name); nm != nil {
			decls = append(decls, nm)
			files = append(files, rel(f.FileName()))
		}
	}
	switch len(decls) {
	case 0:
		return "", nil, false, ""
	case 1:
	default:
		return "", nil, true, fmt.Sprintf("declared in %s; inspect it by file", strings.Join(files, ", "))
	}
	sym := c.GetSymbolAtLocation(decls[0])
	if sym == nil {
		return "", nil, true, "the language service knows no symbol for it"
	}
	desc = kindOf(decls[0].Parent) + " in " + files[0]
	if owner, _, ok := strings.Cut(name, "."); ok {
		desc = kindOf(decls[0].Parent) + " of " + owner + " in " + files[0]
	}
	for _, f := range p.prog.GetProgram().GetSourceFiles() {
		if f.IsDeclarationFile || strings.Contains(f.FileName(), "/node_modules/") {
			continue
		}
		l := newLined(f)
		inspect(f.AsNode(), func(n *ast.Node) bool {
			if ast.IsIdentifier(n) && n != decls[0] && n.Text() == sym.Name {
				if s := c.GetSymbolAtLocation(n); s != nil && c.SkipAlias(s) == sym && !ast.IsImportSpecifier(n.Parent) && !ast.IsExportSpecifier(n.Parent) {
					pos := l.pos(n)
					line := l.line(pos)
					uses = append(uses, Use{File: f.FileName(), Line: line, Pos: pos, Role: useRole(n), Text: strings.TrimSpace(l.text[l.start(line):l.end(line)])})
				}
			}
			return true
		})
	}
	return desc, uses, true, ""
}

// declarationNamed returns the name of the declaration of f called name:
// at its top level, or a member ("Class.member") of a class, interface or
// object type alias there; or nil.
func declarationNamed(f *ast.SourceFile, name string) *ast.Node {
	owner, member, isMember := strings.Cut(name, ".")
	l := newLined(f)
	for _, s := range f.Statements.Nodes {
		if isMember {
			if s.Name() == nil || s.Name().Text() != owner {
				continue
			}
			members, _ := l.members(owner)
			for _, m := range members {
				if nm := m.Name(); nm != nil && l.source(nm) == member {
					return nm
				}
			}
			continue
		}
		if ast.IsVariableStatement(s) {
			for _, d := range s.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes {
				if nm := d.Name(); nm != nil && ast.IsIdentifier(nm) && nm.Text() == name {
					return nm
				}
			}
			continue
		}
		if nm := s.Name(); nm != nil && ast.IsIdentifier(nm) && nm.Text() == name {
			return nm
		}
	}
	return nil
}

// kindOf names the kind of a declaration.
func kindOf(d *ast.Node) string {
	switch {
	case ast.IsFunctionDeclaration(d):
		return "function"
	case ast.IsClassDeclaration(d):
		return "class"
	case ast.IsInterfaceDeclaration(d):
		return "interface"
	case ast.IsTypeAliasDeclaration(d):
		return "type"
	case ast.IsEnumDeclaration(d):
		return "enum"
	case ast.IsMethodDeclaration(d), ast.IsMethodSignatureDeclaration(d):
		return "method"
	case ast.IsPropertyDeclaration(d), ast.IsPropertySignatureDeclaration(d):
		return "property"
	case ast.IsGetAccessorDeclaration(d), ast.IsSetAccessorDeclaration(d):
		return "accessor"
	}
	return "variable"
}

// useRole says what the use n, an identifier, does with what it names.
func useRole(n *ast.Node) string {
	e := n
	if p := e.Parent; p != nil && ast.IsPropertyAccessExpression(p) && p.Name() == e {
		e = p
	}
	for e.Parent != nil && ast.IsParenthesizedExpression(e.Parent) {
		e = e.Parent
	}
	p := e.Parent
	switch {
	case p == nil:
	case ast.IsTypeReferenceNode(p), ast.IsExpressionWithTypeArguments(p) && p.Parent != nil && ast.IsHeritageClause(p.Parent), ast.IsQualifiedName(p):
		return "used as a type"
	case ast.IsSwitchStatement(p) && p.Expression() == e:
		return "switch on it"
	case ast.IsBinaryExpression(p):
		b := p.AsBinaryExpression()
		switch b.OperatorToken.Kind {
		case ast.KindEqualsEqualsEqualsToken, ast.KindExclamationEqualsEqualsToken, ast.KindEqualsEqualsToken, ast.KindExclamationEqualsToken:
			return "compared"
		}
		if b.Left == e && ast.IsAssignmentOperator(b.OperatorToken.Kind) {
			return "written"
		}
	case (ast.IsPrefixUnaryExpression(p) || ast.IsPostfixUnaryExpression(p)) && isIncrement(p):
		return "written"
	case ast.IsCallExpression(p) && p.Expression() == e:
		return "called"
	case ast.IsNewExpression(p) && p.Expression() == e:
		return "made with new"
	}
	return "read"
}

// isIncrement reports whether the unary expression u is ++ or --.
func isIncrement(u *ast.Node) bool {
	var op ast.Kind
	if ast.IsPrefixUnaryExpression(u) {
		op = u.AsPrefixUnaryExpression().Operator
	} else {
		op = u.AsPostfixUnaryExpression().Operator
	}
	return op == ast.KindPlusPlusToken || op == ast.KindMinusMinusToken
}
