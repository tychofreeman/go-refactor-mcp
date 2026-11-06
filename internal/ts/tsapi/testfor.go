package tsapi

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/checker"
)

// TestFor returns a red test of target, a top-level function or a
// Class.method of the project: a test(...) call whose body makes the
// receiver with the class's constructor and placeholder arguments, calls
// the target, keeps its result and throws an error that shows it until
// the assertion is written. found is false when no top-level declaration
// of the project has the target's name.
func (p *Project) TestFor(ctx context.Context, target string) (test string, found bool, refusal string) {
	className, method, isMethod := strings.Cut(target, ".")
	var decl *ast.Node
	for _, f := range p.sources() {
		for _, s := range f.Statements.Nodes {
			if s.Name() == nil || s.Name().Text() != className {
				continue
			}
			if (isMethod && ast.IsClassDeclaration(s)) || (!isMethod && ast.IsFunctionDeclaration(s)) {
				decl = s
			}
		}
	}
	if decl == nil {
		return "", false, ""
	}
	c, done := p.prog.GetProgram().GetTypeChecker(ctx)
	defer done()
	var body strings.Builder
	fn, call, name := decl, target, "result"
	if isMethod {
		fn = nil
		var ctor *ast.Node
		for _, m := range decl.AsClassDeclaration().Members.Nodes {
			switch {
			case ast.IsMethodDeclaration(m) && m.Name() != nil && m.Name().Text() == method && !ast.HasSyntacticModifier(m, ast.ModifierFlagsStatic):
				if fn == nil || m.Body() != nil {
					fn = m
				}
			case ast.IsConstructorDeclaration(m) && (ctor == nil || len(m.Parameters()) < len(ctor.Parameters())):
				ctor = m
			}
		}
		if fn == nil {
			return "", true, fmt.Sprintf("%s has no instance method %s", className, method)
		}
		recv := lowerFirst(className)
		args := ""
		if ctor != nil {
			args = placeholders(c, ctor)
		}
		fmt.Fprintf(&body, "  const %s = new %s(%s);\n", recv, className, args)
		call, name = recv+"."+method, lowerFirst(method)
	} else {
		method = className
	}
	ret := c.GetReturnTypeOfSignature(c.GetSignatureFromDeclaration(fn))
	async, result := "", c.TypeToString(ret)
	invocation := fmt.Sprintf("%s(%s)", call, placeholders(c, fn))
	if strings.HasPrefix(result, "Promise<") {
		async, invocation = "async ", "await "+invocation
		result = strings.TrimSuffix(strings.TrimPrefix(result, "Promise<"), ">")
	}
	if result == "void" || result == "undefined" || result == "never" {
		fmt.Fprintf(&body, "  %s;\n  throw new Error(\"write the assertion\");\n", invocation)
	} else {
		fmt.Fprintf(&body, "  const %s = %s;\n  throw new Error(`write the assertion: %s returned ${JSON.stringify(%s)}`);\n", name, invocation, method, name)
	}
	return fmt.Sprintf("test(%q, %s() => {\n%s});\n", target, async, body.String()), true, ""
}

// placeholders returns an argument for each required parameter of fn: the
// zero of its type, or undefined cast to it.
func placeholders(c *checker.Checker, fn *ast.Node) string {
	var out []string
	for _, prm := range fn.Parameters() {
		pd := prm.AsParameterDeclaration()
		if pd.QuestionToken != nil || pd.Initializer != nil || pd.DotDotDotToken != nil {
			break
		}
		t := c.TypeToString(c.GetTypeAtLocation(prm))
		switch {
		case t == "number":
			out = append(out, "0")
		case t == "string":
			out = append(out, `""`)
		case t == "boolean":
			out = append(out, "false")
		case strings.HasSuffix(t, "[]"):
			out = append(out, "[]")
		default:
			out = append(out, "undefined as unknown as "+t)
		}
	}
	return strings.Join(out, ", ")
}

func lowerFirst(s string) string {
	for i, r := range s {
		return string(unicode.ToLower(r)) + s[i+len(string(r)):]
	}
	return s
}
