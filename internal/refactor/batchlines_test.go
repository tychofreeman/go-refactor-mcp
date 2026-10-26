package refactor

import (
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
)

func TestStartLines(t *testing.T) {
	before := []byte("a\nb\nc\nd\n")
	after := []byte("a\nnew1\nnew2\nb\nc\nd\n") // a step wrote two lines after a
	m := newLineMap(before)
	m.follow(1, edit.LineMap(before, after))
	lines := map[string]*lineMap{"m.go": m}
	for _, c := range []struct{ msg, file, want string }{
		{"the code after the switch does (line 6 on)", "m.go", "the code after the switch does (line 4 on)"},
		{"lines 4-5 do not cover whole statements", "m.go", "lines 2-3 do not cover whole statements"},
		{"m.go:5:3: undefined: x", "", "m.go:3:3: undefined: x"},
		{"line 2 is new", "m.go", "line 2* is new\n(* a line an earlier step of this batch wrote, numbered as it is after that step)"},
		{"other.go:5: unchanged", "m.go", "other.go:5: unchanged"},
		{"Statements near there: 4-5 `if b {...`, 6 `d`", "m.go", "Statements near there: 2-3 `if b {...`, 4 `d`"},
		{"line 6 of a file nothing names", "", "line 6 of a file nothing names"},
	} {
		if got := startLines(c.msg, c.file, lines); got != c.want {
			t.Errorf("startLines(%q, %q) =\n%q, want\n%q", c.msg, c.file, got, c.want)
		}
	}
}
