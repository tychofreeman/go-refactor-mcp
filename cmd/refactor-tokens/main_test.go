package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// A session that entered a worktree is logged with its transcript's
// path under the worktree's project directory too, where none exists:
// the transcript to read is the one of that name that does.
func TestToRead(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "-home-u-m", "s1.jsonl")
	moved := filepath.Join(dir, "-home-u-m--claude-worktrees-w", "s1.jsonl")
	gone := filepath.Join(dir, "-home-u-m", "s2.jsonl")
	if err := os.MkdirAll(filepath.Dir(main), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(main, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got := toRead([]string{moved, main, gone})
	if want := []string{main, gone}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
