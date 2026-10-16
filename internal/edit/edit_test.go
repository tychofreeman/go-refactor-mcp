package edit

import (
	"strings"
	"testing"
)

func TestApply(t *testing.T) {
	src := []byte("func foo() { foo() }")
	got, err := Apply(src, []TextEdit{
		{Start: 13, End: 16, NewText: "bar"},
		{Start: 5, End: 8, NewText: "bar"},
		{Start: 5, End: 8, NewText: "bar"}, // duplicate from a test variant
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := "func bar() { bar() }"; string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestApplyOverlap(t *testing.T) {
	_, err := Apply([]byte("abcdef"), []TextEdit{{Start: 0, End: 3, NewText: "x"}, {Start: 2, End: 4, NewText: "y"}})
	if err == nil || !strings.Contains(err.Error(), "overlapping") {
		t.Fatalf("want overlap error, got %v", err)
	}
}

func TestApplyOutOfRange(t *testing.T) {
	if _, err := Apply([]byte("abc"), []TextEdit{{Start: 2, End: 9}}); err == nil {
		t.Fatal("want out-of-range error")
	}
}

func TestUnified(t *testing.T) {
	before := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\n"
	after := "a\nB\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\n"
	got := Unified("x.go", []byte(before), []byte(after))
	want := `--- a/x.go
+++ b/x.go
@@ -1,5 +1,5 @@
 a
-b
+B
 c
 d
 e
@@ -9,3 +9,4 @@
 i
 j
 k
+l
`
	if got != want {
		t.Errorf("diff mismatch:\n--- got\n%s\n--- want\n%s", got, want)
	}
}

func TestUnifiedEqual(t *testing.T) {
	if d := Unified("x", []byte("same\n"), []byte("same\n")); d != "" {
		t.Errorf("want empty diff, got %q", d)
	}
}

func TestUnifiedNoTrailingNewline(t *testing.T) {
	got := Unified("x", []byte("a"), []byte("b"))
	if !strings.Contains(got, `\ No newline at end of file`) {
		t.Errorf("missing no-newline marker:\n%s", got)
	}
}

// TestMyersRoundTrip checks that the edit script reconstructs b from a.
func TestMyersRoundTrip(t *testing.T) {
	cases := [][2]string{
		{"", "x\n"},
		{"x\n", ""},
		{"a\nb\nc\n", "c\nb\na\n"},
		{"a\nb\nc\na\nb\nb\na\n", "c\nb\na\nb\na\nc\n"},
	}
	for _, c := range cases {
		a, b := splitLines(c[0]), splitLines(c[1])
		var rebuilt []string
		for _, o := range myers(a, b) {
			switch o.kind {
			case ' ':
				if a[o.a] != b[o.b] {
					t.Fatalf("%q→%q: keep op on unequal lines", c[0], c[1])
				}
				rebuilt = append(rebuilt, a[o.a])
			case '+':
				rebuilt = append(rebuilt, b[o.b])
			}
		}
		if strings.Join(rebuilt, "") != c[1] {
			t.Errorf("%q→%q: rebuilt %q", c[0], c[1], strings.Join(rebuilt, ""))
		}
	}
}
