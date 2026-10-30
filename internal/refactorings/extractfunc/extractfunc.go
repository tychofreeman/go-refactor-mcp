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
//     results. Variables that other code can reach while the statements
//     run (address taken, captured by a closure) are passed as pointers
//     so both functions share them;
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
	"go/token"
	"go/types"
	"go/version"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tychofreeman/go-refactor-mcp/internal/analysis"
	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/edits/gocode"
	"github.com/tychofreeman/go-refactor-mcp/internal/imports"
	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/site"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
	"golang.org/x/tools/go/ast/astutil"
)

// ExtractFunction is the "extract_function" refactoring.
type ExtractFunction struct{ refactor.Named[args] }

// New returns the extract_function refactoring.
func New() *ExtractFunction {
	return &ExtractFunction{Named: refactor.Called[args]("extract_function")}
}

func (*ExtractFunction) Description() string {
	return `Move a run of whole statements into a new function, declared after the one they are in, and call it in their place: in func report(plants []Plant) string, extracting total := 0 and the loop that adds each p.Height to it, as sum, gives total := sum(plants) there and func sum(plants []Plant) int { total := 0; for ...; return total }. Locals the statements use become parameters, and those they set that are used afterwards become results; the result lists both. Statements that use the receiver become a method of its type.
Identify the statements by file, start_line and end_line: whole statements of one block, as inspect numbers them; or a case's body by function and case ("fern" in function Water, or default), which stays valid after earlier steps; for a value with no case, when the code after the switch is what runs for it, a new case calls a copy of that code. doc becomes the new function's doc comment.
Returns: when every path returns, the call becomes return sum(...); early error returns (if err != nil { return err }) make the new function return an error, which the call checks; so do early returns of a string last (a refusal: return nil, "no name") known not to be empty.
Refuses, saying why: other returns, defer, recover, fallthrough, a break, continue or goto out of the lines, a variable they declare that a pointer or closure keeps and that is used after them.`
}

type args struct {
	File      string `json:"file" desc:"Path of the file, relative to the module root."`
	StartLine int    `json:"start_line,omitempty" desc:"1-based first line of the statements to extract."`
	EndLine   int    `json:"end_line,omitempty" desc:"1-based last line of the statements to extract (inclusive)."`
	Function  string `json:"function,omitempty" desc:"Instead of the lines, with case: the function (Name or Type.Method) with the switch."`
	Case      string `json:"case,omitempty" desc:"Instead of the lines: the case whose body to extract, its value as written (\"\\\"fern\\\"\", Red) or default."`
	Split     *bool  `json:"split,omitempty" desc:"With case, of several values (case \"fern\", \"moss\":): unless false, the value gets a case of its own, the others a copy of the body."`
	Name      string `json:"name" desc:"Name for the new function."`
	Doc       string `json:"doc,omitempty" desc:"Optional doc comment for the new function, without or with the leading //; start it with the function's name."`
}

// extraction holds the state of one planning run.
type extraction struct {
	site.Site
	path string // absolute
	rel  string // as given
	name string
	doc  string // the new function's doc comment, as given

	stmts []ast.Stmt // the selected statements, in order
	list  []ast.Stmt // the statement list they belong to
	block ast.Node   // the node owning list: *ast.BlockStmt, *ast.CaseClause or *ast.CommClause
	// split gives the extracted case's value a case of its own, when
	// the statements are the body of a case of several values.
	split []edit.TextEdit
	// missing is the case to add for a value the switch has no case for:
	// the selection is then the code after the switch, which stays.
	missing *missingCase

	start, end int // byte range of the selected text: whole lines, from the first to the last token

	fa   *analysis.Func // facts about the outermost enclosing function's locals
	flow *flow

	returns    []*ast.ReturnStmt // in the selection, outside function literals
	mode       returnMode
	exitValues []string   // errorExit: the non-error values the early returns return
	exitType   types.Type // errorExit: the last result's type, an error or a string or number whose zero means no exit

	nonNilFuncs map[*types.Func]bool // alwaysNonNil's and alwaysNonZero's answers

	toks []tok // the file's tokens, scanned once

	imp *imports.Adder // the file's imports and the ones to add

	// recv is the enclosing method's receiver when the statements use it:
	// the new function is then a method on the same type.
	recv     *types.Var
	recvType string // the receiver's type as written, e.g. "*Stack[T]"
	// nameable are type names the new function may write although they
	// are not package-level: type parameters it declares (through its
	// receiver), and local types moved to package level.
	nameable map[*types.TypeName]bool

	hoists []hoist // local constants and types moved to package level

	// tparams are the enclosing function's type parameters, and
	// tparamText their declaration as written ("T any, S ~[]T"). generic
	// reports whether the new function uses them, and so declares them all.
	tparams    []*types.TypeName
	tparamText string
	generic    bool
}

// Plan checks preconditions and computes the edits.
func (*ExtractFunction) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a args
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.File == "" {
		return nil, refactor.Refusef("file is required")
	}
	if err := checkName(a.Name); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	at, err := site.At(ws, path)
	if err != nil {
		return nil, err
	}
	x := &extraction{
		Site: *at,
		path: path,
		rel:  a.File,
		name: a.Name,
		doc:  a.Doc,
	}
	if err := a.caseLines(x.TF, x.File, x.Src); err != nil {
		if a.StartLine, a.EndLine, x.missing, err = x.missingCase(a, err); err != nil {
			return nil, err
		}
	}
	switch {
	case a.StartLine < 1:
		return nil, refactor.Refusef("start_line is required (1-based), or function and case")
	case a.EndLine < 1:
		return nil, refactor.Refusef("end_line is required (1-based)")
	case a.EndLine < a.StartLine:
		return nil, refactor.Refusef("end_line (%d) is before start_line (%d)", a.EndLine, a.StartLine)
	case a.EndLine > x.TF.LineCount():
		return nil, refactor.Refusef("end_line %d is past the end of %s, which has %d lines", a.EndLine, a.File, x.TF.LineCount())
	}
	if err := x.selectStatements(a.StartLine, a.EndLine); err != nil {
		return nil, err
	}
	if a.Case != "" && a.splits() {
		if err := x.splitCase(a.Case); err != nil {
			return nil, err
		}
	}
	x.fa = analysis.AnalyzeFunc(x.Info, x.outermostFunc())
	// What the new function can name: before any check spells a type.
	x.nameable = map[*types.TypeName]bool{}
	x.findReceiver()
	x.findTypeParams()
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

// caseLines sets the lines to those of the case a.Case of a switch in
// a.Function, when the statements are given so.
func (a *args) caseLines(tf *token.File, file *ast.File, src []byte) error {
	if a.Case == "" && a.Function == "" {
		return nil
	}
	if a.StartLine != 0 || a.EndLine != 0 {
		return refactor.Refusef("give either function and case or start_line and end_line, not both")
	}
	if a.Case == "" || a.Function == "" {
		return refactor.Refusef("give function and case together: the case of a switch in that function whose body to extract")
	}
	fd := locate.Func(file, a.Function)
	if fd == nil || fd.Body == nil {
		return refactor.Refusef("no function %s in %s; give Name, or Type.Method for a method", a.Function, a.File)
	}
	var err error
	a.StartLine, a.EndLine, err = locate.CaseBody(tf, src, fd, a.Case, a.splits())
	return err
}

// splits reports whether a case of several values is split: unless
// split is false.
func (a *args) splits() bool { return a.Split == nil || *a.Split }

// splitCase sets x.split when the statements are the body of a case of
// several values: value, as the model gave it, gets a case of its own
// with the body, and each other value one before it with a copy.
func (x *extraction) splitCase(value string) error {
	cc, ok := x.block.(*ast.CaseClause)
	if !ok || len(cc.List) < 2 {
		return nil
	}
	line := x.line(cc.Pos())
	text := func(n ast.Node) string { return string(x.Src[x.TF.Offset(n.Pos()):x.TF.Offset(n.End())]) }
	k := -1
	for _, v := range []string{value, strconv.Quote(value)} {
		want, err := locate.ExprTokens(v)
		if err != nil {
			continue
		}
		for i, e := range cc.List {
			if k < 0 && locate.TokensMatch(x.TF, x.Src, e, want) {
				k = i
			}
		}
	}
	if k < 0 {
		return fmt.Errorf("internal error: no value %s in the case on line %d", value, line)
	}
	for _, e := range cc.List {
		if tv, ok := x.Info.Types[e]; !ok || tv.Value == nil {
			return refactor.Refusef("the case on line %d has %s, which is not a constant; splitting the case would change the order its values are compared in", line, text(e))
		}
	}
	var what string
	for _, st := range cc.Body {
		ast.Inspect(st, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.LabeledStmt:
				what = "label " + n.Label.Name + ", which a function cannot declare twice"
			case *ast.TypeSpec:
				what = "type " + n.Name.Name + ", which would be a different type in each copy"
			}
			return what == ""
		})
	}
	if what != "" {
		return refactor.Refusef("the case on line %d declares %s; splitting it copies its body", line, what)
	}
	path, _ := astutil.PathEnclosingInterval(x.File, cc.Pos(), cc.End())
	if len(path) > 1 {
		if block, ok := path[1].(*ast.BlockStmt); ok {
			if i := slices.Index(block.List, ast.Stmt(cc)); i > 0 {
				prev := block.List[i-1].(*ast.CaseClause)
				if n := len(prev.Body); n > 0 {
					if br, ok := prev.Body[n-1].(*ast.BranchStmt); ok && br.Tok == token.FALLTHROUGH {
						return refactor.Refusef("the case before the one on line %d falls through into it; split it by hand, or give start_line and end_line for the shared body", line)
					}
				}
			}
		}
	}
	indent := string(x.Src[x.TF.Offset(x.TF.LineStart(line)):x.TF.Offset(cc.Case)])
	bodyStart := x.TF.Offset(x.TF.LineStart(x.line(cc.Colon) + 1))
	bodyEnd := len(x.Src)
	if end := x.line(cc.End()); end < x.TF.LineCount() {
		bodyEnd = x.TF.Offset(x.TF.LineStart(end + 1))
	}
	body := string(x.Src[bodyStart:bodyEnd])
	var b strings.Builder
	for i, e := range cc.List {
		if i != k {
			b.WriteString("case " + text(e) + ":\n" + body + indent)
		}
	}
	b.WriteString("case " + text(cc.List[k]) + ":")
	x.split = []edit.TextEdit{{Start: x.TF.Offset(cc.Case), End: x.TF.Offset(cc.Colon) + 1, NewText: b.String()}}
	return nil
}

// A missingCase is a case to add, at the end of a switch, for a value it
// has no case for, whose body calls the new function.
type missingCase struct {
	value  string // as written in the new case
	at     int    // the start of the line of the switch's closing brace
	indent string // the switch's
}

// missingCase answers err, the refusal of a value that no case of the
// function's switch has, when the code after the switch is what runs for
// it: the switch has no default, every case returns, and the statements
// after it in its block end in a return. It returns the lines of those
// statements, which the new function copies, and the case to add.
func (x *extraction) missingCase(a args, err error) (int, int, *missingCase, error) {
	if a.Case == "default" || !strings.HasPrefix(err.Error(), "no case ") {
		return 0, 0, nil, err
	}
	fd := locate.Func(x.File, a.Function)
	var switches []*ast.SwitchStmt
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if sw, ok := n.(*ast.SwitchStmt); ok {
			switches = append(switches, sw)
		}
		return true
	})
	if len(switches) != 1 {
		return 0, 0, nil, err
	}
	sw := switches[0]
	if x.line(sw.Body.Lbrace) == x.line(sw.Body.Rbrace) {
		return 0, 0, nil, err
	}
	for _, s := range sw.Body.List {
		if cc := s.(*ast.CaseClause); cc.List == nil || !analysis.TerminatingList(x.Info, cc.Body, nil) {
			return 0, 0, nil, err
		}
	}
	path, _ := astutil.PathEnclosingInterval(x.File, sw.Pos(), sw.End())
	block, ok := path[1].(*ast.BlockStmt)
	if !ok {
		return 0, 0, nil, err
	}
	rest := block.List[slices.Index(block.List, ast.Stmt(sw))+1:]
	if len(rest) == 0 || !analysis.TerminatingList(x.Info, rest, nil) {
		return 0, 0, nil, err
	}
	value := a.Case
	if token.IsIdentifier(value) && x.Pkg.Types.Scope().Lookup(value) == nil && types.Universe.Lookup(value) == nil {
		value = strconv.Quote(value) // a bare word, as for a case that is there
	}
	line := x.line(sw.Pos())
	m := &missingCase{
		value:  value,
		at:     x.Off(x.TF.LineStart(x.line(sw.Body.Rbrace))),
		indent: string(x.Src[x.Off(x.TF.LineStart(line)):x.Off(sw.Pos())]),
	}
	return x.line(rest[0].Pos()), x.line(rest[len(rest)-1].End()), m, nil
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
	if s := locate.SignatureAt(x.TF, x.File, first, last); s != nil {
		return signatureRefusal(s)
	}
	var lists []stmtList
	ast.Inspect(x.File, func(n ast.Node) bool {
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
	lo := x.Off(x.TF.LineStart(first))
	hi := len(x.Src)
	if last < x.TF.LineCount() {
		hi = x.Off(x.TF.LineStart(last+1)) - 1 // the newline ending the last line
	}
	sFrom, sTo := x.Off(x.stmts[0].Pos()), x.Off(x.stmts[len(x.stmts)-1].End())
	x.start, x.end = -1, -1
	for _, t := range x.tokens() {
		if t.end <= lo || t.off >= hi || t.tok == token.SEMICOLON {
			continue
		}
		inStmts := t.off >= sFrom && t.end <= sTo
		if !inStmts && (t.tok != token.COMMENT || t.off < lo || t.end > hi) {
			l := x.TF.Line(x.TF.Pos(max(t.off, lo)))
			return refactor.Refusef("line %d contains %s, which is not part of the selected statements; the lines must cover whole statements of one block. %s",
				l, quoteText(string(x.Src[t.off:t.end])), x.statementsAround(stmtList{x.block, x.list}, first))
		}
		if x.start < 0 {
			x.start = t.off
		}
		x.end = t.end
	}
	// Widen to whole lines: from the start of the first line to the end of
	// the last (without its newline).
	x.start = x.Off(x.TF.LineStart(x.line(x.TF.Pos(x.start))))
	if e := strings.IndexByte(string(x.Src[x.end:]), '\n'); e >= 0 {
		x.end += e
	} else {
		x.end = len(x.Src)
	}
	return nil
}

// signatureRefusal refuses lines that start on the signature of s's
// function, with the lines of its statements to send instead.
func signatureRefusal(s *locate.Signature) error {
	switch {
	case s.Empty:
		return refactor.Refusef("%s; its body has no statements to extract", s.Text)
	case s.First == 0:
		return refactor.Refusef("%s; its statements are on a line with a brace of the body, which lines cannot select", s.Text)
	}
	return refactor.Refusef("%s; to extract its statements, use start_line %d, end_line %d", s.Text, s.First, s.Last)
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
	for _, d := range x.File.Decls {
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
		x.toks = scanTokens(x.Src)
	}
	return x.toks
}

// scanTokens scans src, including comments.
func scanTokens(src []byte) []tok {
	var out []tok
	for _, t := range textutil.Scan(src) {
		out = append(out, tok{t.Off, t.End, t.Tok})
	}
	return out
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
				if id, ok := ast.Unparen(n.Fun).(*ast.Ident); ok && x.Info.Uses[id] == recov {
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
			obj := x.Info.Defs[ls.Label]
			for id, o := range x.Info.Uses {
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
	for _, n := range analysis.PathTo(x.File, x.stmts[0]) {
		switch n := n.(type) {
		case *ast.FuncLit:
			body, sig = n.Body, typeOf[*types.Signature](x.Info.TypeOf(n))
		case *ast.FuncDecl:
			if obj, ok := x.Info.Defs[n.Name].(*types.Func); ok {
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
	last := x.stmts[len(x.stmts)-1]
	if analysis.TerminatingList(x.Info, body.List, nil) && !analysis.TerminatingList(x.Info, body.List, last) {
		return refactor.Refusef("the selected lines end with %s at line %d, which is how %s ends without a return; a call to a new function does not count as that, so the code would no longer compile. Leave that statement out of the selection",
			x.firstLine(last), x.line(last.Pos()), x.enclosingName())
	}
	return nil
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
		if obj := x.Info.Uses[b.Label]; obj != nil && !x.inSelectionPos(obj.Pos()) {
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
	// pointers are parameters passed as &v: other code can reach them
	// (their address is taken, or a function literal uses them).
	pointers map[*types.Var]bool
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
	var hoistObjs []types.Object
	seen := map[types.Object]bool{}
	for _, s := range x.stmts {
		ast.Inspect(s, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			obj := x.Info.Uses[id]
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
			if tn, ok := obj.(*types.TypeName); ok && x.nameable[tn] {
				return true // declared by the new method's receiver
			}
			if !seen[obj] {
				seen[obj] = true
				if hoistable(obj) {
					hoistObjs = append(hoistObjs, obj)
				} else {
					bad = append(bad, fmt.Sprintf("%s (%s declared at line %d)", obj.Name(), kindOf(obj), x.line(obj.Pos())))
				}
			}
			return true
		})
	}
	if len(bad) > 0 {
		return refactor.Refusef("the selected lines use %s, declared outside them in %s; only variables can be passed to the new function, and local constants and types moved to package level, so type parameters of the function are not supported yet",
			strings.Join(bad, ", "), x.enclosingName())
	}

	f := &flow{fresh: map[*types.Var]bool{}, pointers: map[*types.Var]bool{}}
	for _, v := range order {
		switch {
		case x.fa.Addressed[v], x.declaredOutsideLit(v) && len(x.fa.Modified[v]) > 0,
			x.fa.CapturedAsync[v] && len(x.fa.Modified[v]) > 0 && x.sharedWithCaller(v),
			x.fa.CapturedAsync[v] && x.sharedLoopVar(v):
			// Other code can reach v while the statements run: pass a
			// pointer, so that both work on the same variable. (A variable
			// that function literals use but nothing ever assigns cannot
			// change, so a copy of it is just as good. Neither can a
			// function literal that only runs during the statement that
			// creates it, like a sort.Slice comparison, reach v while the
			// new function runs: outside the selection it runs before or
			// after the call, and inside it it uses the new function's v.
			// That does not hold for a literal containing the selection:
			// v outlives each call of it, and code after the literal reads
			// v even when the selection ends in a return, which would
			// leave no result to assign back. Nor does a literal of the
			// selection that can run later need v shared, when nothing
			// outside the selection sees v again: see sharedWithCaller.)
			if v == x.recv && analysis.IsPointer(v.Type()) {
				return refactor.Refusef("the receiver %s is a pointer whose own address is taken, or a function literal uses it; a method cannot have a pointer-to-pointer receiver", v.Name())
			}
			f.params = append(f.params, v)
			f.pointers[v] = true
			continue
		}
		written, read := x.writtenIn(v), x.readIn(v)
		usedOutside := x.usedOutside(v)
		inLoop := x.loopAround(v)
		// After statements that always return, nothing reads a result.
		out := x.mode != terminal && written && (usedOutside || inLoop && read)
		switch {
		case out && v != x.recv && x.assignedFirst(v, refs[v]):
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
	for id, obj := range x.Info.Uses {
		if done[obj] || !isLocal(obj) || !x.inSelectionPos(obj.Pos()) || x.inSelection(id) {
			continue
		}
		done[obj] = true
		if v, ok := obj.(*types.Var); ok && x.declaredAtTop(v) {
			later = append(later, v)
			continue
		}
		if hoistable(obj) && x.declaredAtTopObj(obj) {
			hoistObjs = append(hoistObjs, obj)
			continue
		}
		badLater = append(badLater, fmt.Sprintf("%s (used at line %d)", obj.Name(), x.line(id.Pos())))
	}
	if len(badLater) > 0 {
		sort.Strings(badLater)
		return refactor.Refusef("the selected lines declare %s, used after them, which cannot be returned from the new function (only variables declared directly in the selected statements can); include those uses in the selection, or select fewer statements",
			strings.Join(badLater, ", "))
	}
	// Local types in the new function's signature (or at the call site)
	// must move too, even when the statements never write their names.
	have := map[types.Object]bool{}
	for _, o := range hoistObjs {
		have[o] = true
	}
	for _, list := range [][]*types.Var{f.params, f.locals, f.results, later} {
		for _, v := range list {
			for _, tn := range localTypeNames(v.Type(), map[types.Type]bool{}) {
				if !have[tn] {
					have[tn] = true
					hoistObjs = append(hoistObjs, tn)
				}
			}
		}
	}
	sort.Slice(hoistObjs, func(i, j int) bool { return hoistObjs[i].Pos() < hoistObjs[j].Pos() })
	if err := x.planHoists(hoistObjs); err != nil {
		return err
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
		case x.capturedLater(v) && x.writtenAgain(v):
			// A literal that only runs during its statement (an ast.Inspect
			// visitor, say), or only when the selected lines call it, has
			// finished by the time the new function returns v, so a copy
			// loses nothing; nor does one of a variable nothing writes
			// after its declaration, whose copy always holds the same.
			return refactor.Refusef("%s (declared at line %d) is used by a function literal that can run later, and after the selected lines; returning it would return a copy that the function literal no longer shares. Include its later uses in the selection",
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
				if id, ok := n.(*ast.Ident); ok && x.Info.Defs[id] != nil {
					ids = append(ids, id)
				}
				return true
			})
		}
		for _, id := range ids {
			if v := taken[id.Name]; v != nil && x.Info.Defs[id] != nil {
				return refactor.Refusef("line %d declares a new %s while the selected lines also use the %s declared at line %d; in the new function both would be in the same scope. Rename one of them first",
					x.line(id.Pos()), id.Name, id.Name, x.line(v.Pos()))
			}
		}
	}

	x.flow = f
	return x.decideGeneric(later)
}

// findTypeParams records the type parameters of the function declaration
// containing the selection (methods have none of their own), which the
// new function may then use.
func (x *extraction) findTypeParams() {
	var fd *ast.FuncDecl
	for _, n := range analysis.PathTo(x.File, x.stmts[0]) {
		if d, ok := n.(*ast.FuncDecl); ok {
			fd = d
		}
	}
	if fd == nil || fd.Recv != nil || fd.Type.TypeParams == nil || len(fd.Type.TypeParams.List) == 0 {
		return
	}
	tl := fd.Type.TypeParams
	x.tparamText = string(x.Src[x.Off(tl.Opening)+1 : x.Off(tl.Closing)])
	for _, fld := range tl.List {
		for _, name := range fld.Names {
			if tn, ok := x.Info.Defs[name].(*types.TypeName); ok {
				x.tparams = append(x.tparams, tn)
				x.nameable[tn] = true
			}
		}
	}
}

// decideGeneric makes the new function generic if the statements, or the
// types it passes in and out, use the enclosing function's type
// parameters. It then declares all of them (constraints may refer to each
// other) and the call instantiates them explicitly, so nothing depends on
// inference.
func (x *extraction) decideGeneric(later []*types.Var) error {
	if len(x.tparams) == 0 {
		return nil
	}
	set := map[*types.TypeName]bool{}
	for _, tp := range x.tparams {
		set[tp] = true
	}
	used := false
	for id, obj := range x.Info.Uses {
		if tn, ok := obj.(*types.TypeName); ok && set[tn] && x.inSelection(id) {
			used = true
			break
		}
	}
	f := x.flow
	for _, list := range [][]*types.Var{f.params, f.locals, f.results, later} {
		for _, v := range list {
			used = used || usesTypeParam(v.Type(), set, map[types.Type]bool{})
		}
	}
	if _, _, sig := x.enclosingFunc(); x.mode == terminal && sig != nil {
		used = used || usesTypeParam(sig.Results(), set, map[types.Type]bool{})
	}
	if !used {
		return nil
	}
	scope := x.innermostScope(x.stmts[0].Pos())
	for _, tp := range x.tparams {
		if tp.Name() == "_" {
			return refactor.Refusef("%s has a type parameter named _, which the call could not pass on; name it first", x.enclosingName())
		}
		if scope != nil {
			if _, o := scope.LookupParent(tp.Name(), x.stmts[0].Pos()); o != tp {
				return refactor.Refusef("the call would pass the type parameter %s, but at the call site %s refers to the %s declared at line %d; rename it first",
					tp.Name(), tp.Name(), kindOf(o), x.line(o.Pos()))
			}
		}
	}
	x.generic = true
	return nil
}

// usesTypeParam reports whether t mentions one of the type parameters.
func usesTypeParam(t types.Type, set map[*types.TypeName]bool, seen map[types.Type]bool) bool {
	if t == nil || seen[t] {
		return false
	}
	seen[t] = true
	any := func(ts ...types.Type) bool {
		for _, e := range ts {
			if usesTypeParam(e, set, seen) {
				return true
			}
		}
		return false
	}
	switch t := t.(type) {
	case *types.TypeParam:
		return set[t.Obj()]
	case *types.Named:
		for i := 0; i < t.TypeArgs().Len(); i++ {
			if any(t.TypeArgs().At(i)) {
				return true
			}
		}
		// A local type's underlying type can mention them too.
		if o := t.Obj(); o.Pkg() != nil && o.Parent() != o.Pkg().Scope() {
			return any(t.Underlying())
		}
	case *types.Alias:
		return any(types.Unalias(t))
	case *types.Pointer:
		return any(t.Elem())
	case *types.Slice:
		return any(t.Elem())
	case *types.Array:
		return any(t.Elem())
	case *types.Chan:
		return any(t.Elem())
	case *types.Map:
		return any(t.Key(), t.Elem())
	case *types.Signature:
		return any(t.Params(), t.Results())
	case *types.Tuple:
		for i := 0; i < t.Len(); i++ {
			if any(t.At(i).Type()) {
				return true
			}
		}
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if any(t.Field(i).Type()) {
				return true
			}
		}
	case *types.Interface:
		for i := 0; i < t.NumMethods(); i++ {
			if any(t.Method(i).Type()) {
				return true
			}
		}
		for i := 0; i < t.NumEmbeddeds(); i++ {
			if any(t.EmbeddedType(i)) {
				return true
			}
		}
	}
	return false
}

// findReceiver makes the new function a method when the statements use the
// receiver of the method they are directly in (not a function literal in
// it): x.recv, x.recvType and the receiver's type parameters.
func (x *extraction) findReceiver() {
	var fd *ast.FuncDecl
	for _, n := range analysis.PathTo(x.File, x.stmts[0]) {
		if _, ok := n.(*ast.FuncLit); ok {
			return // a function literal's statements cannot become a method
		}
		if d, ok := n.(*ast.FuncDecl); ok {
			fd = d
			break
		}
	}
	if fd == nil || fd.Recv == nil || len(fd.Recv.List) != 1 || len(fd.Recv.List[0].Names) != 1 {
		return
	}
	v, ok := x.Info.Defs[fd.Recv.List[0].Names[0]].(*types.Var)
	if !ok || v.Name() == "_" {
		return
	}
	used := false
	for id, obj := range x.Info.Uses {
		if obj == v && x.inSelection(id) {
			used = true
			break
		}
	}
	if !used {
		return
	}
	x.recv = v
	rt := fd.Recv.List[0].Type
	x.recvType = string(x.Src[x.Off(rt.Pos()):x.Off(rt.End())])
	if fn, ok := x.Info.Defs[fd.Name].(*types.Func); ok {
		tps := fn.Signature().RecvTypeParams()
		for i := 0; i < tps.Len(); i++ {
			x.nameable[tps.At(i).Obj()] = true
		}
	}
}

// checkMethodName refuses a method name that would clash with a field or
// method of the receiver's type, or could change which interfaces it (or
// a type embedding it) implements.
func (x *extraction) checkMethodName() error {
	t := x.recv.Type()
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return refactor.Refusef("cannot add a method to the receiver type %s", t)
	}
	named = named.Origin()
	if token.IsExported(x.name) {
		if iface := analysis.MethodInterface(x.WS.Pkgs, named, x.name, true); iface != "" {
			return refactor.Refusef("a method %s could make %s implement %s, which code checks for at run time, and change behavior with no compile error; choose another name",
				x.name, named.Obj().Name(), iface)
		}
	}
	if o, _, _ := types.LookupFieldOrMethod(types.NewPointer(named), false, x.Pkg.Types, x.name); o != nil {
		return refactor.Refusef("%s already has a %s named %s (at %s); choose another name",
			named.Obj().Name(), kindOf(o), x.name, x.WS.Position(o.Pos()))
	}
	// An unexported method can only matter to interfaces in this package.
	var scopes []*types.Scope
	for _, p := range x.WS.Pkgs {
		if p.PkgPath == x.Pkg.PkgPath && p.Types != nil {
			scopes = append(scopes, p.Types.Scope())
		}
	}
	gains := []*types.Named{named} // types whose method sets would gain the method
	for changed := true; changed; {
		changed = false
		for _, sc := range scopes {
			for _, n := range sc.Names() {
				tn, ok := sc.Lookup(n).(*types.TypeName)
				if !ok {
					continue
				}
				cand, ok := tn.Type().(*types.Named)
				if !ok || containsNamed(gains, cand) {
					continue
				}
				if st, ok := cand.Underlying().(*types.Struct); ok && embedsAny(st, gains) {
					gains = append(gains, cand)
					changed = true
				}
			}
		}
	}
	for _, sc := range scopes {
		for _, n := range sc.Names() {
			tn, ok := sc.Lookup(n).(*types.TypeName)
			if !ok {
				continue
			}
			iface, ok := tn.Type().Underlying().(*types.Interface)
			if !ok {
				continue
			}
			has := false
			for i := 0; i < iface.NumMethods(); i++ {
				if m := iface.Method(i); m.Name() == x.name && m.Pkg() == x.Pkg.Types {
					has = true
				}
			}
			if !has {
				continue
			}
			for _, g := range gains {
				if implementsExcept(g, iface, x.name) {
					return refactor.Refusef("adding method %s to %s would make %s implement interface %s, which could change behavior; choose another name",
						x.name, named.Obj().Name(), g.Obj().Name(), tn.Name())
				}
			}
		}
	}
	return nil
}

func containsNamed(list []*types.Named, t *types.Named) bool {
	for _, l := range list {
		if l == t.Origin() || l == t {
			return true
		}
	}
	return false
}

// embedsAny reports whether st has an embedded field of one of the types
// (or a pointer to one).
func embedsAny(st *types.Struct, types_ []*types.Named) bool {
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		if !f.Embedded() {
			continue
		}
		t := f.Type()
		if p, ok := t.(*types.Pointer); ok {
			t = p.Elem()
		}
		if n, ok := t.(*types.Named); ok && containsNamed(types_, n.Origin()) {
			return true
		}
	}
	return false
}

// implementsExcept reports whether *t has every method of iface other than
// the one named except.
func implementsExcept(t *types.Named, iface *types.Interface, except string) bool {
	for i := 0; i < iface.NumMethods(); i++ {
		m := iface.Method(i)
		if m.Name() == except {
			continue
		}
		if o, _, _ := types.LookupFieldOrMethod(types.NewPointer(t), false, m.Pkg(), m.Name()); o == nil {
			return false
		} else if _, isFunc := o.(*types.Func); !isFunc {
			return false
		}
	}
	return true
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
			if read || !ok || x.Info.Uses[id] != v {
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

// isDefineTarget reports whether id is on the left of :=.
func (x *extraction) isDefineTarget(id *ast.Ident) bool {
	path := analysis.PathTo(x.File, id)
	if len(path) < 2 {
		return false
	}
	as, ok := path[1].(*ast.AssignStmt)
	if !ok || as.Tok != token.DEFINE {
		return false
	}
	for _, l := range as.Lhs {
		if l == id {
			return true
		}
	}
	return false
}

// pointerEdits rewrites the selection's uses of variables passed by
// pointer: &v becomes v, v.f and v.M() stay (Go dereferences them), and
// other uses become *v, or (*v) where an operand needs parentheses.
func (x *extraction) pointerEdits() []edit.TextEdit {
	var edits []edit.TextEdit
	redeclared := map[*ast.AssignStmt]bool{}
	for _, s := range x.stmts {
		ast.Inspect(s, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			v, ok := x.Info.Uses[id].(*types.Var)
			if !ok || !x.flow.pointers[v] {
				return true
			}
			path := analysis.PathTo(x.File, id)
			if as, ok := path[1].(*ast.AssignStmt); ok && x.isDefineTarget(id) && !redeclared[as] {
				// v := redeclared: v is now a pointer, so declare the new
				// names with var and assign through it.
				redeclared[as] = true
				edits = append(edits, x.redeclareEdits(as)...)
			}
			// The operand id is part of, skipping parentheses.
			var operand ast.Node = id
			i := 1
			for ; i < len(path); i++ {
				if _, paren := path[i].(*ast.ParenExpr); !paren {
					break
				}
				operand = path[i]
			}
			var parent ast.Node
			if i < len(path) {
				parent = path[i]
			}
			star := "*" + v.Name()
			paren := "(*" + v.Name() + ")"
			repl := star
			switch p := parent.(type) {
			case *ast.UnaryExpr:
				if p.Op == token.AND {
					edits = append(edits, edit.TextEdit{Start: x.Off(p.Pos()), End: x.Off(p.End()), NewText: v.Name()})
					return true
				}
			case *ast.SelectorExpr:
				if p.X == operand {
					switch v.Type().Underlying().(type) {
					case *types.Pointer, *types.Interface:
						repl = paren
					default:
						return true // the selector dereferences the pointer
					}
				}
			case *ast.IndexExpr:
				if p.X == operand {
					repl = paren
				}
			case *ast.IndexListExpr:
				if p.X == operand {
					repl = paren
				}
			case *ast.SliceExpr:
				if p.X == operand {
					repl = paren
				}
			case *ast.CallExpr:
				if p.Fun == operand {
					repl = paren
				}
			case *ast.TypeAssertExpr:
				if p.X == operand {
					repl = paren
				}
			}
			edits = append(edits, edit.TextEdit{Start: x.Off(id.Pos()), End: x.Off(id.End()), NewText: repl})
			return true
		})
	}
	return edits
}

// redeclareEdits returns the edits that make as, a := that redeclares a
// variable passed to the new function as a pointer, an = after var
// declarations of the names it declares: a, v := f() becomes var a T,
// then a, *v = f().
func (x *extraction) redeclareEdits(as *ast.AssignStmt) []edit.TextEdit {
	var decls []string
	for _, l := range as.Lhs {
		id, ok := l.(*ast.Ident)
		if !ok {
			continue
		}
		if v, ok := x.Info.Defs[id].(*types.Var); ok && id.Name != "_" {
			t, err := x.typeString(v.Name(), v.Type())
			if err != nil {
				return nil
			}
			decls = append(decls, "var "+v.Name()+" "+t+"\n"+x.indentOf(as.Pos()))
		}
	}
	return []edit.TextEdit{
		{Start: x.Off(as.Pos()), End: x.Off(as.Pos()), NewText: strings.Join(decls, "")},
		{Start: x.Off(as.TokPos), End: x.Off(as.TokPos) + 2, NewText: "="},
	}
}

// isAssignTarget reports whether id is a whole left-hand operand of = or
// := (which writes without reading).
func (x *extraction) isAssignTarget(id *ast.Ident) bool {
	path := analysis.PathTo(x.File, id)
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
	for _, n := range analysis.PathTo(x.File, x.stmts[0]) {
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
					if x.Info.Defs[name] == v {
						return true
					}
				}
			}
		}
	}
	for id, obj := range x.Info.Uses {
		if obj == v && !x.inSelection(id) {
			return true
		}
	}
	return false
}

// declaredOutsideLit reports whether the selection is inside a function
// literal and v is declared outside the innermost one: the literal
// captures v, which outlives each call of it.
func (x *extraction) declaredOutsideLit(v *types.Var) bool {
	for _, n := range analysis.PathTo(x.File, x.stmts[0]) {
		if lit, ok := n.(*ast.FuncLit); ok {
			return v.Pos() < lit.Pos() || v.Pos() >= lit.End()
		}
	}
	return false
}

// loopAround reports whether the selection is inside a loop (or a function
// that jumps backwards with goto) that v is declared outside of, so that a
// value the selection stores can be read by the selection itself in the
// next iteration.
func (x *extraction) loopAround(v *types.Var) bool {
	for _, n := range analysis.PathTo(x.File, x.stmts[0]) {
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

// sharedWithCaller reports whether code outside the selection can see the
// changes that a function literal able to run later makes to v: a literal
// outside the selection uses v, code outside it uses v, or a loop runs the
// selection again. If none can, the new function may change its own copy.
func (x *extraction) sharedWithCaller(v *types.Var) bool {
	for _, lit := range x.fa.AsyncLits[v] {
		if !x.inSelection(lit) {
			return true
		}
	}
	return x.usedOutside(v) || x.loopAround(v)
}

// sharedLoopVar reports whether v is the variable of a for or range loop
// around the selection and the file's Go version is before 1.22, when
// every iteration shares the variable: the loop changes v after a literal
// of the selection that can run later has captured it.
func (x *extraction) sharedLoopVar(v *types.Var) bool {
	if fv := x.Info.FileVersions[x.File]; fv == "" || version.Compare(fv, "go1.22") >= 0 {
		return false
	}
	for _, n := range analysis.PathTo(x.File, x.stmts[0]) {
		var body *ast.BlockStmt
		switch n := n.(type) {
		case *ast.ForStmt:
			body = n.Body
		case *ast.RangeStmt:
			body = n.Body
		}
		if body != nil && n.Pos() <= v.Pos() && v.Pos() < body.Pos() {
			return true
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
	as := analysis.PathTo(x.File, first)[1].(*ast.AssignStmt)
	top := false
	for _, s := range x.stmts {
		top = top || s == as
	}
	if !top {
		return false
	}
	for _, r := range as.Rhs {
		for _, id := range analysis.ScopeRefs(x.Info, r) {
			if x.Info.Uses[id] == v {
				return false
			}
		}
	}
	return true
}

// hoistable reports whether obj is a local constant or type (not a type
// parameter), which can move to package level.
func hoistable(obj types.Object) bool {
	switch o := obj.(type) {
	case *types.Const:
		return true
	case *types.TypeName:
		_, tp := o.Type().(*types.TypeParam)
		return !tp
	}
	return false
}

// declaredAtTopObj reports whether obj is declared by a declaration
// statement that is one of the selected statements.
func (x *extraction) declaredAtTopObj(obj types.Object) bool {
	for _, s := range x.stmts {
		if ds, ok := s.(*ast.DeclStmt); ok && obj.Pos() >= ds.Pos() && obj.Pos() < ds.End() {
			return true
		}
	}
	return false
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
	what := "the result type " + types.TypeString(t, types.RelativeTo(x.Pkg.Types))
	if name != "" {
		what = fmt.Sprintf("%s has type %s, which", name, types.TypeString(t, types.RelativeTo(x.Pkg.Types)))
	}
	if bad := localTypeIn(t, x.nameable, map[types.Type]bool{}); bad != nil {
		return "", refactor.Refusef("%s uses %s %s declared inside %s; it cannot be named in a new package-level function",
			what, kindOf(bad), bad.Name(), x.enclosingName())
	}
	var refusal error
	s := types.TypeString(t, func(p *types.Package) string {
		n, err := x.qualifier(p)
		if err != nil && refusal == nil {
			refusal = refactor.Refusef("%s needs package %q, %s", what, p.Path(), err)
		}
		return n
	})
	return s, refusal
}

// qualifiers returns the package names t is spelled with in this file
// (after typeString has checked it).
func (x *extraction) qualifiers(t types.Type) []string {
	var out []string
	types.TypeString(t, func(p *types.Package) string {
		n, _ := x.qualifier(p)
		if n != "" {
			out = append(out, n)
		}
		return n
	})
	return out
}

// qualifier returns the name that refers to package p in this file,
// adding an import of p if the file has none. The error says why p cannot
// be imported.
func (x *extraction) qualifier(p *types.Package) (string, error) {
	if x.imp == nil {
		x.imp = imports.New(x.WS, x.Pkg, x.File, x.Src)
	}
	return x.imp.Name(p)
}

// importEdits adds the imports that typeString needed.
func (x *extraction) importEdits() []edit.TextEdit {
	if x.imp == nil {
		return nil
	}
	return x.imp.Edits()
}

// localTypeIn returns a type name in t that cannot be written outside the
// function: a type parameter or a type declared in a function body.
func localTypeIn(t types.Type, allowed map[*types.TypeName]bool, seen map[types.Type]bool) *types.TypeName {
	if seen[t] {
		return nil
	}
	seen[t] = true
	switch t := t.(type) {
	case *types.TypeParam:
		if allowed[t.Obj()] {
			return nil
		}
		return t.Obj()
	case *types.Named:
		if o := t.Obj(); !allowed[o] && o.Parent() != nil && o.Pkg() != nil && o.Parent() != o.Pkg().Scope() && o.Parent() != types.Universe {
			return o
		}
		if b := localTypeIn(t.Underlying(), allowed, seen); b != nil && allowed[t.Obj()] {
			return b // a moved type must not use other local types either
		}
		for i := 0; i < t.TypeArgs().Len(); i++ {
			if b := localTypeIn(t.TypeArgs().At(i), allowed, seen); b != nil {
				return b
			}
		}
	case *types.Alias:
		if o := t.Obj(); !allowed[o] && o.Parent() != nil && o.Pkg() != nil && o.Parent() != o.Pkg().Scope() && o.Parent() != types.Universe {
			return o
		}
		return localTypeIn(types.Unalias(t), allowed, seen)
	case *types.Pointer:
		return localTypeIn(t.Elem(), allowed, seen)
	case *types.Slice:
		return localTypeIn(t.Elem(), allowed, seen)
	case *types.Array:
		return localTypeIn(t.Elem(), allowed, seen)
	case *types.Chan:
		return localTypeIn(t.Elem(), allowed, seen)
	case *types.Map:
		if b := localTypeIn(t.Key(), allowed, seen); b != nil {
			return b
		}
		return localTypeIn(t.Elem(), allowed, seen)
	case *types.Signature:
		if b := localTypeIn(t.Params(), allowed, seen); b != nil {
			return b
		}
		return localTypeIn(t.Results(), allowed, seen)
	case *types.Tuple:
		for i := 0; i < t.Len(); i++ {
			if b := localTypeIn(t.At(i).Type(), allowed, seen); b != nil {
				return b
			}
		}
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if b := localTypeIn(t.Field(i).Type(), allowed, seen); b != nil {
				return b
			}
		}
	case *types.Interface:
		for i := 0; i < t.NumMethods(); i++ {
			if b := localTypeIn(t.Method(i).Type(), allowed, seen); b != nil {
				return b
			}
		}
		for i := 0; i < t.NumEmbeddeds(); i++ {
			if b := localTypeIn(t.EmbeddedType(i), allowed, seen); b != nil {
				return b
			}
		}
	}
	return nil
}

// localTypeNames returns the types declared in a function body that t
// uses (not type parameters).
func localTypeNames(t types.Type, seen map[types.Type]bool) []*types.TypeName {
	var out []*types.TypeName
	allowed := map[*types.TypeName]bool{}
	for {
		tn := localTypeIn(t, allowed, map[types.Type]bool{})
		if tn == nil {
			return out
		}
		if _, tp := tn.Type().(*types.TypeParam); !tp {
			out = append(out, tn)
		}
		allowed[tn] = true
	}
}

// outermostFunc returns the outermost function containing the selection.
func (x *extraction) outermostFunc() ast.Node {
	return analysis.OutermostFunc(analysis.PathTo(x.File, x.stmts[0]))
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
	if x.recv != nil {
		return x.checkMethodName()
	}
	if o := types.Universe.Lookup(x.name); o != nil {
		return refactor.Refusef("%s is a predeclared %s; a package-level function with that name would hide it in the whole package. Choose another name", x.name, kindOf(o))
	}
	for _, p := range x.WS.Pkgs {
		if p.PkgPath != x.Pkg.PkgPath || p.Types == nil {
			continue
		}
		if o := p.Types.Scope().Lookup(x.name); o != nil {
			return refactor.Refusef("%s is already declared in package %s (%s at %s); choose another name",
				x.name, p.Types.Name(), kindOf(o), x.WS.Position(o.Pos()))
		}
		for _, f := range p.Syntax {
			if s := p.TypesInfo.Scopes[f]; s != nil {
				if o := s.Lookup(x.name); o != nil {
					return refactor.Refusef("%s is an imported name in %s; choose another name", x.name, x.WS.Rel(x.WS.Fset.File(f.Pos()).Name()))
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
		if s, o := scope.LookupParent(x.name, first.Pos()); o != nil && s != types.Universe && s != x.Pkg.Types.Scope() && !x.inSelectionPos(o.Pos()) {
			return refactor.Refusef("%s is a %s declared at line %d, which would hide the new function at the call site; choose another name",
				x.name, kindOf(o), x.line(o.Pos()))
		}
	}
	return nil
}

// innermostScope returns the innermost scope containing pos.
func (x *extraction) innermostScope(pos token.Pos) *types.Scope {
	s := x.Info.Scopes[x.File]
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
	if err := x.checkSpelledTypes(); err != nil {
		return nil, err
	}

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
		results = append(results, ts("", x.exitType))
		bodyEdits = x.rewriteExits(zeros)
		tail = strings.Join(append(names(f.results), x.exitZero()), ", ")
		errName = x.errorName(f.results)
	default:
		for _, v := range f.results {
			results = append(results, ts(v.Name(), v.Type()))
		}
		if len(f.results) > 0 && !analysis.TerminatingList(x.Info, x.stmts, nil) {
			tail = strings.Join(names(f.results), ", ")
		}
	}

	// The new function.
	var params []string
	for _, v := range f.params {
		if v == x.recv {
			continue
		}
		t := ts(v.Name(), v.Type())
		if f.pointers[v] {
			t = "*" + t
		}
		params = append(params, v.Name()+" "+t)
	}
	tps := ""
	if x.generic {
		tps = "[" + x.tparamText + "]"
	}
	sigText := fmt.Sprintf("func %s%s(%s)", x.name, tps, strings.Join(params, ", "))
	if x.recv != nil {
		rt := x.recvType
		if f.pointers[x.recv] {
			rt = "*" + rt // a value receiver whose address is taken
		}
		sigText = fmt.Sprintf("func (%s %s) %s(%s)", x.recv.Name(), rt, x.name, strings.Join(params, ", "))
	}
	switch len(results) {
	case 0:
	case 1:
		sigText += " " + results[0]
	default:
		sigText += " (" + strings.Join(results, ", ") + ")"
	}
	unit := x.indentUnit(decl)
	var fn strings.Builder
	fmt.Fprintf(&fn, "\n\n%s%s {\n", gocode.DocComment(x.doc, gocode.CommentWidth(x.Src)), sigText)
	for _, v := range f.locals {
		fmt.Fprintf(&fn, "%svar %s %s\n", unit, v.Name(), ts(v.Name(), v.Type()))
	}
	hoistFile, hoistBody := x.hoistEdits(decl)
	text, err := x.selectedText(append(append(bodyEdits, x.pointerEdits()...), hoistBody...))
	if err != nil {
		return nil, err
	}
	fn.WriteString(reindent(text, indent, unit))
	if tail != "" {
		fmt.Fprintf(&fn, "\n%sreturn %s", unit, tail)
	}
	fn.WriteString("\n}")

	at := x.insertOffset(decl)

	// The call.
	final := x.finalReturn(f)
	siteText := x.callSite(f, sig, indent, names, ts, errName, final)
	end := x.end
	if final != nil {
		end = x.Off(final.End())
	}
	site := edit.TextEdit{Start: x.start, End: end, NewText: siteText}
	notes := x.notes(f, names)
	if m := x.missing; m != nil {
		// The code after the switch stays; the new case calls its copy.
		site = edit.TextEdit{Start: m.at, End: m.at, NewText: m.indent + "case " + m.value + ":\n" + gocode.Reindent(siteText, m.indent+"\t", false) + "\n"}
		notes = append(notes, fmt.Sprintf("no case %s: the code after the switch runs for it, so the new case %s calls %s, a copy of that code, which stays", m.value, m.value, x.name))
	}
	edits := []edit.TextEdit{
		site,
		{Start: at, End: at, NewText: fn.String()},
	}
	edits = append(edits, x.importEdits()...)
	edits = append(edits, hoistFile...)
	edits = append(edits, x.split...)

	if final != nil {
		notes = append(notes, fmt.Sprintf("the return after the selected lines returned what the new function's error says, so the call site is %q", strings.TrimSpace(siteText)))
	}
	first, last := x.line(x.TF.Pos(x.start)), x.line(x.TF.Pos(x.end))
	n := len(x.stmts)
	return &refactor.Plan{
		Summary: fmt.Sprintf("extracted lines %d-%d (%d statement%s) into function %s", first, last, n, textutil.Plural(n), x.name),
		Edits:   []edit.FileEdit{{Path: x.path, Edits: edits}},
		Notes:   notes,
	}, nil
}

// callSite writes the statements that replace the selection. With final,
// the return after the selection, the call site returns the exit values
// and the new function's error in its place; an error that is only checked
// is declared in the if that checks it.
func (x *extraction) callSite(f *flow, sig *types.Signature, indent string, names func(vs []*types.Var) []string, ts func(name string, t types.Type) string, errName string, final *ast.ReturnStmt) string {
	call := x.callExpr(f)
	var site strings.Builder
	if final != nil {
		return fmt.Sprintf("%sreturn %s", indent, strings.Join(append(append([]string(nil), x.exitValues...), call), ", "))
	}
	if errName != "" && len(f.results) == 0 {
		// The error is only checked: declare it in the if, where it hides
		// nothing the call or the return needs.
		name := "err"
		if !x.isError() || slices.ContainsFunc(x.exitValues, func(v string) bool { return slices.Contains(identsIn(v), name) }) {
			name = errName
		}
		vals := strings.Join(append(append([]string(nil), x.exitValues...), name), ", ")
		return fmt.Sprintf("%sif %s := %s; %s != %s {\n%s\treturn %s\n%s}", indent, name, call, name, x.exitZero(), indent, vals, indent)
	}
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
			fresh = append(fresh, fmt.Sprintf("var %s %s", errName, ts("", x.exitType)))
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
			fmt.Fprintf(&site, "\n%sif %s != %s {\n%s\treturn %s\n%s}", indent, errName, x.exitZero(), indent, vals, indent)
		}
	}

	return site.String()
}

func (x *extraction) notes(f *flow, names func(vs []*types.Var) []string) []string {
	var notes []string
	if x.recv != nil {
		notes = append(notes, fmt.Sprintf("the selected lines use the receiver %s, so %s is a method of %s", x.recv.Name(), x.name, strings.TrimPrefix(x.recvType, "*")))
	}
	var shown []*types.Var
	for _, v := range f.params {
		if v != x.recv {
			shown = append(shown, v)
		}
	}
	if len(shown) > 0 || len(f.results) > 0 {
		describe := func(vs []*types.Var) string {
			if len(vs) == 0 {
				return "none"
			}
			return strings.Join(names(vs), ", ")
		}
		notes = append(notes, fmt.Sprintf("parameters: %s; results: %s", describe(shown), describe(f.results)))
	}
	if np, nr := len(shown), len(f.results); np > 5 || nr > 3 {
		notes = append(notes, fmt.Sprintf("%s takes %d parameter%s and returns %d result%s; so many usually means the selection cuts through code that shares many variables, and a different range of lines (or grouping the values in a struct) may give a simpler function",
			x.name, np, textutil.Plural(np), nr, textutil.Plural(nr)))
	}
	var ptrs []string
	for _, v := range f.params {
		if f.pointers[v] && v != x.recv {
			ptrs = append(ptrs, v.Name())
		}
	}
	if len(ptrs) > 0 {
		notes = append(notes, fmt.Sprintf("%s %s passed as a pointer, because %s address is taken or a function literal uses %s; the new function changes the same variable",
			strings.Join(ptrs, ", "), isAre(len(ptrs)), map[bool]string{true: "its", false: "their"}[len(ptrs) == 1], itThem(len(ptrs))))
	}
	if len(x.hoists) > 0 {
		notes = append(notes, x.hoistNote())
	}
	if x.generic {
		notes = append(notes, fmt.Sprintf("the selected lines use type parameters of %s, so the new function declares them too and the call passes them explicitly", x.enclosingName()))
	}
	if x.imp != nil && len(x.imp.Paths()) > 0 {
		var imps []string
		for _, p := range x.imp.Paths() {
			imps = append(imps, strconv.Quote(p))
		}
		notes = append(notes, "added import "+strings.Join(imps, ", ")+" for the new function's types")
	}
	switch x.mode {
	case terminal:
		notes = append(notes, "the selected lines always return, so the call site returns what the new function returns")
	case errorExit:
		if x.isError() {
			notes = append(notes, fmt.Sprintf("the early returns now return their error from %s, and the call site returns it when it is not nil", x.name))
		} else {
			notes = append(notes, fmt.Sprintf("the early returns now return their last value from %s, and the call site returns it when it is not %s", x.name, x.exitZero()))
		}
	}
	if len(f.loopCarried) > 0 {
		notes = append(notes, fmt.Sprintf("%s %s returned because the selected lines are in a loop and may read %s in the next iteration; if not, the result can be dropped",
			strings.Join(f.loopCarried, ", "), isAre(len(f.loopCarried)), itThem(len(f.loopCarried))))
	}
	return notes
}

func (x *extraction) callExpr(f *flow) string {
	var args []string
	for _, v := range f.params {
		if v == x.recv {
			continue
		}
		if f.pointers[v] {
			args = append(args, "&"+v.Name())
		} else {
			args = append(args, v.Name())
		}
	}
	inst := ""
	if x.generic {
		var tpNames []string
		for _, tp := range x.tparams {
			tpNames = append(tpNames, tp.Name())
		}
		inst = "[" + strings.Join(tpNames, ", ") + "]"
	}
	call := fmt.Sprintf("%s%s(%s)", x.name, inst, strings.Join(args, ", "))
	if x.recv != nil {
		call = x.recv.Name() + "." + call // Go takes the address if needed
	}
	return call
}

func (x *extraction) insertOffset(decl ast.Decl) int {
	// Insert after the line on which the enclosing declaration ends, so a
	// trailing comment there stays with it.
	at := x.Off(decl.End())
	if i := strings.IndexByte(string(x.Src[at:]), '\n'); i >= 0 {
		at += i
	} else {
		at = len(x.Src)
	}
	return at
}

// checkSpelledTypes refuses when a type written out in the function body
// or at the call site would name a package through a name that something
// else hides there: a parameter of the new function, a name the selected
// statements declare, or a local of the enclosing function at the call
// site. (In the new function's signature, package names cannot be hidden.)
func (x *extraction) checkSpelledTypes() error {
	f := x.flow
	inBody := map[string]string{} // names visible in the new function's body
	for _, v := range f.params {
		inBody[v.Name()] = "parameter"
	}
	for id, obj := range x.Info.Defs {
		if obj != nil && x.inSelection(id) {
			inBody[id.Name] = "name declared by the selected statements"
		}
	}
	check := func(v *types.Var) error {
		for _, q := range x.qualifiers(v.Type()) {
			if kind, ok := inBody[q]; ok {
				ts, _ := x.typeString("", v.Type())
				return refactor.Refusef("the new function would declare %s with type %s, but in its body %s is a %s; rename it first",
					v.Name(), ts, q, kind)
			}
		}
		return nil
	}
	for _, v := range f.locals {
		if err := check(v); err != nil {
			return err
		}
	}
	if x.mode == errorExit {
		for _, v := range f.results {
			if err := check(v); err != nil {
				return err
			}
		}
	}
	// The call site declares the results it introduces with var when some
	// results already exist, or when it also declares an error variable.
	declares := x.mode == errorExit // var err error, and var for the others
	for _, v := range f.results {
		declares = declares || !f.fresh[v] // mixed results: var for the fresh ones
	}
	scope := x.innermostScope(x.stmts[0].Pos())
	for _, v := range f.results {
		if !declares || !f.fresh[v] || scope == nil {
			continue
		}
		for _, q := range x.qualifiers(v.Type()) {
			_, o := scope.LookupParent(q, x.stmts[0].Pos())
			if _, isPkg := o.(*types.PkgName); isPkg {
				continue
			}
			if o == nil {
				continue // a new import
			}
			ts, _ := x.typeString("", v.Type())
			return refactor.Refusef("the call site would declare %s with type %s, but there %s refers to the %s declared at line %d; rename it first",
				v.Name(), ts, q, kindOf(o), x.line(o.Pos()))
		}
	}
	return nil
}

// selectedText returns the selected lines with edits (at file offsets
// inside them) applied.
func (x *extraction) selectedText(edits []edit.TextEdit) (string, error) {
	shifted := make([]edit.TextEdit, len(edits))
	for i, e := range edits {
		shifted[i] = edit.TextEdit{Start: e.Start - x.start, End: e.End - x.start, NewText: e.NewText}
	}
	out, err := edit.Apply(x.Src[x.start:x.end], shifted)
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

// reindent re-indents text; see textutil.Reindent.
func reindent(text, from, to string) string { return textutil.Reindent(text, from, to) }

// topDecl returns the top-level declaration containing the selection.
func (x *extraction) topDecl() ast.Decl {
	p := x.stmts[0].Pos()
	for _, d := range x.File.Decls {
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

// capturedLater reports whether a function literal that can run after the
// selected lines uses v.
func (x *extraction) capturedLater(v *types.Var) bool {
	for _, lit := range x.fa.AsyncLits[v] {
		if !x.calledOnlyInSelection(lit) {
			return true
		}
	}
	return false
}

// writtenAgain reports whether the function writes v anywhere but where
// it declares it: then a copy of v returned from the new function and the
// variable a function literal keeps could come to differ.
func (x *extraction) writtenAgain(v *types.Var) bool {
	for _, n := range x.fa.Modified[v] {
		if !(n.Pos() <= v.Pos() && v.Pos() < n.End()) {
			return true
		}
	}
	return false
}

// calledOnlyInSelection reports whether lit runs only while the selected
// lines do: they assign it to a variable they declare (a recursive visit
// function, say), and only call that variable, directly and not in another
// function literal of theirs.
func (x *extraction) calledOnlyInSelection(lit *ast.FuncLit) bool {
	if !x.inSelection(lit) {
		return false
	}
	fn := x.holder(lit)
	if fn == nil || !x.inSelectionPos(fn.Pos()) || x.fa.Addressed[fn] {
		return false
	}
	for id, obj := range x.Info.Uses {
		if obj != fn {
			continue
		}
		if !x.inSelection(id) {
			return false
		}
		path, _ := astutil.PathEnclosingInterval(x.File, id.Pos(), id.End())
		if as, ok := path[1].(*ast.AssignStmt); ok && slices.Contains(as.Lhs, ast.Expr(id)) {
			continue // a write, which runs nothing
		}
		if call, ok := path[1].(*ast.CallExpr); !ok || call.Fun != id {
			return false
		}
		switch path[2].(type) {
		case *ast.GoStmt, *ast.DeferStmt:
			return false
		}
		for _, n := range path[2:] {
			if l, ok := n.(*ast.FuncLit); ok && x.inSelection(l) && l != lit {
				return false
			}
		}
	}
	return true
}

// holder returns the local variable that an assignment or declaration in
// the selected lines sets to lit, or nil.
func (x *extraction) holder(lit *ast.FuncLit) *types.Var {
	var out *types.Var
	set := func(lhs []*ast.Ident, rhs []ast.Expr) {
		if len(lhs) != len(rhs) {
			return
		}
		for i, r := range rhs {
			if lhs[i] != nil && ast.Unparen(r) == lit {
				out, _ = x.Info.ObjectOf(lhs[i]).(*types.Var)
			}
		}
	}
	for _, s := range x.stmts {
		ast.Inspect(s, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.AssignStmt:
				var lhs []*ast.Ident
				for _, l := range n.Lhs {
					id, _ := l.(*ast.Ident)
					lhs = append(lhs, id)
				}
				set(lhs, n.Rhs)
			case *ast.ValueSpec:
				set(n.Names, n.Values)
			}
			return out == nil
		})
	}
	return out
}

// enclosingName names the function containing the selection, for messages.
func (x *extraction) enclosingName() string {
	path := analysis.PathTo(x.File, x.stmts[0])
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

func (x *extraction) line(p token.Pos) int { return x.TF.Line(p) }

// firstLine quotes the first line of a statement, shortened if long.
func (x *extraction) firstLine(s ast.Stmt) string {
	text := string(x.Src[x.Off(s.Pos()):x.Off(s.End())])
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

// indentUnit is one level of indentation in this file: a tab, as gofmt
// writes, unless the enclosing function's body is indented with spaces
// (the file is not gofmt'd); then the new function uses the same spaces,
// so its body does not mix tabs and spaces.
func (x *extraction) indentUnit(decl ast.Decl) string {
	fd, ok := decl.(*ast.FuncDecl)
	if !ok || fd.Body == nil || len(fd.Body.List) == 0 {
		return "\t"
	}
	u := x.indentOf(fd.Body.List[0].Pos())
	if u == "" || strings.Trim(u, " ") != "" {
		return "\t"
	}
	return u
}

func (x *extraction) indentOf(p token.Pos) string {
	start := x.Off(x.TF.LineStart(x.line(p)))
	ws := x.Src[start:x.Off(p)]
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
