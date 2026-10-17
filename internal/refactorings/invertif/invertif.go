// Package invertif implements "invert if": negate an if statement's
// condition and swap its branches.
//
//	if a == b {        if a != b {
//		A                  B
//	} else {     =>    } else {
//		B                  A
//	}                  }
//
// Swapping the branches is trivially behavior-preserving; the work is in
// negating the condition so that it reads naturally and still means
// exactly the opposite (see negate). The branch contents move as text, so
// comments survive, and both branches sit at the same depth, so nothing
// is re-indented. An init statement stays where it is.
package invertif

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// InvertIf is the "invert_if" refactoring.
type InvertIf struct{}

// New returns the invert_if refactoring.
func New() *InvertIf { return &InvertIf{} }

func (*InvertIf) Name() string { return "invert_if" }

func (*InvertIf) Description() string {
	return `Invert an if/else statement: negate its condition and swap the two branches.
Use it to put the short or exceptional branch first, which usually reads better and prepares for removing nesting.
Identify the if by file and the line of its "if" keyword (as shown by inspect); add column only if that line has more than one if.
The negated condition is simplified where that is exact: == and != swap, <, <=, >, >= flip for integers and strings (not floats, where NaN makes !(a < b) differ from a >= b), !x becomes x, and && and || use De Morgan's laws (evaluation order is unchanged).
Refuses an if without an else, and an else-if chain in the else branch.`
}

func (*InvertIf) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":   {"type": "string",  "description": "Path of the file, relative to the module root."},
    "line":   {"type": "integer", "description": "1-based line of the if keyword."},
    "column": {"type": "integer", "description": "Optional 1-based column of the if keyword, only needed if the line has more than one if."}
  },
  "required": ["file", "line"],
  "additionalProperties": false
}`)
}

type args struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column,omitempty"`
}

type inversion struct {
	ws   *workspace.Workspace
	info *types.Info
	file *ast.File
	tf   *token.File
	src  []byte
	path string
	rel  string
}

// Plan checks preconditions and computes the edits.
func (*InvertIf) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	switch {
	case a.File == "":
		return nil, refactor.Refusef("file is required")
	case a.Line < 1:
		return nil, refactor.Refusef("line is required (1-based)")
	}
	path := ws.Abs(a.File)
	pkg, file := ws.FileSyntax(path)
	if file == nil {
		return nil, refactor.Refusef("file %s is not part of the module's packages", a.File)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	x := &inversion{ws: ws, info: pkg.TypesInfo, file: file, tf: ws.Fset.File(file.Pos()), src: src, path: path, rel: a.File}
	s, err := x.find(a)
	if err != nil {
		return nil, err
	}
	return x.plan(s)
}

// find returns the if statement whose if keyword is at the line (and
// column, if given).
func (x *inversion) find(a args) (*ast.IfStmt, error) {
	if a.Line > x.tf.LineCount() {
		return nil, refactor.Refusef("line %d is past the end of %s, which has %d lines", a.Line, a.File, x.tf.LineCount())
	}
	var found []*ast.IfStmt
	ast.Inspect(x.file, func(n ast.Node) bool {
		if s, ok := n.(*ast.IfStmt); ok && x.tf.Line(s.If) == a.Line {
			if a.Column == 0 || x.tf.Position(s.If).Column == a.Column {
				found = append(found, s)
			}
		}
		return true
	})
	switch {
	case len(found) == 1:
		return found[0], nil
	case len(found) > 1:
		var cols []string
		for _, s := range found {
			cols = append(cols, fmt.Sprint(x.tf.Position(s.If).Column))
		}
		return nil, refactor.Refusef("line %d has %d if statements (columns %s); pass column to choose one", a.Line, len(found), strings.Join(cols, ", "))
	case a.Column != 0:
		return nil, refactor.Refusef("no if statement starts at %s:%d:%d", a.File, a.Line, a.Column)
	}
	return nil, refactor.Refusef("no if statement starts on line %d of %s; give the line of the if keyword%s", a.Line, a.File, x.lineHint(a.Line))
}

func (x *inversion) plan(s *ast.IfStmt) (*refactor.Plan, error) {
	line := x.tf.Line(s.If)
	var els *ast.BlockStmt
	switch e := s.Else.(type) {
	case nil:
		return nil, refactor.Refusef("the if at line %d has no else branch, so there is nothing to swap. (Turning it into a guard clause is not supported yet.)", line)
	case *ast.IfStmt:
		return nil, refactor.Refusef("the if at line %d is followed by an else-if chain; swapping it in would add nesting. Invert one of the ifs in the chain that has a plain else instead", line)
	case *ast.BlockStmt:
		els = e
	}
	neg := negate(x, s.Cond)
	thenIn := x.inner(s.Body)
	elseIn := x.inner(els)
	edits := []edit.TextEdit{
		{Start: x.off(s.Cond.Pos()), End: x.off(s.Cond.End()), NewText: neg.text},
		{Start: x.off(s.Body.Lbrace) + 1, End: x.off(s.Body.Rbrace), NewText: elseIn},
		{Start: x.off(els.Lbrace) + 1, End: x.off(els.Rbrace), NewText: thenIn},
	}
	return &refactor.Plan{
		Summary: fmt.Sprintf("inverted the if at line %d: condition %s is now %s, and the branches are swapped",
			line, quote(x.text(s.Cond)), quote(neg.text)),
		Edits: []edit.FileEdit{{Path: x.path, Edits: edits}},
	}, nil
}

// inner returns the text between a block's braces.
func (x *inversion) inner(b *ast.BlockStmt) string {
	return string(x.src[x.off(b.Lbrace)+1 : x.off(b.Rbrace)])
}

func (x *inversion) off(p token.Pos) int { return x.tf.Offset(p) }

func (x *inversion) text(n ast.Node) string {
	return string(x.src[x.off(n.Pos()):x.off(n.End())])
}

func (x *inversion) lineHint(line int) string {
	s := x.src[x.off(x.tf.LineStart(line)):]
	if i := strings.IndexByte(string(s), '\n'); i >= 0 {
		s = s[:i]
	}
	return fmt.Sprintf("; that line is: %s", strings.TrimSpace(string(s)))
}

func quote(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 60 {
		s = s[:57] + "..."
	}
	return "`" + s + "`"
}

// ---------------------------------------------------------------------------
// Negation

// expr is a piece of Go expression text with the precedence of its
// outermost operator, as go/token numbers them (|| is 1, && is 2,
// comparisons 3, + 4, * 5); unary operators bind tighter than any binary
// operator, and operands tightest of all.
type expr struct {
	text string
	prec int
}

const (
	unaryPrec   = 6
	operandPrec = 7
)

// operand returns e's own text and precedence, without outer parentheses.
func operand(x *inversion, e ast.Expr) expr {
	e = ast.Unparen(e)
	switch e := e.(type) {
	case *ast.BinaryExpr:
		return expr{x.text(e), e.Op.Precedence()}
	case *ast.UnaryExpr:
		return expr{x.text(e), unaryPrec}
	}
	return expr{x.text(e), operandPrec}
}

// paren parenthesizes e if it binds less tightly than prec.
func paren(e expr, prec int) string {
	if e.prec < prec {
		return "(" + e.text + ")"
	}
	return e.text
}

// negate returns an expression that is true exactly when e is false, and
// evaluates the same operands in the same order under the same conditions:
//
//   - !x becomes x;
//   - == and != swap (exact for every type, NaN included);
//   - <, <=, >, >= flip only when both operands are integers or strings:
//     for floats, a comparison with NaN is false both ways, so !(a < b) is
//     not a >= b;
//   - !(a && b) becomes !a || !b and !(a || b) becomes !a && !b. Each side
//     is still evaluated left to right, and the right side only when the
//     left does not decide the result, exactly as before.
//
// Anything else becomes !e, parenthesized as needed.
func negate(x *inversion, e ast.Expr) expr {
	e = ast.Unparen(e)
	switch e := e.(type) {
	case *ast.UnaryExpr:
		if e.Op == token.NOT {
			return operand(x, e.X)
		}
	case *ast.BinaryExpr:
		switch e.Op {
		case token.LAND:
			l, r := negate(x, e.X), negate(x, e.Y)
			return expr{paren(l, token.LOR.Precedence()) + " || " + paren(r, token.LOR.Precedence()), token.LOR.Precedence()}
		case token.LOR:
			l, r := negate(x, e.X), negate(x, e.Y)
			return expr{paren(l, token.LAND.Precedence()) + " && " + paren(r, token.LAND.Precedence()), token.LAND.Precedence()}
		case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			if op, ok := complement(x, e); ok {
				return expr{x.text(e.X) + " " + op.String() + " " + x.text(e.Y), op.Precedence()}
			}
		}
	}
	o := operand(x, e)
	return expr{"!" + paren(o, unaryPrec), unaryPrec}
}

// complement returns the comparison operator that is true exactly when
// e's is false, if there is one for e's operand types.
func complement(x *inversion, e *ast.BinaryExpr) (token.Token, bool) {
	switch e.Op {
	case token.EQL:
		return token.NEQ, true
	case token.NEQ:
		return token.EQL, true
	}
	if !totallyOrdered(x.info.TypeOf(e.X)) || !totallyOrdered(x.info.TypeOf(e.Y)) {
		return 0, false
	}
	switch e.Op {
	case token.LSS:
		return token.GEQ, true
	case token.LEQ:
		return token.GTR, true
	case token.GTR:
		return token.LEQ, true
	case token.GEQ:
		return token.LSS, true
	}
	return 0, false
}

// totallyOrdered reports whether t's values are totally ordered by <, so
// that !(a < b) equals a >= b: integers and strings, but not floats (NaN).
// Type parameters count as not ordered: their type sets (cmp.Ordered, say)
// usually include floats.
func totallyOrdered(t types.Type) bool {
	if t == nil {
		return false
	}
	if _, ok := t.(*types.TypeParam); ok {
		return false
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&(types.IsInteger|types.IsString) != 0
}
