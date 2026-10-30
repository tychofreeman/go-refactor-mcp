// Package tstest holds what the TypeScript packages' tests share: a
// solution written to a directory and loaded, a refactoring run on it, and
// its files read back.
package tstest

import (
	"encoding/json"
	"os"
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
