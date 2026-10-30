package tsapi

import (
	"fmt"
	"sort"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/core"
	"github.com/buke/typescript-go-internal/v7/pkg/parser"
	"github.com/buke/typescript-go-internal/v7/pkg/scanner"
)

// Place is where new statements go: the bytes [At, End) of a file are
// replaced with Prefix, the statements indented with Indent, and Suffix.
type Place struct {
	At, End        int
	Indent         string
	Prefix, Suffix string
}

// lined is a file's text with the offsets its lines start at.
type lined struct {
	f      *ast.SourceFile
	text   string
	starts []int
}

func newLined(f *ast.SourceFile) *lined {
	l := &lined{f: f, text: f.Text(), starts: []int{0}}
	for i := 0; i < len(l.text); i++ {
		if l.text[i] == '\n' && i+1 < len(l.text) {
			l.starts = append(l.starts, i+1)
		}
	}
	return l
}

// count is the number of lines.
func (l *lined) count() int { return len(l.starts) }

// line is the 1-based line of offset off.
func (l *lined) line(off int) int {
	return sort.Search(len(l.starts), func(i int) bool { return l.starts[i] > off })
}

// start is the offset line starts at; past the last line, the end of the
// text.
func (l *lined) start(line int) int {
	if line > len(l.starts) {
		return len(l.text)
	}
	return l.starts[line-1]
}

// end is the offset of the end of line, before its newline.
func (l *lined) end(line int) int {
	e := l.start(line + 1)
	if e > l.start(line) && l.text[e-1] == '\n' {
		e--
	}
	return e
}

func (l *lined) indent(line int) string {
	s := l.text[l.start(line):l.end(line)]
	return s[:len(s)-len(strings.TrimLeft(s, " \t"))]
}

// unit is the file's indentation step: a tab if a line starts with one,
// else the fewest spaces a line starts with, else two spaces.
func (l *lined) unit() string {
	least := 0
	for n := 1; n <= l.count(); n++ {
		ind := l.indent(n)
		if strings.HasPrefix(ind, "\t") {
			return "\t"
		}
		if line := strings.TrimSpace(l.text[l.start(n):l.end(n)]); strings.HasPrefix(line, "*") {
			continue // a block comment's continuation
		}
		if len(ind) > 0 && len(ind) < len(l.text[l.start(n):l.end(n)]) && (least == 0 || len(ind) < least) {
			least = len(ind)
		}
	}
	if least == 0 {
		least = 2
	}
	return strings.Repeat(" ", least)
}

// quiet reports whether text holds only whitespace and comments.
func quiet(text string) bool {
	t := strings.TrimSpace(text)
	return t == "" || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "/*") || strings.HasPrefix(t, "*")
}

// pos is where n's first token starts.
func (l *lined) pos(n *ast.Node) int { return scanner.GetTokenPosOfNode(n, l.f, false) }

// lines are the lines n's tokens start and end on.
func (l *lined) lines(n *ast.Node) (int, int) {
	return l.line(l.pos(n)), l.line(max(n.End()-1, l.pos(n)))
}

// lists calls fn with each statement list of the file, outer before inner.
func (l *lined) lists(fn func(owner *ast.Node, list []*ast.Node)) {
	inspect(l.f.AsNode(), func(n *ast.Node) bool {
		if n.CanHaveStatements() {
			fn(n, n.Statements())
		}
		return true
	})
}

func (p *Project) lined(path string) (*lined, string) {
	f := p.sourceFile(path)
	if f == nil {
		return nil, "the file is in no project of the solution"
	}
	return newLined(f), ""
}

// Statements returns the bytes of lines first-last, from the start of the
// first to the start of the line after the last, when they hold whole
// statements of one block and otherwise only comments and blank lines;
// with the indentation of those statements and how many there are. If
// they do not, it returns a refusal, naming the lines of whole statements
// that cover them where there are some.
func (p *Project) Statements(path string, first, last int) (start, end int, indent string, n int, refusal string) {
	l, refusal := p.lined(path)
	if refusal != "" {
		return 0, 0, "", 0, refusal
	}
	chosen, why := l.chosen(first, last)
	if why != "" {
		if strings.Contains(why, "whole statements") {
			why += " (to replace a whole body, give function)"
		}
		return 0, 0, "", 0, why
	}
	return l.start(first), l.start(last + 1), l.indent(l.line(l.pos(chosen[0]))), len(chosen), ""
}

// whole reports whether stmts, of one list, are all that lines first-last
// hold besides comments and blank lines.
func (l *lined) whole(stmts []*ast.Node, first, last int) bool {
	if len(stmts) == 0 {
		return false
	}
	covered := map[int]bool{}
	for _, s := range stmts {
		a, b := l.lines(s)
		for n := a; n <= b; n++ {
			covered[n] = true
		}
	}
	for n := first; n <= last; n++ {
		if !covered[n] && !quiet(l.text[l.start(n):l.end(n)]) {
			return false
		}
	}
	head := l.pos(stmts[0])
	tail := stmts[len(stmts)-1].End()
	return strings.TrimSpace(l.text[l.start(l.line(head)):head]) == "" && quiet(l.text[tail:l.end(l.line(tail-1))])
}

// Body returns where the statements of the body of the function at
// [blockStart, blockEnd), its braces, go: everything between the braces,
// indented as its statements are or one step in from the opening brace's
// line.
func (p *Project) Body(path string, blockStart, blockEnd int) (Place, string) {
	l, refusal := p.lined(path)
	if refusal != "" {
		return Place{}, refusal
	}
	outer := l.indent(l.line(blockStart))
	pl := Place{At: blockStart + 1, End: blockEnd - 1, Indent: outer + l.unit(), Prefix: "\n", Suffix: "\n" + outer}
	l.lists(func(owner *ast.Node, list []*ast.Node) {
		if ast.IsBlock(owner) && l.pos(owner) == blockStart && len(list) > 0 && l.line(l.pos(list[0])) != l.line(blockStart) {
			pl.Indent = l.indent(l.line(l.pos(list[0])))
		}
	})
	return pl, ""
}

// Insertion returns where insert_statements puts statements: "before" the
// statement that starts on line, "after" the one that ends on it (the
// outermost), or "into" the block whose opening brace, or the case clause
// whose colon, is on line.
func (p *Project) Insertion(path string, line int, where string) (Place, string) {
	l, refusal := p.lined(path)
	if refusal != "" {
		return Place{}, refusal
	}
	if line < 1 || line > l.count() {
		return Place{}, fmt.Sprintf("line %d out of range: the file has %d lines", line, l.count())
	}
	var pl *Place
	switch where {
	case "before":
		l.lists(func(_ *ast.Node, list []*ast.Node) {
			for _, s := range list {
				if pl == nil && l.line(l.pos(s)) == line && strings.TrimSpace(l.text[l.start(line):l.pos(s)]) == "" {
					pl = &Place{At: l.start(line), End: l.start(line), Indent: l.indent(line), Suffix: "\n"}
				}
			}
		})
		if pl == nil {
			return Place{}, fmt.Sprintf("no statement starts line %d", line)
		}
	case "after":
		l.lists(func(_ *ast.Node, list []*ast.Node) {
			for _, s := range list {
				if pl == nil && l.line(s.End()-1) == line && quiet(l.text[s.End():l.end(line)]) {
					at := l.start(line + 1)
					pl = &Place{At: at, End: at, Indent: l.indent(l.line(l.pos(s))), Suffix: "\n"}
					if at == len(l.text) && !strings.HasSuffix(l.text, "\n") {
						pl.Prefix, pl.Suffix = "\n", ""
					}
				}
			}
		})
		if pl == nil {
			return Place{}, fmt.Sprintf("no statement ends line %d", line)
		}
	case "into":
		l.lists(func(owner *ast.Node, list []*ast.Node) {
			if pl != nil || ast.IsSourceFile(owner) {
				return
			}
			open := l.pos(owner)
			if !ast.IsBlock(owner) && !ast.IsModuleBlock(owner) {
				from := open + len("default")
				if e := owner.Expression(); e != nil {
					from = e.End()
				}
				open = from + strings.IndexByte(l.text[from:], ':')
			}
			if l.line(open) != line {
				return
			}
			pl = &Place{At: open + 1, End: open + 1, Indent: l.indent(line) + l.unit(), Prefix: "\n"}
			switch {
			case len(list) > 0 && l.line(l.pos(list[0])) == line:
				pl, refusal = nil, fmt.Sprintf("the block on line %d has a statement on that line; use replace_statements", line)
			case len(list) > 0:
				pl.Indent = l.indent(l.line(l.pos(list[0])))
			case ast.IsBlock(owner) && l.line(owner.End()-1) == line:
				pl.End = owner.End() - 1
				pl.Suffix = "\n" + l.indent(line)
			}
		})
		if refusal != "" {
			return Place{}, refusal
		}
		if pl == nil {
			return Place{}, fmt.Sprintf("no block opens on line %d (for a case clause, give the line of its colon)", line)
		}
	default:
		return Place{}, fmt.Sprintf("where must be before, after or into, not %q", where)
	}
	return *pl, ""
}

// ParseStatements parses content as statements of a function's body and
// returns how many there are, and whether a template literal spans lines
// (its text must not be indented again). It refuses content that is not
// statements, or that would close the block it goes in.
func ParseStatements(content string) (n int, raw bool, refusal string) {
	const head = "async function* __content() {\n"
	f := parseText(head + content + "\n}\n")
	if ds := f.Diagnostics(); len(ds) > 0 {
		return 0, false, "content is not TypeScript statements: " + messages(ds)
	}
	if len(f.Statements.Nodes) != 1 {
		return 0, false, "content closes the block it goes in: its braces do not balance"
	}
	body := f.Statements.Nodes[0].Body()
	return len(body.Statements()), multiline(f.AsNode()), ""
}

func parseText(text string) *ast.SourceFile {
	return parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: "/__content.ts", Path: "/__content.ts"}, text, core.ScriptKindTS)
}

// multiline reports whether a template literal under n spans lines.
func multiline(n *ast.Node) bool {
	found := false
	var visit func(n *ast.Node) bool
	visit = func(n *ast.Node) bool {
		switch n.Kind {
		case ast.KindNoSubstitutionTemplateLiteral, ast.KindTemplateExpression:
			sf := ast.GetSourceFileOfNode(n)
			if strings.Contains(sf.Text()[scanner.GetTokenPosOfNode(n, sf, false):n.End()], "\n") {
				found = true
			}
		}
		return found || n.ForEachChild(visit)
	}
	visit(n)
	return found
}
