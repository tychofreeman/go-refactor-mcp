// Package refactortest runs golden-file tests for refactorings.
//
// Layout of a test case directory:
//
//	testdata/<case>/
//	  args.json     arguments passed to the refactoring
//	  in/           a complete Go module (with go.mod) before the change
//	  out/          the expected module after the change, or
//	  refused.txt   a substring expected in the refusal message
//	  notes.txt     optional, when applied: substrings expected in the
//	                result's notes, one per line
//	  no_notes.txt  optional, when applied: substrings the notes must not
//	                contain, one per line
//
// Run `go test ./... -update` to (re)write out/ directories from the
// current behavior; review the diff before committing.
package refactortest

import (
	"bytes"
	"encoding/json"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

var update = flag.Bool("update", false, "rewrite golden out/ directories")

// Run executes every case under dir against r, in parallel.
//
// After every applied refactoring, and again after undoing it, the
// incrementally updated workspace is compared with a fresh load of the
// same files: any difference means the incremental update is wrong. The
// comparison is skipped with -short.
func Run(t *testing.T, r refactor.Refactoring, dir string) {
	t.Helper()
	// Test modules are self-contained; don't let a developer's go.work leak
	// in. Set for the whole process because the cases run in parallel, and
	// t.Setenv cannot be combined with t.Parallel.
	os.Setenv("GOWORK", "off")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	ran := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ran++
		caseDir := filepath.Join(dir, e.Name())
		t.Run(e.Name(), func(t *testing.T) {
			t.Parallel()
			runCase(t, r, caseDir)
		})
	}
	if ran == 0 {
		t.Fatalf("no test cases in %s", dir)
	}
}

func runCase(t *testing.T, r refactor.Refactoring, caseDir string) {
	args, err := os.ReadFile(filepath.Join(caseDir, "args.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(args) {
		t.Fatalf("args.json is not valid JSON")
	}
	inDir := filepath.Join(caseDir, "in")
	tmp := t.TempDir()
	copyTree(t, inDir, tmp)

	ws, err := workspace.Load(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if errs := ws.Errors(); len(errs) > 0 {
		t.Fatalf("input does not type-check: %v", errs)
	}

	res, err := refactor.Run(ws, r, args, refactor.Options{})
	if err != nil {
		t.Fatalf("internal error: %v", err)
	}
	t.Logf("result:\n%s", res)

	wantRefusal, err := os.ReadFile(filepath.Join(caseDir, "refused.txt"))
	if err == nil {
		want := strings.TrimSpace(string(wantRefusal))
		if res.Refused == "" {
			t.Fatalf("expected refusal containing %q, but it was applied:\n%s", want, res.Diff)
		}
		if !strings.Contains(res.Refused, want) {
			t.Errorf("refusal mismatch\n got: %s\nwant substring: %s", res.Refused, want)
		}
		compareTrees(t, inDir, tmp) // nothing may have been written
		return
	}

	if res.Refused != "" {
		t.Fatalf("unexpected refusal: %s", res.Refused)
	}
	if want, err := os.ReadFile(filepath.Join(caseDir, "notes.txt")); err == nil {
		got := strings.Join(res.Notes, "\n")
		for _, line := range strings.Split(strings.TrimSpace(string(want)), "\n") {
			if !strings.Contains(got, line) {
				t.Errorf("no note contains %q; notes:\n%s", line, got)
			}
		}
	}
	if absent, err := os.ReadFile(filepath.Join(caseDir, "no_notes.txt")); err == nil {
		got := strings.Join(res.Notes, "\n")
		for _, line := range strings.Split(strings.TrimSpace(string(absent)), "\n") {
			if strings.Contains(got, line) {
				t.Errorf("a note contains %q, which it should not; notes:\n%s", line, got)
			}
		}
	}
	outDir := filepath.Join(caseDir, "out")
	if *update {
		os.RemoveAll(outDir)
		copyTree(t, tmp, outDir)
		return
	}
	compareTrees(t, outDir, tmp)
	matchesFreshLoad(t, ws, "after applying")

	// Every applied step must be undoable back to the exact input.
	if _, err := ws.Undo(); err != nil {
		t.Fatalf("undo: %v", err)
	}
	compareTrees(t, inDir, tmp)
	matchesFreshLoad(t, ws, "after undoing")
}

// matchesFreshLoad fails the test if the (incrementally updated) workspace
// differs from a fresh load of the same files. It is skipped with -short,
// since each comparison costs a full load.
func matchesFreshLoad(t *testing.T, ws *workspace.Workspace, when string) {
	t.Helper()
	if testing.Short() {
		return
	}
	fresh, err := workspace.Load(ws.Root)
	if err != nil {
		t.Fatal(err)
	}
	if diffs := workspace.Compare(ws, fresh, 15); len(diffs) > 0 {
		t.Errorf("%s, the workspace differs from a fresh load (full loads %d, incremental updates %d):\n  %s",
			when, ws.FullLoads, ws.IncrementalUpdates, strings.Join(diffs, "\n  "))
	}
}

func readTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		files[filepath.ToSlash(rel)] = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func compareTrees(t *testing.T, wantDir, gotDir string) {
	t.Helper()
	want, got := readTree(t, wantDir), readTree(t, gotDir)
	names := map[string]bool{}
	for n := range want {
		names[n] = true
	}
	for n := range got {
		names[n] = true
	}
	var sorted []string
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)
	for _, n := range sorted {
		w, wok := want[n]
		g, gok := got[n]
		switch {
		case !wok:
			t.Errorf("unexpected file %s", n)
		case !gok:
			t.Errorf("missing file %s", n)
		case !bytes.Equal(w, g):
			t.Errorf("%s differs from %s (-want +got):\n%s", n, wantDir, edit.Unified(n, w, g))
		}
	}
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}
