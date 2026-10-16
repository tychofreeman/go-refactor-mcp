// Package extractfunc implements "extract function": move a run of
// complete statements from one block into a new package-level function,
// and call it in their place.
//
// The statements run in a different function afterwards, so most of this
// file checks what that can change:
//
//   - selection: the lines must cover whole statements of a single block,
//     plus optional comments; nothing else;
//   - control flow: defer, recover, fallthrough, and break, continue or
//     goto that leave the selection behave differently (or not at all) in
//     another function; return statements are passed on by the call site
//     in the two shapes described in returns.go, and refused otherwise;
//   - data flow: local variables the statements use become parameters, and
//     variables they change or declare that are used afterwards become
//     results. Parameters and results are copies, so variables that other
//     code can reach while the statements run (address taken, captured by
//     a closure) are refused;
//   - names: the new function must not clash with or hide another name in
//     the package, and must not be hidden at the call site.
//
// The edits move the original source text (so comments survive) and
// re-indent it; gofmt tidies the rest if the file was gofmt-clean.
package extractfunc

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/scanner"
	"go/token"
	"go/types"
	"os"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/packages"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// ExtractFunction is the "extract_function" refactoring.
type ExtractFunction struct{}

// New returns the extract_function refactoring.
func New() *ExtractFunction { return &ExtractFunction{} }

func (*ExtractFunction) Name() string { return "extract_function" }

func (*ExtractFunction) Description() string {
	return `Move a run of complete statements into a new function, declared after the enclosing function, and call it in their place.
Use it to split a long function into named steps.
Identify the statements by file, start_line and end_line (inclusive, as shown by inspect). The lines must cover whole statements of one block; comments and blank lines in the range are allowed and comments move with the code.
Local variables the statements use become parameters; variables they change or declare that are used afterwards become results, assigned at the call site. The result notes the parameters and results chosen.
Return statements are supported in two shapes: if every path through the statements returns, the call site becomes "return f(...)"; if the enclosing function returns an error and every return in the statements is an early exit with a non-nil error (like "if err != nil { return nil, err }"), the new function also returns an error and the call site checks it.
Refuses other returns (for example an early return of a success value), defer, recover() or fallthrough, or a break, continue or goto that leaves the selected lines; if a variable they share with the rest of the function has its address taken or is used by a function literal (the new function would work on a copy); or if they use constants or types declared inside the function.`
}

func (*ExtractFunction) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file":       {"type": "string",  "description": "Path of the file, relative to the module root."},
    "start_line": {"type": "integer", "description": "1-based first line of the statements to extract."},
    "end_line":   {"type": "integer", "description": "1-based last line of the statements to extract (inclusive)."},
    "name":       {"type": "string",  "description": "Name for the new function."}
  },
  "required": ["file", "start_line", "end_line", "name"],
  "additionalProperties": false
}`)
}

type args struct {
	File      string `json:"file"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Name      string `json:"name"`
}

// extraction holds the state of one planning run.
type extraction struct {
	ws   *workspace.Workspace
	pkg  *packages.Package
	info *types.Info
	file *ast.File
	tf   *token.File
	src  []byte
	path string // absolute
	rel  string // as given
	name string

	stmts []ast.Stmt // the selected statements, in order
	list  []ast.Stmt // the statement list they belong to
	block ast.Node   // the node owning list: *ast.BlockStmt, *ast.CaseClause or *ast.CommClause

	start, end int // byte range of the selected text: whole lines, from the first to the last token

	fa   *analysis.Func // facts about the outermost enclosing function's locals
	flow *flow

	returns    []*ast.ReturnStmt // in the selection, outside function literals
	mode       returnMode
	exitValues []string // errorExit: the non-error values the early returns return

	toks []tok // the file's tokens, scanned once
}

// Plan checks preconditions and computes the edits.
func (*ExtractFunction) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	switch {
	case a.File == "":
		return nil, refactor.Refusef("file is required")
	case a.StartLine < 1:
		return nil, refactor.Refusef("start_line is required (1-based)")
	case a.EndLine < 1:
		return nil, refactor.Refusef("end_line is required (1-based)")
	case a.EndLine < a.StartLine:
		return nil, refactor.Refusef("end_line (%d) is before start_line (%d)", a.EndLine, a.StartLine)
	}
	if err := checkName(a.Name); err != nil {
		return nil, err
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
	x := &extraction{
		ws: ws, pkg: pkg, info: pkg.TypesInfo, file: file, src: src,
		path: path, rel: a.File, name: a.Name,
		tf: ws.Fset.File(file.Pos()),
	}
	if a.EndLine > x.tf.LineCount() {
		return nil, refactor.Refusef("end_line %d is past the end of %s, which has %d lines", a.EndLine, a.File, x.tf.LineCount())
	}
	if err := x.selectStatements(a.StartLine, a.EndLine); err != nil {
		return nil, err
	}
	x.fa = analysis.AnalyzeFunc(x.info, x.outermostFunc())
	if err := x.checkControlFlow(); err != nil {
		return nil, err
	}
	if err := x.analyzeDataFlow(); err != nil {
		return nil, err
	}
	if err := x.checkNewName(); err != nil {
		return nil, err
	}
	return x.plan()
}

func checkName(name string) error {
	switch {
	case name == "":
		return refactor.Refusef("name is required")
	case name == "_":
		return refactor.Refusef("the blank identifier cannot name a function that is called; choose a name")
	case !token.IsIdentifier(name):
		return refactor.Refusef("%q is not a valid Go identifier", name)
	case name == "init":
		return refactor.Refusef("init is reserved for package initialization functions, which cannot be called; choose another name")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Selection

// selectStatements finds the statements covered by lines [first, last].
//
// It picks the outermost statement list in which every statement touching
// the lines lies entirely within them: selecting a whole if statement
// extracts the if, not its body. Then it checks that the lines contain
// nothing but those statements and comments.
func (x *extraction) selectStatements(first, last int) error {
	var lists []stmtList
	ast.Inspect(x.file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.BlockStmt:
			// The body of a switch or select lists case clauses, not
			// statements; the clauses' own bodies are added below.
			if len(n.List) > 0 {
				switch n.List[0].(type) {
				case *ast.CaseClause, *ast.CommClause:
					return true
				}
			}
			lists = append(lists, stmtList{n, n.List})
		case *ast.CaseClause:
			lists = append(lists, stmtList{n, n.Body})
		case *ast.CommClause:
			lists = append(lists, stmtList{n, n.Body})
		}
		return true
	})

	var touched []stmtList // lists with a statement touching the lines, outermost first
	for _, l := range lists {
		var sel []ast.Stmt
		whole := true
		for _, s := range l.stmts {
			sl, el := x.line(s.Pos()), x.line(s.End())
			if el < first || sl > last {
				continue
			}
			sel = append(sel, s)
			if sl < first || el > last {
				whole = false
			}
		}
		if len(sel) == 0 {
			continue
		}
		touched = append(touched, l)
		if whole && x.stmts == nil {
			x.stmts, x.list, x.block = sel, l.stmts, l.owner
		}
	}
	if len(touched) == 0 {
		return refactor.Refusef("lines %d-%d of %s contain no statements inside a function body%s",
			first, last, x.rel, x.funcHint(first))
	}
	if x.stmts == nil {
		return refactor.Refusef("lines %d-%d do not cover whole statements of one block. %s",
			first, last, x.statementsAround(touched[len(touched)-1], first))
	}

	// Everything in the lines outside the statements must be a comment.
	lo := x.off(x.tf.LineStart(first))
	hi := len(x.src)
	if last < x.tf.LineCount() {
		hi = x.off(x.tf.LineStart(last+1)) - 1 // the newline ending the last line
	}
	sFrom, sTo := x.off(x.stmts[0].Pos()), x.off(x.stmts[len(x.stmts)-1].End())
	x.start, x.end = -1, -1
	for _, t := range x.tokens() {
		if t.end <= lo || t.off >= hi || t.tok == token.SEMICOLON {
			continue
		}
		inStmts := t.off >= sFrom && t.end <= sTo
		if !inStmts && (t.tok != token.COMMENT || t.off < lo || t.end > hi) {
			l := x.tf.Line(x.tf.Pos(max(t.off, lo)))
			return refactor.Refusef("line %d contains %s, which is not part of the selected statements; the lines must cover whole statements of one block. %s",
				l, quoteText(string(x.src[t.off:t.end])), x.statementsAround(stmtList{x.block, x.list}, first))
		}
		if x.start < 0 {
			x.start = t.off
		}
		x.end = t.end
	}
	// Widen to whole lines: from the start of the first line to the end of
	// the last (without its newline).
	x.start = x.off(x.tf.LineStart(x.line(x.tf.Pos(x.start))))
	if e := strings.IndexByte(string(x.src[x.end:]), '\n'); e >= 0 {
		x.end += e
	} else {
		x.end = len(x.src)
	}
	return nil
}

type stmtList struct {
	owner ast.Node
	stmts []ast.Stmt
}

// statementsAround describes the statements of l near line, so the model
// can choose a valid range in one more step.
func (x *extraction) statementsAround(l stmtList, line int) string {
	i := sort.Search(len(l.stmts), func(i int) bool { return x.line(l.stmts[i].End()) >= line })
	from, to := max(i-3, 0), min(i+5, len(l.stmts))
	var parts []string
	for _, s := range l.stmts[from:to] {
		sl, el := x.line(s.Pos()), x.line(s.End())
		r := fmt.Sprint(sl)
		if el != sl {
			r = fmt.Sprintf("%d-%d", sl, el)
		}
		parts = append(parts, fmt.Sprintf("%s %s", r, x.firstLine(s)))
	}
	return "Statements in the block near there: " + strings.Join(parts, ", ") + "."
}

// funcHint names the function at or near line, if the lines missed its body.
func (x *extraction) funcHint(line int) string {
	for _, d := range x.file.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil && x.line(fd.Pos()) <= line && line <= x.line(fd.End()) {
			return fmt.Sprintf("; the body of %s spans lines %d-%d", fd.Name.Name, x.line(fd.Body.Lbrace)+1, x.line(fd.Body.Rbrace)-1)
		}
	}
	return ""
}

type tok struct {
	off, end int
	tok      token.Token
}

// tokens scans the whole file, including comments.
func (x *extraction) tokens() []tok {
	if x.toks == nil {
		x.toks = scanTokens(x.src)
	}
	return x.toks
}

// scanTokens scans src, including comments.
func scanTokens(src []byte) []tok {
	fs := token.NewFileSet()
	f := fs.AddFile("", -1, len(src))
	var s scanner.Scanner
	s.Init(f, src, nil, scanner.ScanComments)
	var out []tok
	for {
		pos, t, lit := s.Scan()
		if t == token.EOF {
			return out
		}
		off := f.Offset(pos)
		var n int
		switch {
		case t == token.SEMICOLON && lit == "\n":
			n = 0 // inserted automatically at a line end
		case lit != "":
			n = len(lit)
		default:
			n = len(t.String())
		}
		out = append(out, tok{off, off + n, t})
	}
}

// ---------------------------------------------------------------------------
// Control flow

// checkControlFlow refuses statements whose meaning depends on the
// function they are in, or that jump out of the selection.
func (x *extraction) checkControlFlow() error {
	recov := types.Universe.Lookup("recover")
	var err error
	for _, s := range x.stmts {
		var stack []ast.Node
		ast.Inspect(s, func(n ast.Node) bool {
			if err != nil {
				return false
			}
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			switch n := n.(type) {
			case *ast.FuncLit:
				return false // its body is its own function and moves with it
			case *ast.ReturnStmt:
				x.returns = append(x.returns, n)
			case *ast.DeferStmt:
				err = refactor.Refusef("the selected lines contain a defer statement at line %d; in a new function the deferred call would run when the new function returns, not when %s does", x.line(n.Pos()), x.enclosingName())
			case *ast.CallExpr:
				if id, ok := ast.Unparen(n.Fun).(*ast.Ident); ok && x.info.Uses[id] == recov {
					err = refactor.Refusef("the selected lines call recover() at line %d, which only stops a panic when called directly by a deferred function; moving it would change that", x.line(n.Pos()))
				}
			case *ast.BranchStmt:
				err = x.checkBranch(n, stack)
			}
			if err != nil {
				return false
			}
			stack = append(stack, n)
			return true
		})
		if err != nil {
			return err
		}
	}

	// A goto, break or continue outside the selection must not target a
	// label inside it.
	for _, s := range x.stmts {
		ast.Inspect(s, func(n ast.Node) bool {
			ls, ok := n.(*ast.LabeledStmt)
			if !ok || err != nil {
				return err == nil
			}
			obj := x.info.Defs[ls.Label]
			for id, o := range x.info.Uses {
				if o == obj && obj != nil && !x.inSelection(id) {
					err = refactor.Refusef("the label %s at line %d is used at line %d, outside the selected lines; a jump cannot reach into another function",
						ls.Label.Name, x.line(ls.Pos()), x.line(id.Pos()))
					return false
				}
			}
			return true
		})
		if err != nil {
			return err
		}
	}
	return x.decideReturns()
}

// checkTermination refuses a selection whose last statement is what makes
// the enclosing function end in a terminating statement (a panic, a for
// without condition, a select with no cases, ...), as Go requires of a
// function with results: a call to the new function does not count.
func (x *extraction) checkTermination() error {
	var body *ast.BlockStmt
	var sig *types.Signature
	for _, n := range analysis.PathTo(x.file, x.stmts[0]) {
		switch n := n.(type) {
		case *ast.FuncLit:
			body, sig = n.Body, typeOf[*types.Signature](x.info.TypeOf(n))
		case *ast.FuncDecl:
			if obj, ok := x.info.Defs[n.Name].(*types.Func); ok {
				body, sig = n.Body, obj.Signature()
			}
		default:
			continue
		}
		break
	}
	if body == nil || sig == nil || sig.Results().Len() == 0 {
		return nil
	}
	if x.terminatingList(body.List, false) && !x.terminatingList(body.List, true) {
		last := x.stmts[len(x.stmts)-1]
		return refactor.Refusef("the selected lines end with %s at line %d, which is how %s ends without a return; a call to a new function does not count as that, so the code would no longer compile. Leave that statement out of the selection",
			x.firstLine(last), x.line(last.Pos()), x.enclosingName())
	}
	return nil
}

// terminatingList reports whether a statement list ends in a terminating
// statement, as defined by the Go spec. With extracted, the selected
// statements count as already replaced by a call.
func (x *extraction) terminatingList(list []ast.Stmt, extracted bool) bool {
	for i := len(list) - 1; i >= 0; i-- {
		s := list[i]
		if _, empty := s.(*ast.EmptyStmt); empty {
			continue
		}
		if extracted && s == x.stmts[len(x.stmts)-1] {
			return false // the last statement is now the call
		}
		return x.terminating(s, extracted)
	}
	return false
}

func (x *extraction) terminating(s ast.Stmt, extracted bool) bool {
	switch s := s.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return s.Tok == token.GOTO
	case *ast.ExprStmt:
		if call, ok := ast.Unparen(s.X).(*ast.CallExpr); ok {
			if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok {
				return x.info.Uses[id] == types.Universe.Lookup("panic")
			}
		}
	case *ast.BlockStmt:
		return x.terminatingList(s.List, extracted)
	case *ast.IfStmt:
		return s.Else != nil && x.terminating(s.Body, extracted) && x.terminating(s.Else, extracted)
	case *ast.ForStmt:
		return s.Cond == nil && !x.hasBreak(s)
	case *ast.LabeledStmt:
		return x.terminating(s.Stmt, extracted)
	case *ast.SwitchStmt:
		return x.terminatingClauses(s, s.Body, extracted)
	case *ast.TypeSwitchStmt:
		return x.terminatingClauses(s, s.Body, extracted)
	case *ast.SelectStmt:
		return x.terminatingClauses(s, s.Body, extracted)
	}
	return false
}

// terminatingClauses applies the spec's rule for switch and select
// statements: no break refers to the statement, a switch has a default
// case, and every clause ends in a terminating statement (or, in a
// switch, a fallthrough).
func (x *extraction) terminatingClauses(stmt ast.Stmt, body *ast.BlockStmt, extracted bool) bool {
	if x.hasBreak(stmt) {
		return false
	}
	_, isSelect := stmt.(*ast.SelectStmt)
	hasDefault := false
	for _, c := range body.List {
		var list []ast.Stmt
		switch c := c.(type) {
		case *ast.CaseClause:
			list, hasDefault = c.Body, hasDefault || c.List == nil
		case *ast.CommClause:
			list = c.Body
		}
		if n := len(list); n > 0 {
			// A selected fallthrough is refused elsewhere, so this one stays.
			if b, ok := list[n-1].(*ast.BranchStmt); ok && b.Tok == token.FALLTHROUGH {
				continue
			}
		}
		if !x.terminatingList(list, extracted) {
			return false
		}
	}
	return isSelect || hasDefault
}

// hasBreak reports whether a break statement inside stmt refers to it:
// unlabeled and not nested in another for, switch or select, or labeled
// with stmt's label.
func (x *extraction) hasBreak(stmt ast.Stmt) bool {
	var label types.Object
	for _, n := range analysis.PathTo(x.file, stmt)[1:] {
		if ls, ok := n.(*ast.LabeledStmt); ok && ls.Stmt == stmt {
			label = x.info.Defs[ls.Label]
		}
		break
	}
	found := false
	var walk func(n ast.Node, nested bool)
	walk = func(n ast.Node, nested bool) {
		ast.Inspect(n, func(c ast.Node) bool {
			if found || c == nil {
				return false
			}
			switch c := c.(type) {
			case *ast.FuncLit:
				return false
			case *ast.BranchStmt:
				if c.Tok == token.BREAK {
					if c.Label == nil && !nested || c.Label != nil && label != nil && x.info.Uses[c.Label] == label {
						found = true
					}
				}
			case *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
				if c != n {
					walk(c, true)
					return false
				}
			}
			return true
		})
	}
	walk(stmt, false)
	return found
}

// typeOf returns t as a T, or the zero T.
func typeOf[T types.Type](t types.Type) T {
	v, _ := t.(T)
	return v
}

// checkBranch refuses a break, continue, goto or fallthrough whose target
// is outside the selection. stack holds the enclosing nodes within the
// selection, outermost first.
func (x *extraction) checkBranch(b *ast.BranchStmt, stack []ast.Node) error {
	line := x.line(b.Pos())
	if b.Label != nil {
		if obj := x.info.Uses[b.Label]; obj != nil && !x.inSelectionPos(obj.Pos()) {
			return refactor.Refusef("the %s at line %d jumps to the label %s, outside the selected lines; a new function cannot jump back into %s",
				b.Tok, line, b.Label.Name, x.enclosingName())
		}
		return nil
	}
	for i := len(stack) - 1; i >= 0; i-- {
		switch stack[i].(type) {
		case *ast.ForStmt, *ast.RangeStmt:
			if b.Tok == token.BREAK || b.Tok == token.CONTINUE {
				return nil
			}
		case *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
			if b.Tok == token.BREAK {
				return nil
			}
		case *ast.CaseClause:
			if b.Tok == token.FALLTHROUGH {
				return nil
			}
		}
	}
	if b.Tok == token.FALLTHROUGH {
		return refactor.Refusef("the selected lines end with fallthrough at line %d, which must stay the last statement of its case", line)
	}
	return refactor.Refusef("the %s at line %d leaves the selected lines (its loop or switch is outside them); a new function cannot %s for %s",
		b.Tok, line, b.Tok, x.enclosingName())
}

// ---------------------------------------------------------------------------
// Data flow

// flow is what the selected statements exchange with the rest of the
// enclosing function.
type flow struct {
	params []*types.Var // passed in, in order of first use
	locals []*types.Var // declared at the top of the new function: only assigned before being read
	// results are returned, in declaration order: variables declared
	// outside the selection that it changes and that are used afterwards
	// (updated), and variables it declares that are used after it (fresh).
	results []*types.Var
	fresh   map[*types.Var]bool
	// loopCarried lists results that are only returned because the
	// selection is in a loop, for the notes.
	loopCarried []string
}

// analyzeDataFlow works out the parameters and results of the new
// function, and refuses what passing copies in and out cannot preserve.
//
// Parameters and results are copies, so a variable that other code can
// reach while the statements run (its address is taken, or a closure
// captures it) would no longer be the same variable: such statements are
// refused. Whether a changed variable is used afterwards is decided
// conservatively: any use outside the selection counts, and so does being
// read in the selection when the selection is in a loop.
func (x *extraction) analyzeDataFlow() error {
	refs := map[*types.Var][]*ast.Ident{} // outside variables, by reference
	var order []*types.Var
	var bad []string
	seen := map[types.Object]bool{}
	for _, s := range x.stmts {
		ast.Inspect(s, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			obj := x.info.Uses[id]
			if obj == nil || !isLocal(obj) || x.inSelectionPos(obj.Pos()) {
				return true
			}
			if v, ok := obj.(*types.Var); ok {
				if refs[v] == nil {
					order = append(order, v)
				}
				refs[v] = append(refs[v], id)
				return true
			}
			if !seen[obj] {
				seen[obj] = true
				bad = append(bad, fmt.Sprintf("%s (%s declared at line %d)", obj.Name(), kindOf(obj), x.line(obj.Pos())))
			}
			return true
		})
	}
	if len(bad) > 0 {
		return refactor.Refusef("the selected lines use %s, declared outside them in %s; only variables can be passed to the new function, so constants, types and type parameters declared in the function are not supported yet",
			strings.Join(bad, ", "), x.enclosingName())
	}

	f := &flow{fresh: map[*types.Var]bool{}}
	for _, v := range order {
		switch {
		case x.fa.Addressed[v]:
			return refactor.Refusef("%s (declared at line %d) has its address taken in %s, or a pointer method is called on it; the new function would get a copy, so code holding the address would no longer see the same variable. Passing pointers is not supported yet",
				v.Name(), x.line(v.Pos()), x.enclosingName())
		case x.fa.Captured[v]:
			return refactor.Refusef("%s (declared at line %d) is used by a function literal in %s; the new function would get a copy, so the function literal and the new function would no longer share it",
				v.Name(), x.line(v.Pos()), x.enclosingName())
		}
		written, read := x.writtenIn(v), x.readIn(v)
		usedOutside := x.usedOutside(v)
		inLoop := x.loopAround(v)
		// After statements that always return, nothing reads a result.
		out := x.mode != terminal && written && (usedOutside || inLoop && read)
		switch {
		case out && x.assignedFirst(v, refs[v]):
			f.locals = append(f.locals, v)
		default:
			f.params = append(f.params, v)
		}
		if out {
			f.results = append(f.results, v)
			if !usedOutside {
				f.loopCarried = append(f.loopCarried, v.Name())
			}
		}
	}

	// Names declared by the selection and used after it.
	var later []*types.Var
	var badLater []string
	done := map[types.Object]bool{}
	for id, obj := range x.info.Uses {
		if done[obj] || !isLocal(obj) || !x.inSelectionPos(obj.Pos()) || x.inSelection(id) {
			continue
		}
		done[obj] = true
		if v, ok := obj.(*types.Var); ok && x.declaredAtTop(v) {
			later = append(later, v)
			continue
		}
		badLater = append(badLater, fmt.Sprintf("%s (used at line %d)", obj.Name(), x.line(id.Pos())))
	}
	if len(badLater) > 0 {
		sort.Strings(badLater)
		return refactor.Refusef("the selected lines declare %s, used after them, which cannot be returned from the new function (only variables declared directly in the selected statements can); include those uses in the selection, or select fewer statements",
			strings.Join(badLater, ", "))
	}
	sort.Slice(later, func(i, j int) bool { return later[i].Pos() < later[j].Pos() })
	if x.mode == terminal && len(later) > 0 {
		return refactor.Refusef("the selected lines always return, but declare %s, which unreachable code after them uses; remove that code first", later[0].Name())
	}
	for _, v := range later {
		switch {
		case x.fa.Addressed[v]:
			return refactor.Refusef("%s (declared at line %d) has its address taken, or a pointer method is called on it, and is used after the selected lines; returning it would return a copy, so anything holding its address would no longer see the same variable (some types, like strings.Builder, also refuse to be copied). Include its later uses in the selection",
				v.Name(), x.line(v.Pos()))
		case x.fa.Captured[v]:
			return refactor.Refusef("%s (declared at line %d) is used by a function literal and after the selected lines; returning it would return a copy that the function literal no longer shares. Include its later uses in the selection",
				v.Name(), x.line(v.Pos()))
		}
		f.results = append(f.results, v)
		f.fresh[v] = true
	}
	sort.Slice(f.results, func(i, j int) bool { return f.results[i].Pos() < f.results[j].Pos() })

	for _, list := range [][]*types.Var{f.params, f.locals, f.results} {
		for _, v := range list {
			if _, err := x.typeString(v.Name(), v.Type()); err != nil {
				return err
			}
		}
	}
	// A selected statement that declares a name the new function already
	// declares (a parameter or a local) would redeclare it.
	taken := map[string]*types.Var{}
	for _, v := range append(append([]*types.Var(nil), f.params...), f.locals...) {
		taken[v.Name()] = v
	}
	for _, s := range x.stmts {
		var ids []*ast.Ident
		switch s := s.(type) {
		case *ast.AssignStmt:
			if s.Tok == token.DEFINE {
				for _, l := range s.Lhs {
					if id, ok := l.(*ast.Ident); ok {
						ids = append(ids, id)
					}
				}
			}
		case *ast.DeclStmt:
			ast.Inspect(s, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && x.info.Defs[id] != nil {
					ids = append(ids, id)
				}
				return true
			})
		}
		for _, id := range ids {
			if v := taken[id.Name]; v != nil && x.info.Defs[id] != nil {
				return refactor.Refusef("line %d declares a new %s while the selected lines also use the %s declared at line %d; in the new function both would be in the same scope. Rename one of them first",
					x.line(id.Pos()), id.Name, id.Name, x.line(v.Pos()))
			}
		}
	}

	x.flow = f
	return nil
}

// writtenIn reports whether the selection assigns v (or part of it).
func (x *extraction) writtenIn(v *types.Var) bool {
	for _, m := range x.fa.Modified[v] {
		if x.inSelection(m) {
			return true
		}
	}
	return false
}

// readIn reports whether the selection reads v: any reference that is not
// the whole target of a plain assignment.
func (x *extraction) readIn(v *types.Var) bool {
	for _, s := range x.stmts {
		read := false
		ast.Inspect(s, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if read || !ok || x.info.Uses[id] != v {
				return !read
			}
			if !x.isAssignTarget(id) {
				read = true
			}
			return false
		})
		if read {
			return true
		}
	}
	return false
}

// isAssignTarget reports whether id is a whole left-hand operand of = or
// := (which writes without reading).
func (x *extraction) isAssignTarget(id *ast.Ident) bool {
	path := analysis.PathTo(x.file, id)
	if len(path) < 2 {
		return false
	}
	as, ok := path[1].(*ast.AssignStmt)
	if !ok || (as.Tok != token.ASSIGN && as.Tok != token.DEFINE) {
		return false
	}
	for _, l := range as.Lhs {
		if l == id {
			return true
		}
	}
	return false
}

// usedOutside reports whether v is referenced outside the selection,
// other than where it is declared. A named result always counts: a bare
// return reads it without naming it.
func (x *extraction) usedOutside(v *types.Var) bool {
	for _, n := range analysis.PathTo(x.file, x.stmts[0]) {
		var ft *ast.FuncType
		switch n := n.(type) {
		case *ast.FuncDecl:
			ft = n.Type
		case *ast.FuncLit:
			ft = n.Type
		}
		if ft != nil && ft.Results != nil {
			for _, fld := range ft.Results.List {
				for _, name := range fld.Names {
					if x.info.Defs[name] == v {
						return true
					}
				}
			}
		}
	}
	for id, obj := range x.info.Uses {
		if obj == v && !x.inSelection(id) {
			return true
		}
	}
	return false
}

// loopAround reports whether the selection is inside a loop (or a function
// that jumps backwards with goto) that v is declared outside of, so that a
// value the selection stores can be read by the selection itself in the
// next iteration.
func (x *extraction) loopAround(v *types.Var) bool {
	for _, n := range analysis.PathTo(x.file, x.stmts[0]) {
		switch n := n.(type) {
		case *ast.ForStmt, *ast.RangeStmt:
			if v.Pos() < n.Pos() {
				return true
			}
		case *ast.FuncDecl, *ast.FuncLit:
			has := false
			ast.Inspect(n, func(c ast.Node) bool {
				if b, ok := c.(*ast.BranchStmt); ok && b.Tok == token.GOTO {
					has = true
				}
				return !has
			})
			return has
		}
	}
	return false
}

// assignedFirst reports whether the selection's first reference to v is a
// plain assignment to all of v by one of the selected statements (not a
// nested one, which might not run), whose right-hand side does not read v.
// Such a variable need not be passed in; the new function declares it.
func (x *extraction) assignedFirst(v *types.Var, refs []*ast.Ident) bool {
	first := refs[0]
	if !x.isAssignTarget(first) {
		return false
	}
	as := analysis.PathTo(x.file, first)[1].(*ast.AssignStmt)
	top := false
	for _, s := range x.stmts {
		top = top || s == as
	}
	if !top {
		return false
	}
	for _, r := range as.Rhs {
		for _, id := range analysis.ScopeRefs(x.info, r) {
			if x.info.Uses[id] == v {
				return false
			}
		}
	}
	return true
}

// declaredAtTop reports whether v is declared directly by one of the
// selected statements (so it is in scope after them), not in a nested
// block.
func (x *extraction) declaredAtTop(v *types.Var) bool {
	for _, s := range x.stmts {
		if v.Pos() >= s.Pos() && v.Pos() < s.End() {
			switch s := s.(type) {
			case *ast.AssignStmt:
				return s.Tok == token.DEFINE
			case *ast.DeclStmt:
				return true
			}
			return false
		}
	}
	return false
}

// typeString spells v's type for the new function's signature, or refuses
// if it cannot be written there: a type declared inside the function, a
// type parameter, or a type from a package the file does not import.
func (x *extraction) typeString(name string, t types.Type) (string, error) {
	what := "the result type " + types.TypeString(t, types.RelativeTo(x.pkg.Types))
	if name != "" {
		what = fmt.Sprintf("%s has type %s, which", name, types.TypeString(t, types.RelativeTo(x.pkg.Types)))
	}
	if bad := localTypeIn(t, x.pkg.Types, map[types.Type]bool{}); bad != nil {
		return "", refactor.Refusef("%s uses %s %s declared inside %s; it cannot be named in a new package-level function",
			what, kindOf(bad), bad.Name(), x.enclosingName())
	}
	s, missing := analysis.TypeString(t, x.file, x.pkg.Types, analysis.ImportMap(x.pkg.Types))
	if missing != "" {
		return "", refactor.Refusef("%s needs package %q; %s does not import it, and adding imports is not supported yet",
			what, missing, x.rel)
	}
	return s, nil
}

// localTypeIn returns a type name in t that cannot be written outside the
// function: a type parameter or a type declared in a function body.
func localTypeIn(t types.Type, pkg *types.Package, seen map[types.Type]bool) *types.TypeName {
	if seen[t] {
		return nil
	}
	seen[t] = true
	switch t := t.(type) {
	case *types.TypeParam:
		return t.Obj()
	case *types.Named:
		if o := t.Obj(); o.Parent() != nil && o.Pkg() != nil && o.Parent() != o.Pkg().Scope() && o.Parent() != types.Universe {
			return o
		}
		for i := 0; i < t.TypeArgs().Len(); i++ {
			if b := localTypeIn(t.TypeArgs().At(i), pkg, seen); b != nil {
				return b
			}
		}
	case *types.Alias:
		if o := t.Obj(); o.Parent() != nil && o.Pkg() != nil && o.Parent() != o.Pkg().Scope() && o.Parent() != types.Universe {
			return o
		}
		return localTypeIn(types.Unalias(t), pkg, seen)
	case *types.Pointer:
		return localTypeIn(t.Elem(), pkg, seen)
	case *types.Slice:
		return localTypeIn(t.Elem(), pkg, seen)
	case *types.Array:
		return localTypeIn(t.Elem(), pkg, seen)
	case *types.Chan:
		return localTypeIn(t.Elem(), pkg, seen)
	case *types.Map:
		if b := localTypeIn(t.Key(), pkg, seen); b != nil {
			return b
		}
		return localTypeIn(t.Elem(), pkg, seen)
	case *types.Signature:
		if b := localTypeIn(t.Params(), pkg, seen); b != nil {
			return b
		}
		return localTypeIn(t.Results(), pkg, seen)
	case *types.Tuple:
		for i := 0; i < t.Len(); i++ {
			if b := localTypeIn(t.At(i).Type(), pkg, seen); b != nil {
				return b
			}
		}
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if b := localTypeIn(t.Field(i).Type(), pkg, seen); b != nil {
				return b
			}
		}
	case *types.Interface:
		for i := 0; i < t.NumMethods(); i++ {
			if b := localTypeIn(t.Method(i).Type(), pkg, seen); b != nil {
				return b
			}
		}
		for i := 0; i < t.NumEmbeddeds(); i++ {
			if b := localTypeIn(t.EmbeddedType(i), pkg, seen); b != nil {
				return b
			}
		}
	}
	return nil
}

// outermostFunc returns the outermost function containing the selection.
func (x *extraction) outermostFunc() ast.Node {
	return analysis.OutermostFunc(analysis.PathTo(x.file, x.stmts[0]))
}

// isLocal reports whether obj is declared inside a function: not at
// package level, not in the universe, and not an import name or a label.
func isLocal(obj types.Object) bool {
	switch obj.(type) {
	case *types.PkgName, *types.Label, *types.Builtin, *types.Nil:
		return false
	}
	if obj.Pkg() == nil || obj.Parent() == nil {
		return false // universe objects, fields and methods
	}
	return obj.Parent() != obj.Pkg().Scope() && obj.Parent() != types.Universe
}

// ---------------------------------------------------------------------------
// The new name

// checkNewName refuses a name that clashes with a package-level or imported
// name, would hide a builtin, would be hidden at the call site, or would
// turn the function into a test.
func (x *extraction) checkNewName() error {
	if o := types.Universe.Lookup(x.name); o != nil {
		return refactor.Refusef("%s is a predeclared %s; a package-level function with that name would hide it in the whole package. Choose another name", x.name, kindOf(o))
	}
	for _, p := range x.ws.Pkgs {
		if p.PkgPath != x.pkg.PkgPath || p.Types == nil {
			continue
		}
		if o := p.Types.Scope().Lookup(x.name); o != nil {
			return refactor.Refusef("%s is already declared in package %s (%s at %s); choose another name",
				x.name, p.Types.Name(), kindOf(o), x.ws.Position(o.Pos()))
		}
		for _, f := range p.Syntax {
			if s := p.TypesInfo.Scopes[f]; s != nil {
				if o := s.Lookup(x.name); o != nil {
					return refactor.Refusef("%s is an imported name in %s; choose another name", x.name, x.ws.Rel(x.ws.Fset.File(f.Pos()).Name()))
				}
			}
		}
	}
	if strings.HasSuffix(x.path, "_test.go") {
		for _, prefix := range []string{"Test", "Benchmark", "Example", "Fuzz"} {
			if rest, ok := strings.CutPrefix(x.name, prefix); ok {
				if r, _ := utf8.DecodeRuneInString(rest); rest == "" || !unicode.IsLower(r) {
					return refactor.Refusef("%s in a _test.go file looks like a %s function to go test; choose another name", x.name, strings.ToLower(prefix))
				}
			}
		}
	}
	// A local declaration of the name in scope at the call site would hide
	// the new function there.
	first := x.stmts[0]
	if scope := x.innermostScope(first.Pos()); scope != nil {
		if s, o := scope.LookupParent(x.name, first.Pos()); o != nil && s != types.Universe && s != x.pkg.Types.Scope() && !x.inSelectionPos(o.Pos()) {
			return refactor.Refusef("%s is a %s declared at line %d, which would hide the new function at the call site; choose another name",
				x.name, kindOf(o), x.line(o.Pos()))
		}
	}
	return nil
}

// innermostScope returns the innermost scope containing pos.
func (x *extraction) innermostScope(pos token.Pos) *types.Scope {
	s := x.info.Scopes[x.file]
	if s == nil {
		return nil
	}
	return s.Innermost(pos)
}

// ---------------------------------------------------------------------------
// Edits

func (x *extraction) plan() (*refactor.Plan, error) {
	decl := x.topDecl()
	if decl == nil {
		return nil, fmt.Errorf("internal error: no declaration encloses line %d", x.line(x.stmts[0].Pos()))
	}
	f := x.flow
	ts := func(name string, t types.Type) string {
		s, _ := x.typeString(name, t) // checked by analyzeDataFlow and decideReturns
		return s
	}
	names := func(vs []*types.Var) []string {
		var out []string
		for _, v := range vs {
			out = append(out, v.Name())
		}
		return out
	}
	_, _, sig := x.enclosingFunc()
	indent := x.indentOf(x.stmts[0].Pos())

	// The new function's results, and what the call site does with them.
	var results []string
	var bodyEdits []edit.TextEdit
	var tail, errName string // tail: the new function's final return
	switch x.mode {
	case terminal:
		for i := 0; i < sig.Results().Len(); i++ {
			results = append(results, ts("", sig.Results().At(i).Type()))
		}
	case errorExit:
		var zeros []string
		for _, v := range f.results {
			results = append(results, ts(v.Name(), v.Type()))
			z, err := x.zeroValue(v.Type())
			if err != nil {
				return nil, err
			}
			zeros = append(zeros, z)
		}
		results = append(results, "error")
		bodyEdits = x.rewriteExits(zeros)
		tail = strings.Join(append(names(f.results), "nil"), ", ")
		errName = x.errorName(f.results)
	default:
		for _, v := range f.results {
			results = append(results, ts(v.Name(), v.Type()))
		}
		if len(f.results) > 0 && !x.terminatingList(x.stmts, false) {
			tail = strings.Join(names(f.results), ", ")
		}
	}

	// The new function.
	var params []string
	for _, v := range f.params {
		params = append(params, v.Name()+" "+ts(v.Name(), v.Type()))
	}
	sigText := fmt.Sprintf("func %s(%s)", x.name, strings.Join(params, ", "))
	switch len(results) {
	case 0:
	case 1:
		sigText += " " + results[0]
	default:
		sigText += " (" + strings.Join(results, ", ") + ")"
	}
	var fn strings.Builder
	fmt.Fprintf(&fn, "\n\n%s {\n", sigText)
	for _, v := range f.locals {
		fmt.Fprintf(&fn, "\tvar %s %s\n", v.Name(), ts(v.Name(), v.Type()))
	}
	text, err := x.selectedText(bodyEdits)
	if err != nil {
		return nil, err
	}
	fn.WriteString(reindent(text, indent, "\t"))
	if tail != "" {
		fmt.Fprintf(&fn, "\n\treturn %s", tail)
	}
	fn.WriteString("\n}")

	// The call.
	call := fmt.Sprintf("%s(%s)", x.name, strings.Join(names(f.params), ", "))
	var site strings.Builder
	switch x.mode {
	case terminal:
		if sig.Results().Len() > 0 {
			fmt.Fprintf(&site, "%sreturn %s", indent, call)
		} else {
			fmt.Fprintf(&site, "%s%s\n%sreturn", indent, call, indent)
		}
	default:
		lhs := names(f.results)
		var fresh, updated []string // fresh: declared by this statement
		for _, v := range f.results {
			if f.fresh[v] {
				fresh = append(fresh, fmt.Sprintf("var %s %s", v.Name(), ts(v.Name(), v.Type())))
			} else {
				updated = append(updated, v.Name())
			}
		}
		if errName != "" {
			lhs = append(lhs, errName)
			fresh = append(fresh, fmt.Sprintf("var %s error", errName))
		}
		switch {
		case len(lhs) == 0:
			site.WriteString(indent + call)
		case len(updated) == 0:
			fmt.Fprintf(&site, "%s%s := %s", indent, strings.Join(lhs, ", "), call)
		default:
			// := could declare a new variable instead of assigning one
			// declared in an outer scope, so declare the new ones first.
			for _, d := range fresh {
				fmt.Fprintf(&site, "%s%s\n", indent, d)
			}
			fmt.Fprintf(&site, "%s%s = %s", indent, strings.Join(lhs, ", "), call)
		}
		if errName != "" {
			vals := strings.Join(append(append([]string(nil), x.exitValues...), errName), ", ")
			fmt.Fprintf(&site, "\n%sif %s != nil {\n%s\treturn %s\n%s}", indent, errName, indent, vals, indent)
		}
	}

	// Insert after the line on which the enclosing declaration ends, so a
	// trailing comment there stays with it.
	at := x.off(decl.End())
	if i := strings.IndexByte(string(x.src[at:]), '\n'); i >= 0 {
		at += i
	} else {
		at = len(x.src)
	}
	edits := []edit.TextEdit{
		{Start: x.start, End: x.end, NewText: site.String()},
		{Start: at, End: at, NewText: fn.String()},
	}

	var notes []string
	if len(f.params) > 0 || len(f.results) > 0 {
		describe := func(vs []*types.Var) string {
			if len(vs) == 0 {
				return "none"
			}
			return strings.Join(names(vs), ", ")
		}
		notes = append(notes, fmt.Sprintf("parameters: %s; results: %s", describe(f.params), describe(f.results)))
	}
	switch x.mode {
	case terminal:
		notes = append(notes, "the selected lines always return, so the call site returns what the new function returns")
	case errorExit:
		notes = append(notes, fmt.Sprintf("the early returns now return their error from %s, and the call site returns it when it is not nil", x.name))
	}
	if len(f.loopCarried) > 0 {
		notes = append(notes, fmt.Sprintf("%s %s returned because the selected lines are in a loop and may read %s in the next iteration; if not, the result can be dropped",
			strings.Join(f.loopCarried, ", "), isAre(len(f.loopCarried)), itThem(len(f.loopCarried))))
	}
	first, last := x.line(x.tf.Pos(x.start)), x.line(x.tf.Pos(x.end))
	n := len(x.stmts)
	return &refactor.Plan{
		Summary: fmt.Sprintf("extracted lines %d-%d (%d statement%s) into function %s", first, last, n, plural(n), x.name),
		Edits:   []edit.FileEdit{{Path: x.path, Edits: edits}},
		Notes:   notes,
	}, nil
}

// selectedText returns the selected lines with edits (at file offsets
// inside them) applied.
func (x *extraction) selectedText(edits []edit.TextEdit) (string, error) {
	shifted := make([]edit.TextEdit, len(edits))
	for i, e := range edits {
		shifted[i] = edit.TextEdit{Start: e.Start - x.start, End: e.End - x.start, NewText: e.NewText}
	}
	out, err := edit.Apply(x.src[x.start:x.end], shifted)
	return string(out), err
}

func isAre(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}

func itThem(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}

// reindent returns text (whole lines of statements) with each line's
// leading from replaced by to. Lines that start inside a multi-line string
// literal or comment are left alone, since changing them would change the
// string.
func reindent(text, from, to string) string {
	var spans [][2]int // multi-line tokens
	for _, t := range scanTokens([]byte(text)) {
		if (t.tok == token.STRING || t.tok == token.COMMENT) && strings.Contains(text[t.off:t.end], "\n") {
			spans = append(spans, [2]int{t.off, t.end})
		}
	}
	inside := func(off int) bool {
		for _, s := range spans {
			if s[0] < off && off < s[1] {
				return true
			}
		}
		return false
	}
	var sb strings.Builder
	off := 0
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			sb.WriteByte('\n')
		}
		switch {
		case inside(off):
			sb.WriteString(line)
		case strings.TrimSpace(line) == "":
		case strings.HasPrefix(line, from):
			sb.WriteString(to + line[len(from):])
		default:
			sb.WriteString(to + strings.TrimLeft(line, " \t"))
		}
		off += len(line) + 1
	}
	return sb.String()
}

// topDecl returns the top-level declaration containing the selection.
func (x *extraction) topDecl() ast.Decl {
	p := x.stmts[0].Pos()
	for _, d := range x.file.Decls {
		if d.Pos() <= p && p < d.End() {
			return d
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers

func (x *extraction) inSelectionPos(p token.Pos) bool {
	return p.IsValid() && p >= x.stmts[0].Pos() && p < x.stmts[len(x.stmts)-1].End()
}

func (x *extraction) inSelection(n ast.Node) bool { return x.inSelectionPos(n.Pos()) }

// enclosingName names the function containing the selection, for messages.
func (x *extraction) enclosingName() string {
	path := analysis.PathTo(x.file, x.stmts[0])
	for _, n := range path {
		switch n := n.(type) {
		case *ast.FuncLit:
			return "the enclosing function literal"
		case *ast.FuncDecl:
			return n.Name.Name
		}
	}
	return "the enclosing function"
}

func (x *extraction) off(p token.Pos) int { return x.tf.Offset(p) }

func (x *extraction) line(p token.Pos) int { return x.tf.Line(p) }

// firstLine quotes the first line of a statement, shortened if long.
func (x *extraction) firstLine(s ast.Stmt) string {
	text := string(x.src[x.off(s.Pos()):x.off(s.End())])
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i] + " ..."
	}
	return quoteText(text)
}

func quoteText(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 40 {
		s = s[:37] + "..."
	}
	return "`" + s + "`"
}

func (x *extraction) indentOf(p token.Pos) string {
	start := x.off(x.tf.LineStart(x.line(p)))
	ws := x.src[start:x.off(p)]
	for i, c := range ws {
		if c != ' ' && c != '\t' {
			return string(ws[:i])
		}
	}
	return string(ws)
}

func kindOf(o types.Object) string {
	switch o := o.(type) {
	case *types.Const:
		return "constant"
	case *types.TypeName:
		if _, ok := o.Type().(*types.TypeParam); ok {
			return "type parameter"
		}
		return "type"
	case *types.Func:
		return "function"
	case *types.Builtin:
		return "builtin function"
	case *types.PkgName:
		return "package name"
	case *types.Var:
		if o.IsField() {
			return "field"
		}
		return "variable"
	}
	return "name"
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
