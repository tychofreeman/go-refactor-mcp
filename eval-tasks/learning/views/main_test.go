package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestView(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"go.mod":      "module example.com/m\n\ngo 1.22\n",
		"a/a.go":      "package a\n\n// Double doubles.\nfunc Double(x int) int {\n\ty := x\n\ty += x\n\tif y < 0 {\n\t\ty = 0\n\t}\n\tz := y\n\treturn z * 1\n}\n",
		"a/a_test.go": "package a\n",
	} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	whole, err := view("whole", dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(whole, "== a/a.go") || !strings.Contains(whole, "return z * 1") || strings.Contains(whole, "a_test.go") {
		t.Errorf("whole =\n%s", whole)
	}
	skel, err := view("skeleton", dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(skel, "func Double(x int) int") || strings.Contains(skel, "return z * 1") {
		t.Errorf("skeleton =\n%s", skel)
	}
}
