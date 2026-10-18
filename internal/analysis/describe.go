package analysis

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"
)

// Describe names the kind of syntax n is, in words, for a message: "an if
// statement", "a call", "a + operation".
func Describe(n ast.Node) string {
	switch n := n.(type) {
	// Statements.
	case *ast.AssignStmt:
		if n.Tok == token.DEFINE {
			return "a short variable declaration"
		}
		if n.Tok != token.ASSIGN {
			return fmt.Sprintf("a %s assignment", n.Tok)
		}
		return "an assignment"
	case *ast.IfStmt:
		return "an if statement"
	case *ast.ForStmt:
		return "a for loop"
	case *ast.RangeStmt:
		return "a range loop"
	case *ast.SwitchStmt:
		return "a switch statement"
	case *ast.TypeSwitchStmt:
		return "a type switch"
	case *ast.SelectStmt:
		return "a select statement"
	case *ast.CaseClause:
		return "a case clause"
	case *ast.CommClause:
		return "a select case"
	case *ast.ReturnStmt:
		return "a return statement"
	case *ast.BranchStmt:
		return fmt.Sprintf("a %s statement", n.Tok)
	case *ast.IncDecStmt:
		if n.Tok == token.INC {
			return "an increment"
		}
		return "a decrement"
	case *ast.ExprStmt:
		return "an expression statement"
	case *ast.DeclStmt:
		return "a declaration"
	case *ast.GoStmt:
		return "a go statement"
	case *ast.DeferStmt:
		return "a defer statement"
	case *ast.SendStmt:
		return "a send statement"
	case *ast.LabeledStmt:
		return "a labeled statement"
	case *ast.BlockStmt:
		return "a block"
	case *ast.EmptyStmt:
		return "an empty statement"

	// Expressions.
	case *ast.Ident:
		return "a name"
	case *ast.BasicLit:
		return "a literal"
	case *ast.CompositeLit:
		return "a composite literal"
	case *ast.FuncLit:
		return "a function literal"
	case *ast.CallExpr:
		return "a call"
	case *ast.BinaryExpr:
		return fmt.Sprintf("a %s operation", n.Op)
	case *ast.UnaryExpr:
		return fmt.Sprintf("a unary %s operation", n.Op)
	case *ast.StarExpr:
		return "a * operation"
	case *ast.SelectorExpr:
		return "a selector"
	case *ast.IndexExpr, *ast.IndexListExpr:
		return "an index expression"
	case *ast.SliceExpr:
		return "a slice expression"
	case *ast.TypeAssertExpr:
		return "a type assertion"
	case *ast.ParenExpr:
		return "a parenthesized expression"
	case *ast.KeyValueExpr:
		return "a key: value pair"
	case *ast.Ellipsis:
		return "a ..."

	// Types and declarations.
	case *ast.ArrayType:
		if n.Len == nil {
			return "a slice type"
		}
		return "an array type"
	case *ast.MapType:
		return "a map type"
	case *ast.ChanType:
		return "a channel type"
	case *ast.FuncType:
		return "a function type"
	case *ast.StructType:
		return "a struct type"
	case *ast.InterfaceType:
		return "an interface type"
	case *ast.FieldList:
		return "a list of parameters or fields"
	case *ast.Field:
		return "a parameter or field"
	case *ast.FuncDecl:
		return "a function declaration"
	case *ast.GenDecl:
		return fmt.Sprintf("a %s declaration", n.Tok)
	case *ast.ValueSpec, *ast.TypeSpec, *ast.ImportSpec:
		return "a declaration"
	case *ast.File:
		return "a file"
	case *ast.Comment, *ast.CommentGroup:
		return "a comment"
	}
	// Anything newer: its type's name, in words.
	name := strings.TrimSuffix(strings.TrimPrefix(fmt.Sprintf("%T", n), "*ast."), "Stmt")
	return "a " + strings.ToLower(name)
}
