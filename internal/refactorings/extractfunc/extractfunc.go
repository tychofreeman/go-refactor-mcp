// Package extractfunc implements "extract function": move a run of
// complete statements from one block into a new package-level function,
// and call it in their place.
//
// The statements run in a different function afterwards, so most of this
// file checks what that can change:
//
//   - selection: the lines must cover whole statements of a single block,
//     plus optional comments; nothing else;
//   - control flow: return, defer, recover, fallthrough, and break,
//     continue or goto that leave the selection behave differently (or not
//     at all) in another function;
//   - data flow: in this version the statements may only use their own
//     variables and package-level names. Local variables declared outside
//     the selection would have to become parameters or results, which comes
//     later;
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
Currently the statements may only use package-level names and variables they declare themselves (and those variables must not be used after them): passing values in or out of the new function is not supported yet.
Refuses if the statements contain return, defer, recover() or fallthrough, or a break, continue or goto that leaves the selected lines.`
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
	if err := x.checkControlFlow(); err != nil {
		return nil, err
	}
	if err := x.checkDataFlow(); err != nil {
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
	fs := token.NewFileSet()
	f := fs.AddFile("", -1, len(x.src))
	var s scanner.Scanner
	s.Init(f, x.src, nil, scanner.ScanComments)
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
				err = refactor.Refusef("the selected lines contain a return statement at line %d; in a new function it would return from that function instead. Returning from the selected code is not supported yet", x.line(n.Pos()))
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
	// Checked last: a selection containing a return is refused above with
	// a clearer reason.
	return x.checkTermination()
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

// checkDataFlow refuses statements that use local names declared outside
// the selection, or declare names used after it. Both need parameters or
// results, which this version does not create.
func (x *extraction) checkDataFlow() error {
	var ins []string
	seen := map[types.Object]bool{}
	for _, s := range x.stmts {
		ast.Inspect(s, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			obj := x.info.Uses[id]
			if obj == nil || seen[obj] || !isLocal(obj) || x.inSelectionPos(obj.Pos()) {
				return true
			}
			seen[obj] = true
			ins = append(ins, fmt.Sprintf("%s (%s declared at line %d)", obj.Name(), kindOf(obj), x.line(obj.Pos())))
			return true
		})
	}
	if len(ins) > 0 {
		return refactor.Refusef("the selected lines use %s, declared outside them in %s. Passing values into the new function as parameters is not supported yet; select statements that only use package-level names and variables they declare themselves",
			strings.Join(ins, ", "), x.enclosingName())
	}

	// Uses after the selection of names it declares.
	var outs []string
	type use struct {
		obj  types.Object
		line int
	}
	var later []use
	for id, obj := range x.info.Uses {
		if isLocal(obj) && x.inSelectionPos(obj.Pos()) && !x.inSelection(id) {
			later = append(later, use{obj, x.line(id.Pos())})
		}
	}
	sort.Slice(later, func(i, j int) bool { return later[i].line < later[j].line })
	done := map[types.Object]bool{}
	for _, u := range later {
		if done[u.obj] {
			continue
		}
		done[u.obj] = true
		outs = append(outs, fmt.Sprintf("%s (used at line %d)", u.obj.Name(), u.line))
	}
	if len(outs) > 0 {
		return refactor.Refusef("the selected lines declare %s, used after them. Returning values from the new function is not supported yet; include those uses in the selection, or select fewer statements",
			strings.Join(outs, ", "))
	}
	return nil
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

	indent := x.indentOf(x.stmts[0].Pos())
	body := x.reindent(indent, "\t")

	// Insert after the line on which the enclosing declaration ends, so a
	// trailing comment there stays with it.
	at := x.off(decl.End())
	if i := strings.IndexByte(string(x.src[at:]), '\n'); i >= 0 {
		at += i
	} else {
		at = len(x.src)
	}
	fn := fmt.Sprintf("\n\nfunc %s() {\n%s\n}", x.name, body)

	edits := []edit.TextEdit{
		{Start: x.start, End: x.end, NewText: indent + x.name + "()"},
		{Start: at, End: at, NewText: fn},
	}
	first, last := x.line(x.tf.Pos(x.start)), x.line(x.tf.Pos(x.end))
	n := len(x.stmts)
	return &refactor.Plan{
		Summary: fmt.Sprintf("extracted lines %d-%d (%d statement%s) into function %s", first, last, n, plural(n), x.name),
		Edits:   []edit.FileEdit{{Path: x.path, Edits: edits}},
	}, nil
}

// reindent returns the selected text with each line's leading from
// replaced by to. Lines that start inside a multi-line string literal or
// comment are left alone, since changing them would change the string.
func (x *extraction) reindent(from, to string) string {
	var spans [][2]int // multi-line tokens
	for _, t := range x.tokens() {
		if t.end > x.start && t.off < x.end && (t.tok == token.STRING || t.tok == token.COMMENT) &&
			strings.Contains(string(x.src[t.off:t.end]), "\n") {
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
	off := x.start
	for i, line := range strings.Split(string(x.src[x.start:x.end]), "\n") {
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
