package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cwfreeman/gorefactor/internal/edit"
)

func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()
	t.Setenv("GOWORK", "off")
	dir := t.TempDir()
	files["go.mod"] = "module example.com/m\n\ngo 1.22\n"
	for name, src := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestApplyAndUndo(t *testing.T) {
	src := "package m\n\nfunc F() int { return 1 }\n"
	dir := writeModule(t, map[string]string{"m.go": src})
	w, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if errs := w.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	path := filepath.Join(dir, "m.go")
	i := strings.Index(src, "1")
	c, err := w.Apply("change literal", []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{Start: i, End: i + 1, NewText: "2"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.Diff, "+func F() int { return 2 }") {
		t.Errorf("unexpected diff:\n%s", c.Diff)
	}
	if got, _ := os.ReadFile(path); !strings.Contains(string(got), "return 2") {
		t.Errorf("file not updated: %s", got)
	}
	if _, err := w.Undo(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != src {
		t.Errorf("undo did not restore file: %s", got)
	}
	if _, err := w.Undo(); err == nil {
		t.Error("second undo should fail")
	}
}

func TestUndoRefusesAfterExternalEdit(t *testing.T) {
	src := "package m\n\nvar X = 1\n"
	dir := writeModule(t, map[string]string{"m.go": src})
	w, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "m.go")
	i := strings.Index(src, "1")
	if _, err := w.Apply("x", []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{Start: i, End: i + 1, NewText: "2"}}}}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte("package m\n\nvar X = 3\n"), 0o644)
	if _, err := w.Undo(); err == nil || !strings.Contains(err.Error(), "modified since") {
		t.Errorf("want modified-since error, got %v", err)
	}
}

func TestLoadFromSubdirUsesModuleRoot(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"a/a.go": "package a\n\nvar X = 1\n",
		"b/b.go": "package b\n\nimport \"example.com/m/a\"\n\nvar Y = a.X\n",
	})
	w, err := Load(filepath.Join(dir, "a"))
	if err != nil {
		t.Fatal(err)
	}
	if w.Root != dir {
		t.Errorf("Root = %s, want module root %s", w.Root, dir)
	}
	if _, f := w.FileSyntax(filepath.Join(dir, "b", "b.go")); f == nil {
		t.Error("sibling package b was not loaded")
	}
	if got, want := w.Abs("a.go"), filepath.Join(dir, "a", "a.go"); got != want {
		t.Errorf("Abs(a.go) = %s, want %s (relative to the given dir)", got, want)
	}
	if got, want := w.Abs("b/b.go"), filepath.Join(dir, "b", "b.go"); got != want {
		t.Errorf("Abs(b/b.go) = %s, want %s (fallback to module root)", got, want)
	}
}

func TestRefusesOutsideRoot(t *testing.T) {
	dir := writeModule(t, map[string]string{"m.go": "package m\n"})
	w, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.Preview("x", []edit.FileEdit{{Path: "/etc/hosts", Edits: []edit.TextEdit{{Start: 0, End: 0, NewText: "x"}}}})
	if err == nil || !strings.Contains(err.Error(), "outside workspace") {
		t.Errorf("want outside-workspace error, got %v", err)
	}
}

func TestErrorsReported(t *testing.T) {
	dir := writeModule(t, map[string]string{"m.go": "package m\n\nvar X int = \"s\"\n"})
	w, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	errs := w.Errors()
	if len(errs) == 0 || !strings.HasPrefix(errs[0], "m.go:3") {
		t.Errorf("want root-relative type error, got %v", errs)
	}
}
