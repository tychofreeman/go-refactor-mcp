// Package tstest holds what the TypeScript packages' tests share: a
// solution written to a directory and loaded, a refactoring run on it, and
// its files read back.
package tstest

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

// Load writes files (by path relative to dir) into dir and loads the
// solution there.
func Load(t testing.TB, dir string, files map[string]string) *workspace.Workspace {
	t.Helper()
	for name, text := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ws, err := workspace.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

// Run runs r on ws with args, a JSON object, and returns its result.
func Run(t testing.TB, ws *workspace.Workspace, r refactor.RefactoringOf[*workspace.Workspace], args string) *refactor.Result {
	t.Helper()
	res, err := refactor.Run(ws, r, json.RawMessage(args), refactor.OptionsOf[*workspace.Workspace]{})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// Read returns the file at rel under dir.
func Read(t testing.TB, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Unchanged fails t when a file under dir, other than those named in
// changed, differs from files, the solution as it was written: changed,
// added or removed. A test checks the files it expects to change itself.
func Unchanged(t testing.TB, dir string, files map[string]string, changed ...string) {
	t.Helper()
	may := map[string]bool{}
	for _, c := range changed {
		may[c] = true
	}
	seen := map[string]bool{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		seen[rel] = true
		if may[rel] {
			return nil
		}
		was, ok := files[rel]
		if !ok {
			t.Errorf("%s was added", rel)
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if string(b) != was {
			t.Errorf("%s changed:\n%s\nwas:\n%s", rel, b, was)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for rel := range files {
		if !seen[rel] && !may[rel] {
			t.Errorf("%s was removed", rel)
		}
	}
}

// RunNode runs src/g.ts of the solution in dir with node, stripping its
// types, and returns what it printed.
func RunNode(t *testing.T, dir string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(dir, "src/g.ts"))
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "g.mts")
	if err := os.WriteFile(f, src, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("node", "--experimental-strip-types", "--no-warnings", f).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	return string(out)
}

// NeedNode skips t in -short mode, or when there is no node to run.
func NeedNode(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("runs node")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("no node")
	}
}

// Probe runs src/g.ts of the solution in dir with node, applies r with
// args to ws, and runs it again: it must print the same.
func Probe(t *testing.T, ws *workspace.Workspace, dir string, r refactor.RefactoringOf[*workspace.Workspace], args string) {
	t.Helper()
	before := RunNode(t, dir)
	res := Run(t, ws, r, args)
	if !res.Applied {
		t.Fatalf("%s", res)
	}
	if after := RunNode(t, dir); after != before {
		t.Errorf("printed\n%s\nbefore, and\n%s\nafter:\n%s", before, after, res)
	}
}
