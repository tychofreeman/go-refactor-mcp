// Package textutil holds helpers for refactorings that move Go source text
// around: scanning it into tokens (comments included) and re-indenting it.
package textutil

import (
	"fmt"
	"go/scanner"
	"go/token"
	"regexp"
	"strconv"
	"strings"
)

// Token is one token of source text, by byte offsets.
type Token struct {
	Off, End int
	Tok      token.Token
}

// Scan returns the tokens of src, including comments. Semicolons that the
// scanner inserts at line ends are empty (Off == End).
func Scan(src []byte) []Token {
	fs := token.NewFileSet()
	f := fs.AddFile("", -1, len(src))
	var s scanner.Scanner
	s.Init(f, src, nil, scanner.ScanComments)
	var out []Token
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
		out = append(out, Token{off, off + n, t})
	}
}

// Reindent returns text (whole lines of statements) with each line's
// leading from replaced by to. Blank lines become empty, and lines that
// start inside a multi-line string literal or comment are left alone,
// since changing them would change the string.
func Reindent(text, from, to string) string {
	var spans [][2]int // multi-line tokens
	for _, t := range Scan([]byte(text)) {
		if (t.Tok == token.STRING || t.Tok == token.COMMENT) && strings.Contains(text[t.Off:t.End], "\n") {
			spans = append(spans, [2]int{t.Off, t.End})
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

// Plural returns "s" unless n is 1, for a message: "%d file%s".
func Plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// FirstFew joins the first n items with commas and says how many more
// there are: "a, b, and 3 more".
func FirstFew(items []string, n int) string {
	s := strings.Join(items[:min(len(items), n)], ", ")
	if len(items) > n {
		s += fmt.Sprintf(", and %d more", len(items)-n)
	}
	return s
}

// Locate returns the byte spans of old in src[lo:hi]: its exact
// occurrences or, when there are none, those that match it with the
// spacing within lines ignored (each run of spaces and tabs standing for
// any other, as gofmt aligns them); loose says which. refusal says why
// none was taken: old was not found, or was found more than once and not
// all were asked for. where names the file in a refusal.
func Locate(src, old string, lo, hi int, all bool, where string) (spans [][2]int, loose bool, refusal string) {
	text := src[lo:hi]
	for i := 0; ; {
		j := strings.Index(text[i:], old)
		if j < 0 {
			break
		}
		spans = append(spans, [2]int{lo + i + j, lo + i + j + len(old)})
		i += j + len(old)
	}
	if len(spans) == 0 {
		for _, m := range spacingIgnored(old).FindAllStringIndex(text, -1) {
			spans = append(spans, [2]int{lo + m[0], lo + m[1]})
		}
		loose = len(spans) > 0
	}
	switch {
	case len(spans) == 0:
		return nil, false, fmt.Sprintf("old was not found in %s; it must match the file, though spacing within lines may differ%s", where, lineHint(src[:hi], old, lo))
	case len(spans) > 1 && !all:
		var lines []string
		for _, s := range spans {
			lines = append(lines, strconv.Itoa(1+strings.Count(src[:s[0]], "\n")))
		}
		return nil, false, fmt.Sprintf("old occurs %d times in %s (lines %s); include more of the surrounding text to pick one, or set all_occurrences", len(spans), where, strings.Join(lines, ", "))
	}
	return spans, loose, ""
}

// spacingIgnored returns a pattern matching text with each run of spaces
// and tabs in it standing for any other, and line breaks kept.
func spacingIgnored(text string) *regexp.Regexp {
	var sb strings.Builder
	for i := 0; i < len(text); {
		if c := text[i]; c != ' ' && c != '\t' && c != '\n' {
			sb.WriteString(regexp.QuoteMeta(text[i : i+1]))
			i++
			continue
		}
		j := i
		for j < len(text) && strings.IndexByte(" \t\n", text[j]) >= 0 {
			j++
		}
		if n := strings.Count(text[i:j], "\n"); n > 0 {
			sb.WriteString(strings.Repeat(`[ \t]*\n`, n) + `[ \t]*`)
		} else {
			sb.WriteString(`[ \t]+`)
		}
		i = j
	}
	return regexp.MustCompile(sb.String())
}

// lineHint names, for a refusal, the lines of src from offset lo where the
// first line of old that src has is, with spacing ignored; "" if none.
func lineHint(src, old string, lo int) string {
	start := 1 + strings.Count(src[:lo], "\n")
	srcLines := strings.Split(src[lo:], "\n")
	for _, ol := range strings.Split(old, "\n") {
		want := strings.Join(strings.Fields(ol), " ")
		if want == "" {
			continue
		}
		var at []string
		for i, sl := range srcLines {
			if strings.Contains(strings.Join(strings.Fields(sl), " "), want) {
				at = append(at, strconv.Itoa(start+i))
			}
		}
		if len(at) > 0 {
			return fmt.Sprintf("; its line %s is at line%s %s", want, Plural(len(at)), strings.Join(at[:min(len(at), 5)], ", "))
		}
	}
	return ""
}

// Numbered returns lines start through end (1-based) of a file split into
// lines, each after its number in a column as wide as end's.
func Numbered(lines []string, start, end int) string {
	var sb strings.Builder
	width := len(fmt.Sprint(end))
	for l := start; l <= end; l++ {
		sb.WriteString(NumberedLine(width, l, lines[l-1]))
	}
	return sb.String()
}

// NumberedLine is line l of a file after its number, in a column width
// wide.
func NumberedLine(width, l int, line string) string {
	return fmt.Sprintf("%*d  %s\n", width, l, strings.TrimRight(line, "\n"))
}

// NextLine returns the offset just past the newline that ends off's line.
func NextLine(src []byte, off int) int {
	if i := strings.IndexByte(string(src[off:]), '\n'); i >= 0 {
		return off + i + 1
	}
	return len(src)
}
