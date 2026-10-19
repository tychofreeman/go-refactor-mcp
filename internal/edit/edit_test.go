package edit

import (
	"slices"
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

func TestMergeKeepsDelete(t *testing.T) {
	got := Merge([]FileEdit{
		{Path: "a.go", Edits: []TextEdit{{Start: 0, End: 1}}},
		{Path: "a.go", Delete: true},
		{Path: "b.go", Edits: []TextEdit{{Start: 0, End: 1}}},
	})
	if len(got) != 2 || !got[0].Delete || got[1].Delete {
		t.Errorf("Merge = %+v, want a.go deleted and b.go not", got)
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

func TestElideSupplied(t *testing.T) {
	for _, tc := range []struct{ name, diff, supplied, want string }{
		{
			name:     "a new file",
			diff:     "--- a/m.go\n+++ b/m.go\n@@ -0,0 +1,3 @@\n+package m\n+\n+var X = 1\n",
			supplied: "package m\n\nvar X = 1\n",
			want:     "--- a/m.go\n+++ b/m.go\n@@ -0,0 +1,3 @@\n[3 lines added as written: lines 1-3]\n",
		},
		{
			name: "added code beside an import the model did not write",
			diff: "--- a/m.go\n+++ b/m.go\n@@ -1,5 +1,12 @@\n package m\n \n+import \"strings\"\n+\n func A() {}\n \n" +
				"+// B upper-cases s.\n+func B(s string) string {\n+\treturn strings.ToUpper(s)\n+}\n+\n var X = 1\n",
			supplied: "// B upper-cases s.\nfunc B(s string) string {\n\treturn strings.ToUpper(s)\n}",
			want: "--- a/m.go\n+++ b/m.go\n@@ -1,5 +1,12 @@\n package m\n \n+import \"strings\"\n+\n func A() {}\n \n" +
				"[5 lines added as written: lines 7-11]\n var X = 1\n",
		},
		{
			name:     "gofmt changed the spacing",
			diff:     "--- a/m.go\n+++ b/m.go\n@@ -2,0 +3,2 @@\n+type T struct {\n+\tx    int\n",
			supplied: "type T struct {\n x int",
			want:     "--- a/m.go\n+++ b/m.go\n@@ -2,0 +3,2 @@\n[2 lines added as written: lines 3-4]\n",
		},
		{
			name:     "removed and changed lines are kept",
			diff:     "--- a/m.go\n+++ b/m.go\n@@ -3,2 +3,2 @@\n-a()\n-b()\n+c()\n+d()\n",
			supplied: "x()\ny()\n",
			want:     "--- a/m.go\n+++ b/m.go\n@@ -3,2 +3,2 @@\n-a()\n-b()\n+c()\n+d()\n",
		},
		{
			name:     "one line is not worth eliding",
			diff:     "--- a/m.go\n+++ b/m.go\n@@ -3 +3,2 @@\n a()\n+b()\n",
			supplied: "b()",
			want:     "--- a/m.go\n+++ b/m.go\n@@ -3 +3,2 @@\n a()\n+b()\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ElideSupplied(tc.diff, tc.supplied); got != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

func TestElideSuppliedNothingSupplied(t *testing.T) {
	diff := "--- a/m.go\n+++ b/m.go\n@@ -1 +1,3 @@\n a()\n+\n+\n"
	if got := ElideSupplied(diff, ""); got != diff {
		t.Errorf("got:\n%s", got)
	}
}

func TestUnifiedContext(t *testing.T) {
	before := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\n"
	after := "a\nb\nc\nd\nE\nf\ng\nh\ni\nJ\n"
	want := "--- a/m.go\n+++ b/m.go\n@@ -5 +5 @@\n-e\n+E\n@@ -10 +10 @@\n-j\n+J\n"
	if got := UnifiedContext("m.go", []byte(before), []byte(after), 0); got != want {
		t.Errorf("no context:\ngot:\n%s\nwant:\n%s", got, want)
	}
	if got, want := UnifiedContext("m.go", []byte(before), []byte(after), 3), Unified("m.go", []byte(before), []byte(after)); got != want {
		t.Errorf("3 lines of context differ from Unified:\n%s\n%s", got, want)
	}
}

func TestLocations(t *testing.T) {
	diff := "--- a/a.go\n+++ b/a.go\n@@ -12 +12 @@\n-x\n+y\n@@ -40,2 +40,2 @@\n-x\n-x\n+y\n+y\n" +
		"--- a/b/c.go\n+++ b/b/c.go\n@@ -3,0 +4 @@\n+new\n@@ -9 +9,0 @@\n-gone\n"
	want := "changed lines:\n  a.go: 12, 40-41\n  b/c.go: 4, after 9\n"
	if got := Locations(diff); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestLineMap(t *testing.T) {
	before := "a\nb\nc\nd\ne\n"
	after := "new\na\nB\nc\ne\n" // a line added before a, b changed, d removed
	got := LineMap([]byte(before), []byte(after))
	want := []int{0, 2, 0, 4, 0, 5}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := LineMap(nil, []byte("x\n")); len(got) != 1 {
		t.Errorf("a new file has no old lines to map: %v", got)
	}
}
