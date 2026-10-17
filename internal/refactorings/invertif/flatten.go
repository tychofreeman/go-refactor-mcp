package invertif

// Un-nesting: moving a branch's statements out of an if statement, into
// the block that contains it, right after the if.
//
// Three refactorings do this:
//
//   - a guard clause: `if c { A }` as the last statement of a function
//     without results (or of a loop body) becomes `if !c { return }` (or
//     continue) followed by A;
//   - invert_if on an if/else whose else branch cannot fall through:
//     `if c { A } else { B; return }` becomes `if !c { B; return }` then A;
//   - remove_else on an if/else whose if branch cannot fall through:
//     `if c { X; return } else { B }` becomes `if c { X; return }` then B.
//
// The moved statements run in exactly the same cases as before, because
// the branch that stays in the if never falls through to them. What can
// change is what names mean: the moved statements' declarations now belong
// to the enclosing block. checkMove refuses when that matters.

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
)

// place is where an if statement sits: the statement list that contains
// it, the node owning that list, and the block's scope.
type place struct {
	list  []ast.Stmt
	index int
	owner ast.Node // *ast.BlockStmt, *ast.CaseClause or *ast.CommClause
	scope *types.Scope
	fn    ast.Node // the innermost function (*ast.FuncDecl or *ast.FuncLit)
}

// placeOf finds where s sits, or explains why it is not directly in a
// block (an else-if, say).
func (x *inversion) placeOf(s *ast.IfStmt) (*place, error) {
	path := analysis.PathTo(x.file, s)
	if len(path) < 2 {
		return nil, fmt.Errorf("internal error: if statement not found in file syntax")
	}
	p := &place{owner: path[1]}
	switch o := path[1].(type) {
	case *ast.BlockStmt:
		p.list = o.List
	case *ast.CaseClause:
		p.list = o.Body
	case *ast.CommClause:
		p.list = o.Body
	case *ast.IfStmt:
		return nil, refactor.Refusef("the if at line %d is an else-if, so its branches cannot move out of the if/else chain", x.tf.Line(s.If))
	default:
		return nil, refactor.Refusef("the if at line %d is not directly inside a block (it is labeled, say), so its branches cannot move after it", x.tf.Line(s.If))
	}
	for i, st := range p.list {
		if st == s {
			p.index = i
		}
	}
	for _, n := range path[1:] {
		switch n.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			if p.fn == nil {
				p.fn = n
			}
		}
	}
	p.scope = x.info.Scopes[p.owner]
	if p.scope == nil { // a function body shares the function's scope
		switch f := p.fn.(type) {
		case *ast.FuncDecl:
			p.scope = x.info.Scopes[f.Type]
		case *ast.FuncLit:
			p.scope = x.info.Scopes[f.Type]
		}
	}
	if p.scope == nil {
		return nil, fmt.Errorf("internal error: no scope for the block containing line %d", x.tf.Line(s.If))
	}
	// The moved text is re-indented relative to the if, which must start
	// its own line.
	ls := x.off(x.tf.LineStart(x.tf.Line(s.Pos())))
	if strings.TrimSpace(string(x.src[ls:x.off(s.Pos())])) != "" {
		return nil, refactor.Refusef("the if at line %d does not start its own line; format the file first", x.tf.Line(s.If))
	}
	return p, nil
}

// following returns the statements after the if in its block.
func (p *place) following() []ast.Stmt { return p.list[p.index+1:] }

// last reports whether the if is the last statement of its block.
func (p *place) last() bool {
	for _, st := range p.following() {
		if _, empty := st.(*ast.EmptyStmt); !empty {
			return false
		}
	}
	return true
}

// checkMove refuses moving the statements of block (a branch of s) after
// s when that could change what names mean or break a goto.
func (x *inversion) checkMove(s *ast.IfStmt, block *ast.BlockStmt, p *place) error {
	line := x.tf.Line(s.If)
	if err := x.checkInitUse(s, block); err != nil {
		return err
	}

	// The branch's own declarations move to the enclosing block.
	inner := x.info.Scopes[block]
	if inner == nil {
		return nil // declares nothing
	}
	declaresVar := false
	for _, name := range inner.Names() {
		o := inner.Lookup(name)
		if _, ok := o.(*types.Var); ok {
			declaresVar = true
		}
		if other := p.scope.Lookup(name); other != nil {
			return refactor.Refusef("the statements that would move out of the if at line %d declare %s, which the enclosing block already declares (line %d); rename one first",
				line, name, x.tf.Line(other.Pos()))
		}
		// Later statements must not start seeing the moved declaration.
		for _, st := range p.following() {
			var err error
			ast.Inspect(st, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident)
				if !ok || err != nil || id.Name != name {
					return err == nil
				}
				if u := x.info.Uses[id]; u != nil && (u.Pos() < st.Pos() || u.Pos() >= st.End()) {
					err = refactor.Refusef("the statements that would move out of the if at line %d declare %s, and the code after the if uses another %s (line %d), which the moved declaration would hide; rename one first",
						line, name, name, x.tf.Line(id.Pos()))
				}
				return err == nil
			})
			if err != nil {
				return err
			}
		}
	}
	// Go forbids a goto that jumps over a variable declaration into its
	// scope; moved declarations could create one.
	if declaresVar && p.fn != nil {
		labels := map[types.Object]token.Pos{}
		var gotos []*ast.BranchStmt
		ast.Inspect(p.fn, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.LabeledStmt:
				labels[x.info.Defs[n.Label]] = n.Pos()
			case *ast.BranchStmt:
				if n.Tok == token.GOTO {
					gotos = append(gotos, n)
				}
			}
			return true
		})
		for _, g := range gotos {
			if at, ok := labels[x.info.Uses[g.Label]]; ok && g.Pos() < s.Pos() && at > s.End() {
				return refactor.Refusef("the goto at line %d jumps over the if at line %d; moving declarations out of the if would make it jump over them, which Go does not allow", x.tf.Line(g.Pos()), line)
			}
		}
	}
	return nil
}

// checkInitUse refuses when n, which is moving out of the if s, uses
// variables of s's init statement: they are only in scope inside the if.
func (x *inversion) checkInitUse(s *ast.IfStmt, n ast.Node) error {
	if s.Init == nil {
		return nil
	}
	var err error
	ast.Inspect(n, func(c ast.Node) bool {
		id, ok := c.(*ast.Ident)
		if !ok || err != nil {
			return err == nil
		}
		if o := x.info.Uses[id]; o != nil && o.Pos() >= s.Init.Pos() && o.Pos() < s.Init.End() {
			err = refactor.Refusef("the statements that would move out of the if at line %d use %s, declared by its init statement, which is only in scope inside the if. Move the init statement before the if first",
				x.tf.Line(s.If), id.Name)
		}
		return err == nil
	})
	return err
}

// noCommentsBetween refuses when comments sit between the end of the if
// branch and the start of the else branch, which the edit would drop.
func (x *inversion) noCommentsBetween(s *ast.IfStmt) error {
	from, to := x.off(s.Body.Rbrace), x.off(s.Else.Pos())
	if b, ok := s.Else.(*ast.BlockStmt); ok {
		to = x.off(b.Lbrace)
	}
	for _, t := range textutil.Scan(x.src[from:to]) {
		if t.Tok == token.COMMENT {
			return refactor.Refusef("there is a comment between the if branch and the else at line %d; move it first", x.tf.Line(s.Body.Rbrace))
		}
	}
	return nil
}

// indentOf returns the indentation of the line containing p.
func (x *inversion) indentOf(p token.Pos) string {
	start := x.off(x.tf.LineStart(x.tf.Line(p)))
	ws := x.src[start:x.off(p)]
	for i, c := range ws {
		if c != ' ' && c != '\t' {
			return string(ws[:i])
		}
	}
	return string(ws)
}

// dedented returns the statements of block as lines indented like the if
// at indent (one level less than inside the block), or "" if it is empty.
// A comment on the line of the opening brace is kept, on its own line.
func (x *inversion) dedented(block *ast.BlockStmt, indent string) string {
	inner := string(x.src[x.off(block.Lbrace)+1 : x.off(block.Rbrace)])
	nl := strings.IndexByte(inner, '\n')
	if nl < 0 { // a one-line block
		if t := strings.TrimSpace(inner); t != "" {
			return indent + t
		}
		return ""
	}
	first := strings.TrimSpace(inner[:nl])
	rest := inner[nl+1:]
	if i := strings.LastIndexByte(rest, '\n'); i >= 0 && strings.TrimSpace(rest[i+1:]) == "" {
		rest = rest[:i] // the closing brace's indentation
	} else if strings.TrimSpace(rest) == "" {
		rest = ""
	}
	body := textutil.Reindent(rest, indent+"\t", indent)
	if first != "" {
		if body != "" {
			body = indent + first + "\n" + body
		} else {
			body = indent + first
		}
	}
	if strings.TrimSpace(body) == "" {
		return ""
	}
	return body
}

// blockText returns the text between a block's braces, unchanged.
func (x *inversion) blockText(b *ast.BlockStmt) string {
	return string(x.src[x.off(b.Lbrace)+1 : x.off(b.Rbrace)])
}

// header returns the if statement's text up to its condition: "if " or
// "if init; ".
func (x *inversion) header(s *ast.IfStmt) string {
	return string(x.src[x.off(s.Pos()):x.off(s.Cond.Pos())])
}

// then joins the if statement's new text and the moved lines.
func then(ifText, moved string) string {
	if moved == "" {
		return ifText
	}
	return ifText + "\n" + moved
}
