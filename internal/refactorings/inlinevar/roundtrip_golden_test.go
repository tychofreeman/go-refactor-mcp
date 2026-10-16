package inlinevar

import (
	"bytes"
	"encoding/json"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactorings/extractvar"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// TestGoldenRoundTrip checks that inline_variable undoes extract_variable:
// for every extract_variable golden case that applies, it extracts the
// expression, inlines the new variable back, and requires the result to
// match the original input after gofmt. Unlike undo, which restores saved
// file contents, this checks that the two refactorings are inverses: the
// inlined expression gets the parentheses, conversions and placement it
// had before extraction.
func TestGoldenRoundTrip(t *testing.T) {
	os.Setenv("GOWORK", "off") // see refactortest.Run
	dir := filepath.Join("..", "extractvar", "testdata")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	ran := 0
	for _, e := range entries {
		caseDir := filepath.Join(dir, e.Name())
		if _, err := os.Stat(filepath.Join(caseDir, "out")); err != nil {
			continue // a refusal case: nothing to round-trip
		}
		ran++
		t.Run(e.Name(), func(t *testing.T) {
			t.Parallel()
			roundTrip(t, caseDir)
		})
	}
	if ran == 0 {
		t.Fatalf("no applying extract_variable cases in %s", dir)
	}
}

func roundTrip(t *testing.T, caseDir string) {
	raw, err := os.ReadFile(filepath.Join(caseDir, "args.json"))
	if err != nil {
		t.Fatal(err)
	}
	var args struct {
		File string `json:"file"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		t.Fatal(err)
	}
	inDir := filepath.Join(caseDir, "in")
	tmp := t.TempDir()
	copyDir(t, inDir, tmp)

	ws, err := workspace.Load(tmp)
	if err != nil {
		t.Fatal(err)
	}
	res, err := refactor.Run(ws, extractvar.New(), raw, refactor.Options{})
	if err != nil || !res.Applied {
		t.Fatalf("extract_variable did not apply: err=%v\n%s", err, res)
	}

	line := declarationLine(t, ws, args.File, args.Name)
	iargs, _ := json.Marshal(map[string]any{"file": args.File, "line": line, "name": args.Name})
	res, err = refactor.Run(ws, New(), iargs, refactor.Options{})
	if err != nil || !res.Applied {
		t.Fatalf("inline_variable did not apply: err=%v\n%s", err, res)
	}

	// Every file must match the original input, up to gofmt.
	want, got := readGo(t, inDir), readGo(t, tmp)
	for name, w := range want {
		if g := got[name]; !bytes.Equal(w, g) {
			t.Errorf("%s does not round-trip (-original +after extract and inline):\n%s", name, edit.Unified(name, w, g))
		}
	}
}

// declarationLine returns the line of the declaration of name in file.
func declarationLine(t *testing.T, ws *workspace.Workspace, file, name string) int {
	t.Helper()
	path := ws.Abs(file)
	for _, p := range ws.Pkgs {
		for id, obj := range p.TypesInfo.Defs {
			if obj == nil || id.Name != name {
				continue
			}
			if pos := ws.Fset.Position(id.Pos()); pos.Filename == path {
				return pos.Line
			}
		}
	}
	t.Fatalf("no declaration of %q in %s after extraction", name, file)
	return 0
}

// readGo returns the gofmt-formatted contents of the .go files under root.
func readGo(t *testing.T, root string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") {
			return err
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if formatted, err := format.Source(src); err == nil {
			src = formatted
		}
		rel, _ := filepath.Rel(root, p)
		out[filepath.ToSlash(rel)] = src
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func copyDir(t *testing.T, src, dst string) {
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
