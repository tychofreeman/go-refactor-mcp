package inlinevar

// Opt-in round-trip test: for many expressions of a real module, apply
// extract_variable and then inline_variable on the new variable. The two
// are inverses, so the file must come back unchanged (after gofmt), and
// every step must compile (and, with STRESS_TESTS=1, pass the package's
// tests). Same environment variables as TestStress.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cwfreeman/gorefactor/internal/edit"
	"github.com/cwfreeman/gorefactor/internal/refactor"
	"github.com/cwfreeman/gorefactor/internal/refactorings/extractvar"
	"github.com/cwfreeman/gorefactor/internal/workspace"
)

func TestRoundTrip(t *testing.T) {
	dir := os.Getenv("STRESS_DIR")
	if dir == "" {
		t.Skip("set STRESS_DIR")
	}
	if w := os.Getenv("STRESS_WORK"); w != "" {
		t.Setenv("GOWORK", w)
	}
	ws, err := workspace.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	type cand struct {
		file      string
		line, col int
		text      string
	}
	var cands []cand
	seen := map[string]bool{}
	for _, p := range ws.Pkgs {
		for _, f := range p.Syntax {
			name := ws.Fset.File(f.Pos()).Name()
			if seen[name] || !ws.Contains(name) || strings.HasSuffix(name, "_test.go") {
				continue
			}
			seen[name] = true
			src, _ := os.ReadFile(name)
			tf := ws.Fset.File(f.Pos())
			ast.Inspect(f, func(n ast.Node) bool {
				e, ok := n.(ast.Expr)
				if !ok {
					return true
				}
				text := string(src[tf.Offset(e.Pos()):tf.Offset(e.End())])
				if strings.Contains(text, "\n") || len(text) > 80 {
					return true
				}
				pos := tf.Position(e.Pos())
				cands = append(cands, cand{ws.Rel(name), pos.Line, pos.Column, text})
				return true
			})
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		return fmt.Sprintf("%s:%06d:%04d", a.file, a.line, a.col) < fmt.Sprintf("%s:%06d:%04d", b.file, b.line, b.col)
	})
	rand.New(rand.NewSource(int64(envInt("STRESS_SEED", 1)))).Shuffle(len(cands), func(i, j int) { cands[i], cands[j] = cands[j], cands[i] })
	n := min(envInt("STRESS_N", 400), len(cands))

	extracted, back, identical := 0, 0, 0
	refusedBack := map[string]int{}
	var bugs, diffs []string
	runTests := func(file string) string {
		if os.Getenv("STRESS_TESTS") == "" {
			return ""
		}
		cmd := exec.Command("go", "test", "-count=1", "./"+filepath.Dir(file))
		cmd.Dir = ws.Root
		if out, err := cmd.CombinedOutput(); err != nil {
			return tailLines(string(out), 15)
		}
		return ""
	}
	for i, c := range cands[:n] {
		abs := ws.Abs(c.file)
		orig, _ := os.ReadFile(abs)
		name := fmt.Sprintf("zzRound%d", i)
		where := fmt.Sprintf("%s:%d:%d %q", c.file, c.line, c.col, c.text)
		eargs, _ := json.Marshal(map[string]any{"file": c.file, "line": c.line, "column": c.col, "expression": c.text, "name": name})
		res, err := refactor.Run(ws, extractvar.New(), eargs, refactor.Options{})
		if err != nil || !res.Applied {
			continue
		}
		extracted++
		// The declaration is on the line of the original statement start;
		// find it by name.
		line := findLine(ws, abs, name)
		iargs, _ := json.Marshal(map[string]any{"file": c.file, "line": line, "name": name})
		res2, err := refactor.Run(ws, New(), iargs, refactor.Options{})
		switch {
		case err != nil:
			bugs = append(bugs, fmt.Sprintf("ERROR inlining back %s: %v", where, err))
		case strings.Contains(res2.Refused, "rolled back"):
			bugs = append(bugs, fmt.Sprintf("ROLLBACK inlining back %s:\n%s", where, res2.Refused))
		case !res2.Applied:
			r := res2.Refused
			if j := strings.Index(r, " at "); j > 0 && j < 80 {
				r = r[:j]
			}
			refusedBack[r]++
			if os.Getenv("STRESS_VERBOSE") != "" {
				t.Logf("NOT INLINED %s: %s", where, res2.Refused)
			}
		default:
			back++
			if out := runTests(c.file); out != "" {
				bugs = append(bugs, fmt.Sprintf("TESTS FAIL after round trip %s:\n%s", where, out))
			}
			now, _ := os.ReadFile(abs)
			fo, _ := format.Source(orig)
			fn, _ := format.Source(now)
			if bytes.Equal(fo, fn) {
				identical++
			} else if len(diffs) < 15 {
				diffs = append(diffs, fmt.Sprintf("%s:\n%s", where, edit.Unified(c.file, fo, fn)))
			}
			if _, err := ws.Undo(); err != nil {
				t.Fatalf("undo inline: %v", err)
			}
		}
		if _, err := ws.Undo(); err != nil {
			t.Fatalf("undo extract: %v", err)
		}
		if now, _ := os.ReadFile(abs); !bytes.Equal(now, orig) {
			t.Fatalf("undo did not restore %s", c.file)
		}
	}
	t.Logf("tried %d expressions: extracted %d, inlined back %d, identical after round trip %d", n, extracted, back, identical)
	for r, k := range refusedBack {
		t.Logf("  %4d not inlined back: %s", k, r)
	}
	for _, d := range diffs {
		t.Logf("DIFFERENT %s", d)
	}
	for _, b := range bugs {
		t.Errorf("%s", b)
	}
}

func findLine(ws *workspace.Workspace, path, name string) int {
	_, f := ws.FileSyntax(path)
	line := 0
	ast.Inspect(f, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == name && line == 0 {
			line = ws.Fset.Position(id.Pos()).Line
		}
		return line == 0
	})
	return line
}
