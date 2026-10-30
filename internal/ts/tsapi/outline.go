package tsapi

import (
	"sort"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/scanner"
	"github.com/buke/typescript-go-internal/v7/pkg/tspath"
)

// Decl summarizes a declaration of a file: one at the top level, or a
// member of a top-level class.
type Decl struct {
	Kind      string // function, class, method, interface, type, enum, const, let, var
	Name      string // "name", or "Class.member" for a member
	Signature string // its head, on one line
	// Start and End are its lines, including the doc comment.
	Start, End int
	// BodyStart and BodyEnd are a function's or method's lines inside the
	// braces; 0 when there are fewer than 2.
	BodyStart, BodyEnd int
	// BlockStart and BlockEnd are the offsets of its body's opening brace
	// and just past its closing one; 0 when its body is not in braces.
	BlockStart, BlockEnd int
}

// Outline returns the declarations of the file at path, in order, or nil
// if the program does not have the file.
func (p *Project) Outline(path string) []Decl {
	f := p.sourceFile(path)
	if f == nil {
		return nil
	}
	text := f.Text()
	line := func(off int) int { return 1 + strings.Count(text[:off], "\n") }
	decl := func(n *ast.Node, kind, name string, body *ast.Node) Decl {
		start := scanner.GetTokenPosOfNode(n, f, false)
		d := Decl{Kind: kind, Name: name, Start: line(scanner.GetTokenPosOfNode(n, f, true)), End: line(max(n.End()-1, start))}
		head := n.End()
		if body != nil && ast.IsBlock(body) {
			head = scanner.GetTokenPosOfNode(body, f, false)
			d.BlockStart, d.BlockEnd = head, body.End()
			open, close := line(head), line(max(body.End()-1, head))
			if close-open-1 >= 2 {
				d.BodyStart, d.BodyEnd = open+1, close-1
			}
		}
		d.Signature = oneLine(text[start:head])
		return d
	}
	// nameOf is a declaration's name as written: a computed name
	// ([key]) or a private one (#lock) has no text of its own.
	nameOf := func(n *ast.Node) string {
		if nm := n.Name(); nm != nil {
			return text[scanner.GetTokenPosOfNode(nm, f, false):nm.End()]
		}
		return ""
	}
	var out []Decl
	for _, s := range f.Statements.Nodes {
		switch {
		case ast.IsFunctionDeclaration(s):
			out = append(out, decl(s, "function", nameOf(s), s.Body()))
		case ast.IsInterfaceDeclaration(s):
			out = append(out, decl(s, "interface", nameOf(s), nil))
		case ast.IsTypeAliasDeclaration(s):
			out = append(out, decl(s, "type", nameOf(s), nil))
		case ast.IsEnumDeclaration(s):
			out = append(out, decl(s, "enum", nameOf(s), nil))
		case ast.IsClassDeclaration(s):
			class := nameOf(s)
			out = append(out, decl(s, "class", class, nil))
			members := len(out)
			for _, m := range s.Members() {
				name := nameOf(m)
				switch {
				case ast.IsConstructorDeclaration(m):
					out = append(out, decl(m, "method", class+".constructor", m.Body()))
				case ast.IsMethodDeclaration(m), ast.IsGetAccessorDeclaration(m), ast.IsSetAccessorDeclaration(m):
					out = append(out, decl(m, "method", class+"."+name, m.Body()))
				case ast.IsPropertyDeclaration(m):
					if fn := functionBody(m.Initializer()); fn != nil {
						out = append(out, decl(m, "method", class+"."+name, fn))
					}
				}
			}
			// A member's head names its class, as a Go method's names its
			// receiver: Bed.plant(p: Plant): void, Bed: get tallest().
			for i := members; i < len(out); i++ {
				if member := strings.TrimPrefix(out[i].Name, class+"."); strings.HasPrefix(out[i].Signature, member) {
					out[i].Signature = class + "." + out[i].Signature
				} else {
					out[i].Signature = class + ": " + out[i].Signature
				}
			}
		case ast.IsVariableStatement(s):
			list := s.AsVariableStatement().DeclarationList
			kind := "var"
			if lt := strings.TrimSpace(text[scanner.GetTokenPosOfNode(list, f, false):]); strings.HasPrefix(lt, "const") {
				kind = "const"
			} else if strings.HasPrefix(lt, "let") {
				kind = "let"
			}
			for _, d := range list.AsVariableDeclarationList().Declarations.Nodes {
				if nm := d.Name(); nm == nil || !ast.IsIdentifier(nm) {
					continue
				}
				if fn := functionBody(d.Initializer()); fn != nil && len(list.AsVariableDeclarationList().Declarations.Nodes) == 1 {
					out = append(out, decl(s, "function", nameOf(d), fn))
					continue
				}
				out = append(out, decl(s, kind, nameOf(d), nil))
			}
		}
	}
	return out
}

// functionBody is the block body of an arrow function or function
// expression, or nil.
func functionBody(n *ast.Node) *ast.Node {
	if n == nil || !(ast.IsArrowFunction(n) || ast.IsFunctionExpression(n)) {
		return nil
	}
	if b := n.Body(); b != nil && ast.IsBlock(b) {
		return b
	}
	return nil
}

// oneLine is s with its whitespace runs made single spaces, trimmed, at
// most 160 characters.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 160 {
		s = s[:157] + "..."
	}
	return s
}

// Files returns the source files of the program under dir, not in
// node_modules and not declaration files, sorted.
func (p *Project) Files(dir string) []string {
	dir = tspath.NormalizePath(dir)
	var out []string
	for _, f := range p.sources() {
		if name := f.FileName(); strings.HasPrefix(name, dir+"/") {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
