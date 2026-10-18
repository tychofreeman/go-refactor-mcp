// Package textutil holds helpers for refactorings that move Go source text
// around: scanning it into tokens (comments included) and re-indenting it.
package textutil

import (
	"go/scanner"
	"go/token"
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
