package workspace

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// withFile returns a copy of files with name set to src.
func withFile(files map[string]string, name, src string) map[string]string {
	out := copyMap(files)
	out[name] = src
	return out
}

// The metadata of a load, applied to the same files, gives the same
// workspace as the load, without another one.
func TestMetadataLoadMatchesLoad(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"chain":    incModule,
		"variants": variantModule,
		// go list reports the error on the missing package, which the
		// importer only sees through the type checker.
		"missing import": withFile(incModule, "c/d.go", "package c\n\nimport \"example.com/m/nope\"\n\nvar _ = nope.X\n"),
	} {
		t.Run(name, func(t *testing.T) {
			w, err := Load(writeModule(t, copyMap(files)))
			if err != nil {
				t.Fatal(err)
			}
			m, ok := w.Metadata()
			if !ok {
				t.Fatal("no metadata")
			}
			fresh, err := m.Load()
			if err != nil {
				t.Fatal(err)
			}
			if fresh.FullLoads != 0 {
				t.Errorf("full loads = %d, want 0", fresh.FullLoads)
			}
			if diffs := Compare(w, fresh, 15); len(diffs) > 0 {
				t.Errorf("differs from the load:\n  %s", strings.Join(diffs, "\n  "))
			}
			if !slices.Equal(w.Errors(), fresh.Errors()) {
				t.Errorf("errors = %v, want %v", fresh.Errors(), w.Errors())
			}
		})
	}
}

// It reads the files on disk, not the syntax of the workspace the
// metadata came from.
func TestMetadataLoadReadsTheFiles(t *testing.T) {
	w, err := Load(writeModule(t, copyMap(variantModule)))
	if err != nil {
		t.Fatal(err)
	}
	m, ok := w.Metadata()
	if !ok {
		t.Fatal("no metadata")
	}
	src := strings.Replace(variantModule["a/a.go"], "return T{N: n}", "m := n\n\treturn T{N: m}", 1)
	if err := os.WriteFile(filepath.Join(w.Root, "a/a.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	fresh, err := m.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(Compare(w, fresh, 1)) == 0 {
		t.Error("the load matches the workspace from before the edit")
	}
	mustMatchFresh(t, fresh)
}

// Files that no longer parse or type-check, under the same headers, give
// the errors a load would.
func TestMetadataLoadOfBrokenFiles(t *testing.T) {
	for name, src := range map[string]string{
		"type error":  "package a\n\nfunc Twice(n int) int { return \"s\" }\n",
		"parse error": "package a\n\nfunc Twice(n int) int { return 2 * }\n",
	} {
		t.Run(name, func(t *testing.T) {
			w, err := Load(writeModule(t, copyMap(incModule)))
			if err != nil {
				t.Fatal(err)
			}
			m, ok := w.Metadata()
			if !ok {
				t.Fatal("no metadata")
			}
			if err := os.WriteFile(filepath.Join(w.Root, "a/more.go"), []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
			fresh, err := m.Load()
			if err != nil {
				t.Fatal(err)
			}
			if len(fresh.Errors()) == 0 {
				t.Fatal("no errors")
			}
			mustMatchFresh(t, fresh)
		})
	}
}

// go list compiles none of the module's packages, so a type error is the
// type checker's alone, not a loader error: such a load gives metadata,
// and edits of the package are checked incrementally.
func TestMetadataDespiteTypeErrors(t *testing.T) {
	w, err := Load(writeModule(t, withFile(incModule, "c/bad.go", "package c\n\nvar X int = \"s\"\n")))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := w.Metadata(); !ok {
		t.Error("no metadata for a package with a type error")
	}
}

// The fingerprint changes with what go list reads, and only with that.
func TestFingerprint(t *testing.T) {
	base, err := Fingerprint(writeModule(t, copyMap(incModule)))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, file, src string
		same            bool
	}{
		{"a body", "a/more.go", "package a\n\nfunc Twice(n int) int { return Apply(n) * 2 }\n", true},
		{"a file in testdata", "a/testdata/x.go", "package x\n", true},
		{"an import", "a/more.go", "package a\n\nimport \"strings\"\n\nvar _ = strings.ToUpper\n\nfunc Twice(n int) int { return 2 * Apply(n) }\n", false},
		{"the package clause", "a/more.go", "package b\n\nfunc Twice(n int) int { return 2 * Apply(n) }\n", false},
		{"a build constraint", "a/more.go", "//go:build linux\n\npackage a\n\nfunc Twice(n int) int { return 2 * Apply(n) }\n", false},
		{"a new file", "a/new.go", "package a\n", false},
		{"go.sum", "go.sum", "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := Fingerprint(writeModule(t, withFile(incModule, c.file, c.src)))
			if err != nil {
				t.Fatal(err)
			}
			if (got == base) != c.same {
				t.Errorf("same fingerprint = %v, want %v", got == base, c.same)
			}
		})
	}
}
