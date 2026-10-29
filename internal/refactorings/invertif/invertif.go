// Package invertif implements "invert if": negate an if statement's
// condition and swap its branches, and the guard clauses that this
// enables (see flatten.go); and "remove else".
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
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/site"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// InvertIf is the "invert_if" refactoring.
type InvertIf struct{}

// New returns the invert_if refactoring.
func New() *InvertIf { return &InvertIf{} }

func (*InvertIf) Name() string { return "invert_if" }

func (*InvertIf) Description() string {
	return `Negate an if's condition and swap its branches, turning nesting into guard clauses: if c { A } else { return } becomes if !c { return } followed by A; in a loop body, a last if x > 0 { n += x } becomes if x <= 0 { continue } followed by n += x. keep_else only swaps the branches.
An if without else is un-nested this way when it ends a function without results (return), a loop body (continue) or a case (break), or when only the function's final return follows it. The negation is simplified where that is exact (== and != swap, < and >= swap for integers and strings, De Morgan for && and ||).
Identify the if by file and the line of its if keyword (column if the line has two).
Refuses, saying why: an else-if chain in the else branch, an if without else anywhere else, un-nesting that would make names clash.`
}

func (*InvertIf) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":   {"type": "string",  "description": "Path of the file, relative to the module root."},
    "line":   {"type": "integer", "description": "1-based line of the if keyword."},
    "column": {"type": "integer", "description": "Optional 1-based column of the if keyword, only needed if the line has more than one if."},
    "keep_else": {"type": "boolean", "description": "Only swap the branches of an if/else; do not remove the else even if the new first branch exits."}
  },
  "required": ["file", "line"],
  "additionalProperties": false
}`)
}

type args struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Column   int    `json:"column,omitempty"`
	KeepElse bool   `json:"keep_else,omitempty"`
	Wrap     bool   `json:"wrap,omitempty"`
}

type inversion struct {
	site.Site
	path string
	rel  string
}

// Plan checks preconditions and computes the edits.
func (*InvertIf) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.Wrap {
		return nil, refactor.Refusef("invalid arguments: wrap is a remove_else argument")
	}
	x, s, err := load(ws, a)
	if err != nil {
		return nil, err
	}
	return x.plan(s, a.KeepElse)
}

// load finds the if statement the arguments identify.
func load(ws *workspace.Workspace, a args) (*inversion, *ast.IfStmt, error) {
	switch {
	case a.File == "":
		return nil, nil, refactor.Refusef("file is required")
	case a.Line < 1:
		return nil, nil, refactor.Refusef("line is required (1-based)")
	}
	path := ws.Abs(a.File)
	at, err := site.At(ws, path)
	if err != nil {
		return nil, nil, err
	}
	x := &inversion{Site: *at, path: path, rel: a.File}
	s, err := x.find(a)
	if err != nil {
		return nil, nil, err
	}
	return x, s, nil
}

// find returns the if statement whose if keyword is at the line (and
// column, if given).
func (x *inversion) find(a args) (*ast.IfStmt, error) {
	if a.Line > x.TF.LineCount() {
		return nil, refactor.Refusef("line %d is past the end of %s, which has %d lines", a.Line, a.File, x.TF.LineCount())
	}
	var found []*ast.IfStmt
	ast.Inspect(x.File, func(n ast.Node) bool {
		if s, ok := n.(*ast.IfStmt); ok && x.TF.Line(s.If) == a.Line {
			if a.Column == 0 || x.TF.Position(s.If).Column == a.Column {
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
			cols = append(cols, fmt.Sprint(x.TF.Position(s.If).Column))
		}
		return nil, refactor.Refusef("line %d has %d if statements (columns %s); pass column to choose one", a.Line, len(found), strings.Join(cols, ", "))
	case a.Column != 0:
		return nil, refactor.Refusef("no if statement starts at %s:%d:%d", a.File, a.Line, a.Column)
	}
	return nil, refactor.Refusef("no if statement starts on line %d of %s; give the line of the if keyword%s", a.Line, a.File, x.lineHint(a.Line))
}

func (x *inversion) plan(s *ast.IfStmt, keepElse bool) (*refactor.Plan, error) {
	line := x.TF.Line(s.If)
	var els *ast.BlockStmt
	switch e := s.Else.(type) {
	case nil:
		return x.guard(s)
	case *ast.IfStmt:
		return nil, refactor.Refusef("the if at line %d is followed by an else-if chain; swapping it in would add nesting. Invert one of the ifs in the chain that has a plain else instead", line)
	case *ast.BlockStmt:
		els = e
	}
	neg := negate(x, s.Cond)
	var notes []string
	if !keepElse && analysis.EndsFlow(x.Info, els.List) {
		// The else branch becomes the if branch and cannot fall through,
		// so the old if branch can follow the if, un-nested.
		p, err := x.placeOf(s)
		if err == nil {
			err = x.noCommentsBetween(s)
		}
		hoist := false
		if err == nil {
			hoist, err = x.checkMove(s, s.Body, p)
		}
		if err == nil {
			indent := x.indentOf(s.Pos())
			text := then(x.head(s, hoist, indent)+neg.text+" {"+x.blockText(els)+"}", x.dedented(s.Body, indent))
			return &refactor.Plan{
				Summary: fmt.Sprintf("inverted the if at line %d into a guard clause: condition %s is now %s, and since that branch exits, the else is removed and the former if branch follows, un-nested",
					line, quote(x.Text(s.Cond)), quote(neg.text)),
				Edits: []edit.FileEdit{{Path: x.path, Edits: []edit.TextEdit{{Start: x.Off(s.Pos()), End: x.Off(s.End()), NewText: text}}}},
			}, nil
		}
		if !refactor.IsRefusal(err) {
			return nil, err
		}
		notes = append(notes, "the else was kept because "+lowerFirst(err.Error()))
	}
	thenIn := x.inner(s.Body)
	elseIn := x.inner(els)
	edits := []edit.TextEdit{
		{Start: x.Off(s.Cond.Pos()), End: x.Off(s.Cond.End()), NewText: neg.text},
		{Start: x.Off(s.Body.Lbrace) + 1, End: x.Off(s.Body.Rbrace), NewText: elseIn},
		{Start: x.Off(els.Lbrace) + 1, End: x.Off(els.Rbrace), NewText: thenIn},
	}
	return &refactor.Plan{
		Summary: fmt.Sprintf("inverted the if at line %d: condition %s is now %s, and the branches are swapped",
			line, quote(x.Text(s.Cond)), quote(neg.text)),
		Edits: []edit.FileEdit{{Path: x.path, Edits: edits}},
		Notes: notes,
	}, nil
}

// guard turns an if without else, the last statement of a function
// without results or of a loop body, into a guard clause.
func (x *inversion) guard(s *ast.IfStmt) (*refactor.Plan, error) {
	line := x.TF.Line(s.If)
	p, err := x.placeOf(s)
	if err != nil {
		return nil, err
	}
	if analysis.EndsFlow(x.Info, s.Body.List) {
		return nil, refactor.Refusef("the if at line %d already exits (its body ends in return, continue, break, goto or panic), so it is already a guard clause; inverting it would only move the code after it inside", line)
	}
	indent := x.indentOf(s.Pos())
	exit, exitText, err := x.exit(s, p, indent)
	if err != nil {
		return nil, err
	}
	if len(s.Body.List) == 0 {
		return nil, refactor.Refusef("the if at line %d has an empty body; remove it instead", line)
	}
	hoist, err := x.checkMove(s, s.Body, p)
	if err != nil {
		return nil, err
	}
	neg := negate(x, s.Cond)
	text := then(x.head(s, hoist, indent)+neg.text+" {\n"+exitText+"\n"+indent+"}", x.dedented(s.Body, indent))
	var notes []string
	if hoist {
		notes = append(notes, "the if's init statement moved before it, because the former body uses its variables")
	}
	return &refactor.Plan{
		Summary: fmt.Sprintf("turned the if at line %d into a guard clause: if %s { %s }, followed by its former body, un-nested",
			line, neg.text, exit),
		Edits: []edit.FileEdit{{Path: x.path, Edits: []edit.TextEdit{{Start: x.Off(s.Pos()), End: x.Off(s.End()), NewText: text}}}},
		Notes: notes,
	}, nil
}

// exit returns how a guard clause for s leaves early (for the summary),
// and the guard's body text, indented one level deeper than indent:
//
//   - return, when s is the last statement of a function without results;
//   - continue, when it is the last statement of a loop body;
//   - break, when it is the last statement of a switch or select case;
//   - a copy of the return statement, when s is followed only by a return
//     at the end of a function: every path still evaluates it once.
func (x *inversion) exit(s *ast.IfStmt, p *place, indent string) (string, string, error) {
	line := x.TF.Line(s.If)
	simple := func(e string) (string, string, error) { return e, indent + "\t" + e, nil }
	body := x.funcBody(p)
	if p.last() {
		switch o := p.owner.(type) {
		case *ast.CaseClause, *ast.CommClause:
			return simple("break")
		case *ast.BlockStmt:
			if body == o {
				if x.funcResults(p) == 0 {
					return simple("return")
				}
				break // a function with results cannot end with this if
			}
			switch l := analysis.PathTo(x.File, o)[1].(type) {
			case *ast.ForStmt:
				if l.Body == o {
					return simple("continue")
				}
			case *ast.RangeStmt:
				if l.Body == o {
					return simple("continue")
				}
			}
		}
		return "", "", refactor.Refusef("the if at line %d has no else, and it is not the last statement of a function, loop body or case, so there is no return, continue or break to exit with", line)
	}
	// Followed only by the function's final return: copy it into the guard.
	var rest []ast.Stmt
	for _, st := range p.following() {
		if _, empty := st.(*ast.EmptyStmt); !empty {
			rest = append(rest, st)
		}
	}
	ret, isRet := rest[0].(*ast.ReturnStmt)
	if len(rest) != 1 || !isRet || body == nil || body != p.owner {
		return "", "", refactor.Refusef("the if at line %d has no else, and the code after it is not just the function's final return, so it cannot become a guard clause: that code runs whether or not the condition holds", line)
	}
	// Inside the if, the copied return must mean the same: the init
	// statement's variables would hide outer names it uses.
	if s.Init != nil {
		var err error
		ast.Inspect(ret, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && err == nil {
				if sc := x.Info.Scopes[s]; sc != nil {
					if o := sc.Lookup(id.Name); o != nil && x.Info.Uses[id] != o {
						err = refactor.Refusef("the guard clause would repeat the return at line %d inside the if at line %d, where %s refers to the variable from the if's init statement, not the one the return uses; rename one first",
							x.TF.Line(ret.Pos()), line, id.Name)
					}
				}
			}
			return err == nil
		})
		if err != nil {
			return "", "", err
		}
	}
	ls := x.Off(x.TF.LineStart(x.TF.Line(ret.Pos())))
	text := textutil.Reindent(string(x.Src[ls:x.Off(ret.End())]), indent, indent+"\t")
	return "return (repeating the final return)", text, nil
}

// funcBody returns the body of the innermost function containing the if.
func (x *inversion) funcBody(p *place) *ast.BlockStmt {
	switch f := p.fn.(type) {
	case *ast.FuncDecl:
		return f.Body
	case *ast.FuncLit:
		return f.Body
	}
	return nil
}

// funcResults returns how many results the innermost function has.
func (x *inversion) funcResults(p *place) int {
	var ft *ast.FuncType
	switch f := p.fn.(type) {
	case *ast.FuncDecl:
		ft = f.Type
	case *ast.FuncLit:
		ft = f.Type
	}
	if ft == nil || ft.Results == nil {
		return 0
	}
	return ft.Results.NumFields()
}

// returnFrom returns "return" for a function without results, or "".
func (x *inversion) returnFrom(ft *ast.FuncType) string {
	if ft.Results == nil || len(ft.Results.List) == 0 {
		return "return"
	}
	return ""
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// inner returns the text between a block's braces.
func (x *inversion) inner(b *ast.BlockStmt) string {
	return string(x.Src[x.Off(b.Lbrace)+1 : x.Off(b.Rbrace)])
}

func (x *inversion) lineHint(line int) string {
	s := x.Src[x.Off(x.TF.LineStart(line)):]
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
		return expr{x.Text(e), e.Op.Precedence()}
	case *ast.UnaryExpr:
		return expr{x.Text(e), unaryPrec}
	}
	return expr{x.Text(e), operandPrec}
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
				return expr{x.Text(e.X) + " " + op.String() + " " + x.Text(e.Y), op.Precedence()}
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
	if !totallyOrdered(x.Info.TypeOf(e.X)) || !totallyOrdered(x.Info.TypeOf(e.Y)) {
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
