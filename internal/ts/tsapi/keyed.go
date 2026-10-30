package tsapi

import (
	"context"
	"fmt"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/checker"
)

// byKey are the functions that find a value's properties by name: their
// result changes when a property is renamed.
var byKey = map[string]bool{
	"JSON.stringify": true, "Object.keys": true, "Object.values": true, "Object.entries": true,
	"Object.getOwnPropertyNames": true, "Object.assign": true, "Reflect.ownKeys": true,
}

// KeyedUses checks renaming the member at offset in the file at path:
// where a value holding its type reaches code that finds properties by
// name (JSON.stringify, Object.keys and the like, for...in, or "name" in
// x), the rename could change what that code does, and refusal says
// where. printed lists the calls of console that print such a value,
// whose output changes. Both are empty for what is not a member of a
// class, interface or object type.
func (p *Project) KeyedUses(ctx context.Context, path string, offset int) (refusal string, printed []string) {
	l, why := p.lined(path)
	if why != "" {
		return "", nil
	}
	var id *ast.Node
	var find func(n *ast.Node) bool
	find = func(n *ast.Node) bool {
		if ast.IsIdentifier(n) && l.pos(n) == offset {
			id = n
			return true
		}
		return n.ForEachChild(find)
	}
	find(l.f.AsNode())
	if id == nil {
		return "", nil
	}
	c, done := p.prog.GetProgram().GetTypeChecker(ctx)
	defer done()
	sym := c.GetSymbolAtLocation(id)
	if sym == nil || sym.Flags&(ast.SymbolFlagsProperty|ast.SymbolFlagsMethod|ast.SymbolFlagsGetAccessor|ast.SymbolFlagsSetAccessor) == 0 || sym.Parent == nil {
		return "", nil
	}
	owner := sym.Parent
	holds := func(t *checker.Type) bool { return typeHolds(c, t, owner, map[*checker.Type]bool{}) }
	for _, f := range p.prog.GetProgram().GetSourceFiles() {
		if f.IsDeclarationFile || strings.Contains(f.FileName(), "/node_modules/") {
			continue
		}
		fl := newLined(f)
		var visit func(n *ast.Node) bool
		visit = func(n *ast.Node) bool {
			if refusal != "" {
				return true
			}
			switch {
			case ast.IsCallExpression(n) && ast.IsPropertyAccessExpression(n.Expression()):
				callee := fl.source(n.Expression())
				for _, a := range n.Arguments() {
					if !holds(c.GetTypeAtLocation(a)) {
						continue
					}
					switch {
					case byKey[callee]:
						refusal = fmt.Sprintf("a value holding %s is passed to %s at %s, which finds properties by name, so renaming %s could change what it does", owner.Name, callee, fl.where(n), sym.Name)
					case strings.HasPrefix(callee, "console."):
						printed = append(printed, fl.where(n)+" "+callee)
					}
				}
			case ast.IsForInStatement(n) && holds(c.GetTypeAtLocation(n.Expression())):
				refusal = fmt.Sprintf("a value holding %s is looped over with for...in at %s, which finds properties by name, so renaming %s could change what it does", owner.Name, fl.where(n), sym.Name)
			case ast.IsBinaryExpression(n) && n.AsBinaryExpression().OperatorToken.Kind == ast.KindInKeyword:
				b := n.AsBinaryExpression()
				if ast.IsStringLiteral(b.Left) && b.Left.Text() == sym.Name && holds(c.GetTypeAtLocation(b.Right)) {
					refusal = fmt.Sprintf("%q in tests a value of %s for the property by name at %s; the rename would leave the string, so change the test first", sym.Name, owner.Name, fl.where(n))
				}
			}
			return n.ForEachChild(visit)
		}
		visit(f.AsNode())
		if refusal != "" {
			return refusal, nil
		}
	}
	return "", printed
}

// typeHolds reports whether a value of type t holds a value of the class,
// interface or object type owner: t itself, the elements of an array or
// tuple, the members of a union or intersection, or the properties of an
// object type.
func typeHolds(c *checker.Checker, t *checker.Type, owner *ast.Symbol, seen map[*checker.Type]bool) bool {
	if t == nil || seen[t] {
		return false
	}
	seen[t] = true
	if t.Symbol() == owner || t.Alias() != nil && t.Alias().Symbol() == owner {
		return true
	}
	if t.IsUnion() || t.IsIntersection() {
		for _, u := range t.Types() {
			if typeHolds(c, u, owner, seen) {
				return true
			}
		}
		return false
	}
	for _, a := range c.GetTypeArguments(t) {
		if typeHolds(c, a, owner, seen) {
			return true
		}
	}
	if s := t.Symbol(); s != nil && s.Flags&(ast.SymbolFlagsTypeLiteral|ast.SymbolFlagsObjectLiteral|ast.SymbolFlagsInterface|ast.SymbolFlagsClass) != 0 {
		for _, prop := range c.GetPropertiesOfType(t) {
			if typeHolds(c, c.GetTypeOfSymbol(prop), owner, seen) {
				return true
			}
		}
	}
	return false
}
