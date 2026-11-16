package history

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, p, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newChange() Change {
	return Change{Before: map[string][]byte{}, After: map[string][]byte{}, Created: map[string]bool{}, Deleted: map[string]bool{}}
}

// Deleting the last file of a module removes the directories it empties
// up to the root, and not the root or what is above it.
func TestWriteRemovesEmptyDirectoriesUpToTheRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "mod")
	p := filepath.Join(root, "a", "b", "m.go")
	write(t, p, "package b\n")
	c := newChange()
	c.Before[p], c.After[p], c.Deleted[p] = []byte("package b\n"), nil, true
	if err := c.Write(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "a")); !os.IsNotExist(err) {
		t.Errorf("a was not removed: %v", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Errorf("the root was removed: %v", err)
	}
}

// A change that cannot write one of its files changes none of them.
func TestWriteFailingChangesNothing(t *testing.T) {
	root := t.TempDir()
	kept, gone, blocked := filepath.Join(root, "a.go"), filepath.Join(root, "d.go"), filepath.Join(root, "f", "x.go")
	write(t, kept, "old\n")
	write(t, gone, "deleted\n")
	write(t, filepath.Join(root, "f"), "a file where a directory should be\n")
	c := newChange()
	c.Before[kept], c.After[kept] = []byte("old\n"), []byte("new\n")
	c.Before[gone], c.After[gone], c.Deleted[gone] = []byte("deleted\n"), nil, true
	c.Before[blocked], c.After[blocked] = nil, []byte("x\n")
	if err := c.Write(root); err == nil {
		t.Fatal("Write succeeded")
	}
	if b, _ := os.ReadFile(kept); string(b) != "old\n" {
		t.Errorf("a.go = %q, want it unchanged", b)
	}
	if _, err := os.Stat(gone); err != nil {
		t.Errorf("d.go was deleted: %v", err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 3 {
		t.Errorf("the root holds %d entries, want the 3 it had", len(entries))
	}
}

// A change to a link writes the file it points at, and the link stays.
func TestWriteThroughALink(t *testing.T) {
	root := t.TempDir()
	target, link := filepath.Join(root, "real.go"), filepath.Join(root, "link.go")
	write(t, target, "old\n")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("no symlinks:", err)
	}
	c := newChange()
	c.Before[link], c.After[link] = []byte("old\n"), []byte("new\n")
	if err := c.Write(root); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(target); string(b) != "new\n" {
		t.Errorf("real.go = %q, want %q", b, "new\n")
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("link.go is no longer a link: %v", err)
	}
}
