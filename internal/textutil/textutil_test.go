package textutil

import "testing"

func TestReindent(t *testing.T) {
	in := "\t\tx := 1\n\n\t\ts := `a\n\t\tb`\n\t\t// c\n"
	want := "\tx := 1\n\n\ts := `a\n\t\tb`\n\t// c\n"
	if got := Reindent(in, "\t\t", "\t"); got != want {
		t.Errorf("Reindent:\n got %q\nwant %q", got, want)
	}
}

func TestScanAutoSemicolon(t *testing.T) {
	toks := Scan([]byte("a\nb"))
	if len(toks) != 4 || toks[1].Off != toks[1].End {
		t.Errorf("want ident, empty semicolon, ident, semicolon; got %v", toks)
	}
}
