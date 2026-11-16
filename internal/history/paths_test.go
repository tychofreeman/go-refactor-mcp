package history

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// links makes a root holding a file and links: to a file outside, to a
// directory outside, to a file inside, and to nothing.
func links(t *testing.T) (root, outside string) {
	t.Helper()
	root, outside = t.TempDir(), t.TempDir()
	for _, p := range []string{filepath.Join(root, "in.go"), filepath.Join(outside, "out.go")} {
		if err := os.WriteFile(p, []byte("package m\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for link, target := range map[string]string{
		"file-out": filepath.Join(outside, "out.go"),
		"dir-out":  outside,
		"file-in":  filepath.Join(root, "in.go"),
		"nowhere":  filepath.Join(outside, "missing.go"),
	} {
		if err := os.Symlink(target, filepath.Join(root, link)); err != nil {
			t.Skip("no symlinks:", err)
		}
	}
	return root, outside
}

func TestOutside(t *testing.T) {
	root, _ := links(t)
	for _, c := range []struct {
		rel     string
		outside string // part of the error, "" for none
	}{
		{"in.go", ""},
		{"new/dir/new.go", ""},
		{"file-in", ""},
		{"file-out", "out.go"},
		{"dir-out/out.go", "out.go"},
		{"dir-out/new.go", "new.go"},
		{"nowhere", "points to nothing"},
	} {
		err := Outside(root, filepath.Join(root, c.rel))
		switch {
		case c.outside == "" && err != nil:
			t.Errorf("%s: %v", c.rel, err)
		case c.outside != "" && (err == nil || !strings.Contains(err.Error(), c.outside)):
			t.Errorf("%s: error %v, want one naming %s", c.rel, err, c.outside)
		}
	}
}
