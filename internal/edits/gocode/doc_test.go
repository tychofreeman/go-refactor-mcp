package gocode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommentWidth(t *testing.T) {
	line := func(n int) string { return "// " + strings.Repeat("x ", n)[:n-3] + "\n" }
	for _, c := range []struct {
		name string
		src  string
		want int
	}{
		{"the longest line", line(60) + line(72) + line(70) + line(65) + line(50), 72},
		{"too few lines", line(60) + line(72), 76},
		{"over 100 left out", line(60) + line(72) + line(70) + line(65) + line(50) + line(120), 72},
		{"indented left out", line(60) + line(72) + line(70) + line(65) + line(50) + "func f() {\n\t// " + strings.Repeat("y", 90) + "\n}\n", 72},
	} {
		if got := CommentWidth([]byte(c.src)); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

func TestDocComment(t *testing.T) {
	for _, c := range []struct {
		name, doc, want string
	}{
		{"empty", "  \n", ""},
		{"short", "f does it.\nIn two lines.", "// f does it.\n// In two lines.\n"},
		{"long", "f does one thing and then another thing, and finally a third thing.",
			"// f does one thing and then another thing,\n// and finally a third thing.\n"},
		{"commented", "// f does one thing and then another thing, and finally a third thing.",
			"// f does one thing and then another thing,\n// and finally a third thing.\n"},
		{"directive", "f is generated.\n//go:generate stringer -type=Kind", "// f is generated.\n//go:generate stringer -type=Kind\n"},
		{"paragraphs", "f does it.\n\nThen more.", "// f does it.\n//\n// Then more.\n"},
		{"indented", "f runs:\n\tgo run . --with-a-very-long-flag --and-another-very-long-flag", "// f runs:\n//\tgo run . --with-a-very-long-flag --and-another-very-long-flag\n"},
		{"one long word", "see https://example.com/a/very/long/path/that/does/not/fit/anywhere", "// see\n// https://example.com/a/very/long/path/that/does/not/fit/anywhere\n"},
	} {
		if got := DocComment(c.doc, 45); got != c.want {
			t.Errorf("%s: got\n%s\nwant\n%s", c.name, got, c.want)
		}
	}
}

// A GOROOT whose src is a symlink, as Debian's golang packages lay it
// out (/usr/lib/go-1.26/src -> ../../share/go-1.26/src), still lists
// the standard library.
func TestStdPackagesThroughSymlinkedSrc(t *testing.T) {
	share := t.TempDir()
	if err := os.MkdirAll(filepath.Join(share, "src", "strconv"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(share, "src", "strconv", "atoi.go"), []byte("package strconv\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(share, "src"), filepath.Join(root, "src")); err != nil {
		t.Skip("no symlinks:", err)
	}
	if got := stdPackagesIn(root)["strconv"]; len(got) != 1 || got[0] != "strconv" {
		t.Errorf("strconv: got %v, want [strconv]", got)
	}
}
