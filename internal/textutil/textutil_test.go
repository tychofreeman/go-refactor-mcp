package textutil

import (
	"slices"
	"strings"
	"testing"
)

func TestReindent(t *testing.T) {
	in := "\t\tx := 1\n\n\t\ts := `a\n\t\tb`\n\t\t// c\n"
	want := "\tx := 1\n\n\ts := `a\n\t\tb`\n\t// c\n"
	if got := Reindent(in, "\t\t", "\t"); got != want {
		t.Errorf("Reindent:\n got %q\nwant %q", got, want)
	}
}

// Text is found exactly, or else with the spacing within lines ignored
// (as gofmt aligns it), within the range given; a refusal says why.
func TestLocate(t *testing.T) {
	src := "m := map[string]int{\n\t\"a\":    1,\n\t\"bb\":   2,\n}\nn := 1\nn := 1\n"
	for _, c := range []struct {
		old     string
		lo, hi  int
		all     bool
		spans   [][2]int
		loose   bool
		refusal string
	}{
		{old: "\"a\":    1", hi: len(src), spans: [][2]int{{22, 31}}},
		{old: "\"a\": 1,\n    \"bb\": 2,", hi: len(src), spans: [][2]int{{22, 44}}, loose: true},
		{old: "n := 1", hi: len(src), refusal: "occurs 2 times in m.go (lines 5, 6)"},
		{old: "n := 1", hi: len(src), all: true, spans: [][2]int{{47, 53}, {54, 60}}},
		{old: "n := 1", lo: 54, hi: len(src), spans: [][2]int{{54, 60}}},
		{old: "\"a\": 2", hi: len(src), refusal: "was not found in m.go"},
		{old: "x := 2\nn := 1", hi: len(src), refusal: "its line n := 1 is at lines 5, 6"},
	} {
		spans, loose, refusal := Locate(src, c.old, c.lo, c.hi, c.all, "m.go")
		if c.refusal != "" {
			if !strings.Contains(refusal, c.refusal) {
				t.Errorf("%q: refusal %q, want it to contain %q", c.old, refusal, c.refusal)
			}
			continue
		}
		if refusal != "" || loose != c.loose || !slices.Equal(spans, c.spans) {
			t.Errorf("%q: got %v loose %v %q, want %v loose %v", c.old, spans, loose, refusal, c.spans, c.loose)
		}
	}
}

func TestScanAutoSemicolon(t *testing.T) {
	toks := Scan([]byte("a\nb"))
	if len(toks) != 4 || toks[1].Off != toks[1].End {
		t.Errorf("want ident, empty semicolon, ident, semicolon; got %v", toks)
	}
}
