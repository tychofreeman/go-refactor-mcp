package tsapi

import (
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/core"
	"github.com/buke/typescript-go-internal/v7/pkg/parser"
	"github.com/buke/typescript-go-internal/v7/pkg/scanner"
	"github.com/buke/typescript-go-internal/v7/pkg/tspath"
)

// Measured is a function's numbers, as Go's inspect gives them: its lines
// (with its doc comment), its cyclomatic complexity and its deepest
// nesting of control statements, a closure's counted in its function's.
type Measured struct {
	Name                string // "name", or "Class.member"
	Start, End          int    // lines
	Complexity, Nesting int
	Text                string // its source, to tell whether it changed
}

// Measure returns the functions, methods and functions bound to a const
// at the top level of the TypeScript source src (named path), in order;
// nil when it does not parse.
func Measure(path string, src []byte) []Measured {
	f := parseFile(path, src)
	if f == nil {
		return nil
	}
	text := f.Text()
	line := func(off int) int { return 1 + strings.Count(text[:off], "\n") }
	var out []Measured
	add := func(n, fn *ast.Node, name string) {
		if fn == nil || fn.Body() == nil {
			return
		}
		start := scanner.GetTokenPosOfNode(n, f, true)
		m := Measured{Name: name, Start: line(start), End: line(max(n.End()-1, start)), Text: text[start:n.End()]}
		m.Complexity, m.Nesting = complexity(fn.Body())
		out = append(out, m)
	}
	for _, s := range f.Statements.Nodes {
		switch {
		case ast.IsFunctionDeclaration(s) && s.Name() != nil:
			add(s, s, s.Name().Text())
		case ast.IsClassDeclaration(s) && s.Name() != nil:
			for _, m := range s.Members() {
				switch {
				case ast.IsConstructorDeclaration(m):
					add(m, m, s.Name().Text()+".constructor")
				case (ast.IsMethodDeclaration(m) || ast.IsAccessor(m)) && m.Name() != nil:
					add(m, m, s.Name().Text()+"."+text[scanner.GetTokenPosOfNode(m.Name(), f, false):m.Name().End()])
				}
			}
		case ast.IsVariableStatement(s):
			for _, d := range s.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes {
				if init := d.Initializer(); init != nil && (ast.IsArrowFunction(init) || ast.IsFunctionExpression(init)) && ast.IsIdentifier(d.Name()) {
					add(s, init, d.Name().Text())
				}
			}
		}
	}
	return out
}

// complexity returns 1 and a point for each if, loop, case (not default),
// conditional expression, &&, || and ??, and the deepest nesting of ifs,
// loops and switches in body.
func complexity(body *ast.Node) (int, int) {
	c, deepest := 1, 0
	var visit func(n *ast.Node, depth int)
	visit = func(n *ast.Node, depth int) {
		control := ast.IsIfStatement(n) || ast.IsIterationStatement(n, false) || ast.IsSwitchStatement(n)
		if control {
			depth++
			deepest = max(deepest, depth)
		}
		switch {
		case ast.IsIfStatement(n), ast.IsIterationStatement(n, false), ast.IsCaseClause(n), ast.IsConditionalExpression(n):
			c++
		case ast.IsBinaryExpression(n):
			switch n.AsBinaryExpression().OperatorToken.Kind {
			case ast.KindAmpersandAmpersandToken, ast.KindBarBarToken, ast.KindQuestionQuestionToken:
				c++
			}
		}
		n.ForEachChild(func(ch *ast.Node) bool { visit(ch, depth); return false })
	}
	visit(body, 0)
	return c, deepest
}

// Parses reports whether the TypeScript source src (named path) parses.
func Parses(path string, src []byte) bool {
	return parseFile(path, src) != nil
}

func parseFile(path string, src []byte) *ast.SourceFile {
	kind := core.ScriptKindTS
	if strings.HasSuffix(path, ".tsx") {
		kind = core.ScriptKindTSX
	}
	f := parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: "/" + path, Path: tspath.Path("/" + path)}, string(src), kind)
	if len(f.Diagnostics()) > 0 {
		return nil
	}
	return f
}
