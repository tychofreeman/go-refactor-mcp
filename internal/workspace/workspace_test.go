package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
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

func TestApplyCreatesFileAndUndoRemovesIt(t *testing.T) {
	dir := writeModule(t, map[string]string{"m.go": "package m\n\nfunc F() int { return G() }\n\nfunc G() int { return 1 }\n"})
	w, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := filepath.Join(dir, "m.go")
	src, _ := os.ReadFile(m)
	i := strings.Index(string(src), "\nfunc G")
	g := filepath.Join(dir, "g.go")
	c, err := w.Apply("move G", []edit.FileEdit{
		{Path: m, Edits: []edit.TextEdit{{Start: i, End: len(src)}}},
		{Path: g, Edits: []edit.TextEdit{{NewText: "package m\nfunc G() int { return 1 }\n"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(g); string(got) != "package m\n\nfunc G() int { return 1 }\n" {
		t.Errorf("new file not written (and gofmt-ed): %q", got)
	}
	if !strings.Contains(c.Diff, "+package m") {
		t.Errorf("diff does not show the new file:\n%s", c.Diff)
	}
	if errs := w.Errors(); len(errs) != 0 {
		t.Fatalf("errors after creating the file: %v", errs)
	}
	if _, err := w.Undo(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(g); !os.IsNotExist(err) {
		t.Errorf("undo did not remove the new file: %v", err)
	}
	if got, _ := os.ReadFile(m); string(got) != string(src) {
		t.Errorf("undo did not restore m.go: %s", got)
	}
	if errs := w.Errors(); len(errs) != 0 {
		t.Fatalf("errors after undo: %v", errs)
	}
}

func TestApplyDeletesFileAndUndoRestoresIt(t *testing.T) {
	gSrc := "package m\n\nfunc G() int { return 1 }\n"
	dir := writeModule(t, map[string]string{
		"m.go": "package m\n\nfunc F() int { return 1 }\n",
		"g.go": gSrc,
	})
	w, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	g := filepath.Join(dir, "g.go")
	c, err := w.Apply("delete g.go", []edit.FileEdit{{Path: g, Delete: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(g); !os.IsNotExist(err) {
		t.Errorf("g.go was not deleted: %v", err)
	}
	if !strings.Contains(c.Diff, "-func G() int { return 1 }") {
		t.Errorf("diff does not show the deleted file:\n%s", c.Diff)
	}
	if errs := w.Errors(); len(errs) != 0 {
		t.Fatalf("errors after deleting the file: %v", errs)
	}
	if fresh, err := Load(dir); err != nil {
		t.Fatal(err)
	} else if diffs := Compare(w, fresh, 5); len(diffs) > 0 {
		t.Errorf("after deleting, the workspace differs from a fresh load:\n  %s", strings.Join(diffs, "\n  "))
	}
	if _, err := w.Undo(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(g); string(got) != gSrc {
		t.Errorf("undo did not restore g.go: %q", got)
	}
	if fresh, err := Load(dir); err != nil {
		t.Fatal(err)
	} else if diffs := Compare(w, fresh, 5); len(diffs) > 0 {
		t.Errorf("after undoing, the workspace differs from a fresh load:\n  %s", strings.Join(diffs, "\n  "))
	}
}

func TestApplyCreatesDirectoriesAndUndoRemovesThem(t *testing.T) {
	dir := writeModule(t, map[string]string{"m.go": "package m\n\nfunc F() int { return 1 }\n"})
	w, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	g := filepath.Join(dir, "sub", "deeper", "g.go")
	if _, err := w.Apply("new package", []edit.FileEdit{
		{Path: g, Edits: []edit.TextEdit{{NewText: "package deeper\n\nfunc G() int { return 1 }\n"}}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(g); err != nil {
		t.Fatalf("new file in new directories not written: %v", err)
	}
	if errs := w.Errors(); len(errs) != 0 {
		t.Fatalf("errors after creating the package: %v", errs)
	}
	found := false
	for _, p := range w.Pkgs {
		found = found || p.Name == "deeper"
	}
	if !found {
		t.Error("the new package was not loaded")
	}
	if _, err := w.Undo(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sub")); !os.IsNotExist(err) {
		t.Errorf("undo did not remove the directories it created: %v", err)
	}
}

// A directory a change created that has since gained another file stays,
// with that file, and the rest of the change is still undone.
func TestUndoKeepsACreatedDirectoryWithOtherFiles(t *testing.T) {
	src := "package m\n\nfunc F() int { return 1 }\n"
	dir := writeModule(t, map[string]string{"m.go": src})
	w, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := filepath.Join(dir, "m.go")
	g := filepath.Join(dir, "sub", "g.go")
	i := strings.Index(src, "1")
	if _, err := w.Apply("new file and an edit", []edit.FileEdit{
		{Path: g, Edits: []edit.TextEdit{{NewText: "package sub\n\nfunc G() int { return 1 }\n"}}},
		{Path: m, Edits: []edit.TextEdit{{Start: i, End: i + 1, NewText: "2"}}},
	}); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(dir, "sub", "notes.txt")
	if err := os.WriteFile(stray, []byte("not ours\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Undo(); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if got, _ := os.ReadFile(m); string(got) != src {
		t.Errorf("m.go not restored:\n%s", got)
	}
	if _, err := os.Stat(g); !os.IsNotExist(err) {
		t.Errorf("the created file is still there: %v", err)
	}
	if _, err := os.Stat(stray); err != nil {
		t.Errorf("the other file in the directory was removed: %v", err)
	}
	if w.CanUndo() {
		t.Error("the undone change is still on the stack")
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

// An edit after which the module no longer loads (a broken go.mod) is put
// back, kept off the undo stack and reported as ErrBreaksLoading, and the
// workspace goes on working.
func TestApplyThatBreaksLoadingIsRestored(t *testing.T) {
	src := "package m\n\nvar X = 1\n"
	dir := writeModule(t, map[string]string{"m.go": src})
	w, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	gomod := filepath.Join(dir, "go.mod")
	before, _ := os.ReadFile(gomod)
	i := strings.Index(string(before), "module")
	_, err = w.Apply("break go.mod", []edit.FileEdit{{Path: gomod, Edits: []edit.TextEdit{{Start: i, End: i + len("module"), NewText: "modul"}}}})
	if !errors.Is(err, ErrBreaksLoading) {
		t.Fatalf("want ErrBreaksLoading, got %v", err)
	}
	if got, _ := os.ReadFile(gomod); string(got) != string(before) {
		t.Errorf("go.mod not restored:\n%s", got)
	}
	if w.CanUndo() {
		t.Error("the change that was put back is on the undo stack")
	}
	path := filepath.Join(dir, "m.go")
	j := strings.Index(src, "1")
	if _, err := w.Apply("x", []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{Start: j, End: j + 1, NewText: "2"}}}}); err != nil {
		t.Fatalf("the workspace no longer works: %v", err)
	}
	if errs := w.Errors(); len(errs) != 0 {
		t.Errorf("errors after the next edit: %v", errs)
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

func TestRefresh(t *testing.T) {
	dir := writeModule(t, map[string]string{"m.go": "package m\n\nvar X = 1\n"})
	w, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded, err := w.Refresh(); err != nil || reloaded {
		t.Fatalf("Refresh with no changes: reloaded=%v err=%v", reloaded, err)
	}

	// An external edit (e.g. the model editing a file directly) must be
	// picked up, even if it keeps the size and lands in the same mtime tick.
	path := filepath.Join(dir, "m.go")
	os.WriteFile(path, []byte("package m\n\nvar Y = 2\n"), 0o644)
	future := time.Now().Add(2 * time.Second)
	os.Chtimes(path, future, future)
	if reloaded, err := w.Refresh(); err != nil || !reloaded {
		t.Fatalf("Refresh after edit: reloaded=%v err=%v", reloaded, err)
	}
	_, f := w.FileSyntax(path)
	if f == nil || f.Scope.Lookup("Y") == nil {
		t.Error("reloaded syntax does not contain the edit")
	}

	// A new file is also a change.
	os.WriteFile(filepath.Join(dir, "n.go"), []byte("package m\n"), 0o644)
	if reloaded, _ := w.Refresh(); !reloaded {
		t.Error("adding a file did not trigger a reload")
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

func TestSquash(t *testing.T) {
	src := "package m\n\nfunc F() int { return 1 }\n"
	dir := writeModule(t, map[string]string{"m.go": src})
	w, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "m.go")
	change := func(from, to string) {
		t.Helper()
		cur, _ := os.ReadFile(path)
		i := strings.Index(string(cur), from)
		if _, err := w.Apply(from+" to "+to, []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{Start: i, End: i + len(from), NewText: to}}}}); err != nil {
			t.Fatal(err)
		}
	}
	change("return 1", "return 2")
	change("func F", "func G")
	created := filepath.Join(dir, "sub", "n.go")
	if _, err := w.Apply("create", []edit.FileEdit{{Path: created, Edits: []edit.TextEdit{{NewText: "package sub\n"}}}}); err != nil {
		t.Fatal(err)
	}

	c, err := w.Squash(3, "batch")
	if err != nil {
		t.Fatal(err)
	}
	if c.Label != "batch" || !strings.Contains(c.Diff, "-func F() int { return 1 }\n+func G() int { return 2 }") || !strings.Contains(c.Diff, "+package sub") {
		t.Errorf("squashed change %q, diff:\n%s", c.Label, c.Diff)
	}
	if _, err := w.Undo(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != src {
		t.Errorf("one undo did not restore the file: %s", got)
	}
	if _, err := os.Stat(filepath.Dir(created)); !os.IsNotExist(err) {
		t.Errorf("one undo did not remove the created file and directory: %v", err)
	}
	if w.CanUndo() {
		t.Error("squashed changes left on the undo stack")
	}
	if _, err := w.Squash(1, "nothing"); err == nil {
		t.Error("squashing more changes than the stack holds should fail")
	}
}

// The test main package go generates for a package's tests is not the
// module's code: renaming or removing a test must not make it an error.
func TestRenamingATestIsNotAnError(t *testing.T) {
	test := "package m\n\nimport \"testing\"\n\nfunc TestOld(t *testing.T) {}\n"
	dir := writeModule(t, map[string]string{"m.go": "package m\n", "m_test.go": test})
	w, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range w.Pkgs {
		if strings.HasSuffix(p.ID, ".test") {
			t.Errorf("generated test main package %s loaded", p.ID)
		}
	}
	path := filepath.Join(dir, "m_test.go")
	i := strings.Index(test, "TestOld")
	if _, err := w.Apply("rename test", []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{Start: i, End: i + len("TestOld"), NewText: "TestNew"}}}}); err != nil {
		t.Fatal(err)
	}
	if errs := w.Errors(); len(errs) != 0 {
		t.Errorf("renaming a test gave errors: %v", errs)
	}
}
