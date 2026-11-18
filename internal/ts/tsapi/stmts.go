package tsapi

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/checker"
	"github.com/buke/typescript-go-internal/v7/pkg/core"
	"github.com/buke/typescript-go-internal/v7/pkg/parser"
	"github.com/buke/typescript-go-internal/v7/pkg/scanner"
	"github.com/tychofreeman/go-refactor-mcp/internal/textutil"
)

// Place is where new statements go: the bytes [At, End) of a file are
// replaced with Prefix, the statements indented with Indent, and Suffix.
type Place struct {
	At, End        int
	Indent         string
	Prefix, Suffix string
}

// Lined is a file's text with the offsets its lines start at.
type Lined struct {
	File   *ast.SourceFile
	Text   string
	starts []int
}

func newLined(f *ast.SourceFile) *Lined {
	l := &Lined{File: f, Text: f.Text(), starts: []int{0}}
	for i := 0; i < len(l.Text); i++ {
		if l.Text[i] == '\n' && i+1 < len(l.Text) {
			l.starts = append(l.starts, i+1)
		}
	}
	return l
}

// Count is the number of lines.
func (l *Lined) Count() int { return len(l.starts) }

// Line is the 1-based line of offset off.
func (l *Lined) Line(off int) int {
	return sort.Search(len(l.starts), func(i int) bool { return l.starts[i] > off })
}

// Start is the offset line starts at; past the last line, the end of the
// text.
func (l *Lined) Start(line int) int {
	if line > len(l.starts) {
		return len(l.Text)
	}
	return l.starts[line-1]
}

// End is the offset of the end of line, before its newline.
func (l *Lined) End(line int) int {
	e := l.Start(line + 1)
	if e > l.Start(line) && l.Text[e-1] == '\n' {
		e--
	}
	return e
}

func (l *Lined) Indent(line int) string {
	return textutil.Indent(l.Text[l.Start(line):l.End(line)])
}

// Unit is the file's indentation step: a tab if a line starts with one,
// else the fewest spaces a line starts with, else two spaces.
func (l *Lined) Unit() string {
	least := 0
	for n := 1; n <= l.Count(); n++ {
		ind := l.Indent(n)
		if strings.HasPrefix(ind, "\t") {
			return "\t"
		}
		if line := strings.TrimSpace(l.Text[l.Start(n):l.End(n)]); strings.HasPrefix(line, "*") {
			continue // a block comment's continuation
		}
		if len(ind) > 0 && len(ind) < len(l.Text[l.Start(n):l.End(n)]) && (least == 0 || len(ind) < least) {
			least = len(ind)
		}
	}
	if least == 0 {
		least = 2
	}
	return strings.Repeat(" ", least)
}

// indentable returns the lines from first to last that a change of
// indentation may touch: not blank, and not starting inside a template
// literal, whose text the change would alter.
func (l *Lined) indentable(first, last int) []int {
	var spans [][2]int
	Inspect(l.File.AsNode(), func(n *ast.Node) bool {
		if n.Kind == ast.KindNoSubstitutionTemplateLiteral || n.Kind == ast.KindTemplateExpression {
			spans = append(spans, [2]int{l.Pos(n), n.End()})
		}
		return true
	})
	var lines []int
	for n := first; n <= last; n++ {
		s := l.Start(n)
		inside := slices.ContainsFunc(spans, func(sp [2]int) bool { return sp[0] < s && s < sp[1] })
		if !inside && strings.TrimSpace(l.Text[s:l.End(n)]) != "" {
			lines = append(lines, n)
		}
	}
	return lines
}

// quiet reports whether text holds only whitespace and comments.
func quiet(text string) bool {
	t := strings.TrimSpace(text)
	return t == "" || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "/*") || strings.HasPrefix(t, "*")
}

// Pos is where n's first token starts.
func (l *Lined) Pos(n *ast.Node) int { return scanner.GetTokenPosOfNode(n, l.File, false) }

// Lines are the lines n's tokens start and end on.
func (l *Lined) Lines(n *ast.Node) (int, int) {
	return l.Line(l.Pos(n)), l.Line(max(n.End()-1, l.Pos(n)))
}

// lists calls fn with each statement list of the file, outer before inner.
func (l *Lined) lists(fn func(owner *ast.Node, list []*ast.Node)) {
	Inspect(l.File.AsNode(), func(n *ast.Node) bool {
		if n.CanHaveStatements() {
			fn(n, n.Statements())
		}
		return true
	})
}

func (p *Project) Lined(path string) (*Lined, string) {
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
	l, refusal := p.Lined(path)
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
	return l.Start(first), l.Start(last + 1), l.Indent(l.Line(l.Pos(chosen[0]))), len(chosen), ""
}

// whole reports whether stmts, of one list, are all that lines first-last
// hold besides comments and blank lines.
func (l *Lined) whole(stmts []*ast.Node, first, last int) bool {
	if len(stmts) == 0 {
		return false
	}
	covered := map[int]bool{}
	for _, s := range stmts {
		a, b := l.Lines(s)
		for n := a; n <= b; n++ {
			covered[n] = true
		}
	}
	for n := first; n <= last; n++ {
		if !covered[n] && !quiet(l.Text[l.Start(n):l.End(n)]) {
			return false
		}
	}
	head := l.Pos(stmts[0])
	tail := stmts[len(stmts)-1].End()
	return strings.TrimSpace(l.Text[l.Start(l.Line(head)):head]) == "" && quiet(l.Text[tail:l.End(l.Line(tail-1))])
}

// Body returns where the statements of the body of the function at
// [blockStart, blockEnd), its braces, go: everything between the braces,
// indented as its statements are or one step in from the opening brace's
// line.
func (p *Project) Body(path string, blockStart, blockEnd int) (Place, string) {
	l, refusal := p.Lined(path)
	if refusal != "" {
		return Place{}, refusal
	}
	outer := l.Indent(l.Line(blockStart))
	pl := Place{At: blockStart + 1, End: blockEnd - 1, Indent: outer + l.Unit(), Prefix: "\n", Suffix: "\n" + outer}
	l.lists(func(owner *ast.Node, list []*ast.Node) {
		if ast.IsBlock(owner) && l.Pos(owner) == blockStart && len(list) > 0 && l.Line(l.Pos(list[0])) != l.Line(blockStart) {
			pl.Indent = l.Indent(l.Line(l.Pos(list[0])))
		}
	})
	return pl, ""
}

// Insertion returns where insert_statements puts statements: "before" the
// statement that starts on line, "after" the one that ends on it (the
// outermost), or "into" the block whose opening brace, or the case clause
// whose colon, is on line.
func (p *Project) Insertion(path string, line int, where string) (Place, string) {
	l, refusal := p.Lined(path)
	if refusal != "" {
		return Place{}, refusal
	}
	if line < 1 || line > l.Count() {
		return Place{}, fmt.Sprintf("line %d out of range: the file has %d lines", line, l.Count())
	}
	var pl *Place
	switch where {
	case "before":
		l.lists(func(_ *ast.Node, list []*ast.Node) {
			for _, s := range list {
				if pl == nil && l.Line(l.Pos(s)) == line && strings.TrimSpace(l.Text[l.Start(line):l.Pos(s)]) == "" {
					pl = &Place{At: l.Start(line), End: l.Start(line), Indent: l.Indent(line), Suffix: "\n"}
				}
			}
		})
		if pl == nil {
			return Place{}, fmt.Sprintf("no statement starts line %d", line)
		}
	case "after":
		l.lists(func(_ *ast.Node, list []*ast.Node) {
			for _, s := range list {
				if pl == nil && l.Line(s.End()-1) == line && quiet(l.Text[s.End():l.End(line)]) {
					at := l.Start(line + 1)
					pl = &Place{At: at, End: at, Indent: l.Indent(l.Line(l.Pos(s))), Suffix: "\n"}
					if at == len(l.Text) && !strings.HasSuffix(l.Text, "\n") {
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
			open := l.Pos(owner)
			if !ast.IsBlock(owner) && !ast.IsModuleBlock(owner) {
				from := open + len("default")
				if e := owner.Expression(); e != nil {
					from = e.End()
				}
				open = from + strings.IndexByte(l.Text[from:], ':')
			}
			if l.Line(open) != line {
				return
			}
			pl = &Place{At: open + 1, End: open + 1, Indent: l.Indent(line) + l.Unit(), Prefix: "\n"}
			switch {
			case len(list) > 0 && l.Line(l.Pos(list[0])) == line:
				pl, refusal = nil, fmt.Sprintf("the block on line %d has a statement on that line; use replace_statements", line)
			case len(list) > 0:
				pl.Indent = l.Indent(l.Line(l.Pos(list[0])))
			case ast.IsBlock(owner) && l.Line(owner.End()-1) == line:
				pl.End = owner.End() - 1
				pl.Suffix = "\n" + l.Indent(line)
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

// Checker returns the type checker for f, and the function to call when
// done with it.
func (p *Project) Checker(ctx context.Context, f *ast.SourceFile) (*checker.Checker, func()) {
	return p.prog.GetProgram().GetTypeCheckerForFile(ctx, f)
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
