package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
)

// incModule is a small module with an import chain a <- b <- b_test and an
// unrelated package c, so tests can see which packages an edit reaches.
var incModule = map[string]string{
	"a/a.go": `package a

// Rate is the multiplier.
var Rate = 3

func Apply(n int) int { return n * Rate }
`,
	"a/more.go": `package a

func Twice(n int) int { return 2 * Apply(n) }
`,
	"b/b.go": `package b

import "example.com/m/a"

func Run() int { return a.Apply(a.Rate) }
`,
	"b/b_test.go": `package b_test

import (
	"testing"

	"example.com/m/a"
	"example.com/m/b"
)

func TestRun(t *testing.T) {
	if b.Run() != a.Apply(a.Rate) {
		t.Fatal("bad")
	}
}
`,
	"c/c.go": `package c

import "strings"

func Up(s string) string { return strings.ToUpper(s) }
`,
}

// mustMatchFresh fails the test if w differs from a fresh load.
func mustMatchFresh(t *testing.T, w *Workspace) {
	t.Helper()
	fresh, err := Load(w.Root)
	if err != nil {
		t.Fatal(err)
	}
	if diffs := Compare(w, fresh, 15); len(diffs) > 0 {
		t.Fatalf("incremental state differs from a fresh load:\n  %s", strings.Join(diffs, "\n  "))
	}
}

// replace edits path by replacing old with new (once) through Apply.
func replace(t *testing.T, w *Workspace, rel, old, new string) Change {
	t.Helper()
	path := filepath.Join(w.Root, rel)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(src), old)
	if i < 0 {
		t.Fatalf("%q not in %s", old, rel)
	}
	c, err := w.Apply("test", []edit.FileEdit{{Path: path, Edits: []edit.TextEdit{{Start: i, End: i + len(old), NewText: new}}}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func counts(t *testing.T, w *Workspace, full, inc int) {
	t.Helper()
	if w.FullLoads != full || w.IncrementalUpdates != inc {
		t.Errorf("full loads %d, incremental updates %d; want %d, %d", w.FullLoads, w.IncrementalUpdates, full, inc)
	}
}

func TestIncrementalBodyEdit(t *testing.T) {
	w, err := Load(writeModule(t, copyMap(incModule)))
	if err != nil {
		t.Fatal(err)
	}
	replace(t, w, "a/a.go", "return n * Rate", "return Rate * n")
	counts(t, w, 1, 1)
	mustMatchFresh(t, w)
	if _, err := w.Undo(); err != nil {
		t.Fatal(err)
	}
	counts(t, w, 1, 2)
	mustMatchFresh(t, w)
}

// An exported rename touches a and every package that uses it, including a
// test variant; all importers must see the new types.
func TestIncrementalCrossPackageRename(t *testing.T) {
	w, err := Load(writeModule(t, copyMap(incModule)))
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{
		"a/a.go":      strings.ReplaceAll(incModule["a/a.go"], "Rate", "Factor"),
		"b/b.go":      strings.ReplaceAll(incModule["b/b.go"], "a.Rate", "a.Factor"),
		"b/b_test.go": strings.ReplaceAll(incModule["b/b_test.go"], "a.Rate", "a.Factor"),
	}
	var fes []edit.FileEdit
	for rel, after := range paths {
		p := filepath.Join(w.Root, rel)
		before, _ := os.ReadFile(p)
		fes = append(fes, edit.FileEdit{Path: p, Edits: []edit.TextEdit{{Start: 0, End: len(before), NewText: after}}})
	}
	if _, err := w.Apply("rename", fes); err != nil {
		t.Fatal(err)
	}
	counts(t, w, 1, 1)
	if errs := w.Errors(); len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	mustMatchFresh(t, w)
}

// Type errors introduced and fixed incrementally are reported exactly as a
// full load reports them.
func TestIncrementalTypeErrors(t *testing.T) {
	w, err := Load(writeModule(t, copyMap(incModule)))
	if err != nil {
		t.Fatal(err)
	}
	replace(t, w, "a/a.go", "var Rate = 3", `var Rate = "three"`)
	counts(t, w, 1, 1)
	if len(w.Errors()) == 0 {
		t.Fatal("type error not reported")
	}
	mustMatchFresh(t, w)
	if _, err := w.Undo(); err != nil {
		t.Fatal(err)
	}
	if errs := w.Errors(); len(errs) > 0 {
		t.Fatalf("errors remain after fixing: %v", errs)
	}
	mustMatchFresh(t, w)
}

func TestIncrementalParseError(t *testing.T) {
	w, err := Load(writeModule(t, copyMap(incModule)))
	if err != nil {
		t.Fatal(err)
	}
	replace(t, w, "c/c.go", "return strings.ToUpper(s)", "return strings.ToUpper(s")
	counts(t, w, 1, 1)
	if len(w.Errors()) == 0 {
		t.Fatal("parse error not reported")
	}
	mustMatchFresh(t, w)
}

// Edits that can change package metadata fall back to a full reload.
func TestIncrementalFallsBack(t *testing.T) {
	cases := []struct {
		name, file, old, new string
	}{
		{"new import", "c/c.go", `import "strings"`, "import (\n\t\"fmt\"\n\t\"strings\"\n)\n\nvar _ = fmt.Sprint"},
		{"package name", "c/c.go", "package c", "package d"},
		{"build constraint", "c/c.go", "package c", "//go:build ignore\n\npackage c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, err := Load(writeModule(t, copyMap(incModule)))
			if err != nil {
				t.Fatal(err)
			}
			replace(t, w, tc.file, tc.old, tc.new)
			counts(t, w, 2, 0)
			mustMatchFresh(t, w)
		})
	}
}

// Refresh picks up an edit made outside the workspace incrementally, and a
// new file with a full reload.
func TestRefreshIncrementalAndStructural(t *testing.T) {
	w, err := Load(writeModule(t, copyMap(incModule)))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(w.Root, "a", "more.go")
	os.WriteFile(path, []byte(strings.Replace(incModule["a/more.go"], "2 *", "3 *", 1)), 0o644)
	future := time.Now().Add(2 * time.Second)
	os.Chtimes(path, future, future)
	if changed, err := w.Refresh(); err != nil || !changed {
		t.Fatalf("Refresh: changed=%v err=%v", changed, err)
	}
	counts(t, w, 1, 1)
	mustMatchFresh(t, w)

	os.WriteFile(filepath.Join(w.Root, "c", "d.go"), []byte("package c\n\nfunc Down() {}\n"), 0o644)
	if changed, err := w.Refresh(); err != nil || !changed {
		t.Fatalf("Refresh after adding a file: changed=%v err=%v", changed, err)
	}
	counts(t, w, 2, 1)
	mustMatchFresh(t, w)
}

func copyMap(m map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		out[k] = v
	}
	return out
}
