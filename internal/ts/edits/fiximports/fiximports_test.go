package fiximports

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/edits/adddecl"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/edits/createfile"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/edits/replacetext"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

type step = refactor.StepOf[*workspace.Workspace]

func load(t *testing.T, files map[string]string) (*workspace.Workspace, string) {
	t.Helper()
	dir := t.TempDir()
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
	return ws, dir
}

func solution(t *testing.T) (*workspace.Workspace, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("../../workspace/testdata/solution")); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return ws, dir
}

func read(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mk(r refactor.RefactoringOf[*workspace.Workspace], args map[string]string) step {
	raw, _ := json.Marshal(args)
	return step{Refactoring: r, Args: raw}
}

func batch(t *testing.T, ws *workspace.Workspace, steps ...step) *refactor.BatchResult {
	t.Helper()
	b, err := refactor.RunBatch(ws, steps, refactor.OptionsOf[*workspace.Workspace]{Unchecked: true, Imports: New()})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A name another file exports is imported: in a new declaration after the
// file's imports, or into an import of the same module, a type as type.
func TestImports(t *testing.T) {
	ws, dir := solution(t)
	b := batch(t, ws,
		mk(createfile.New(), map[string]string{"file": "src/sun.ts", "content": "export function shine(): number {\n  return 1;\n}"}),
		mk(adddecl.New(), map[string]string{"file": "src/plant.ts", "content": "export const rain = 2;\n\nexport interface Pot {\n  size: number;\n}"}),
		mk(adddecl.New(), map[string]string{"file": "src/bed.ts", "content": "export const noon = shine();\nexport const wet = water(rain);\nexport const pot: Pot = { size: 1 };"}),
	)
	if !b.Applied {
		t.Fatalf("not applied: %s", b)
	}
	want := "import { water, rain, type Pot } from \"./plant\";\nimport { shine } from \"./sun\";\n\nexport const morning"
	if got := read(t, dir, "src/bed.ts"); !strings.HasPrefix(got, want) {
		t.Fatalf("bed.ts:\n%s\nwant it to start:\n%s", got, want)
	}
	if !strings.Contains(b.String(), "fix_imports: imported shine from src/sun.ts, rain from src/plant.ts, Pot from src/plant.ts in src/bed.ts") {
		t.Errorf("the result does not say what was imported:\n%s", b)
	}
}

// A name two files export is not imported, and the batch says so.
func TestAmbiguous(t *testing.T) {
	ws, _ := solution(t)
	b := batch(t, ws,
		mk(createfile.New(), map[string]string{"file": "src/sun.ts", "content": "export const glow = 1;"}),
		mk(createfile.New(), map[string]string{"file": "src/moon.ts", "content": "export const glow = 2;"}),
		mk(adddecl.New(), map[string]string{"file": "src/bed.ts", "content": "export const night = glow;"}),
	)
	if b.Applied || !strings.Contains(b.Refused, "Cannot find name 'glow'") {
		t.Fatalf("a name two files export was imported: %s", b)
	}
}

// With noUnusedLocals, an import an edit leaves unused is removed, in the
// file's quotes and without semicolons.
func TestRemovesUnused(t *testing.T) {
	ws, dir := load(t, map[string]string{
		"tsconfig.json": `{"compilerOptions": {"strict": true, "noUnusedLocals": true, "module": "ESNext", "moduleResolution": "Bundler", "noEmit": true, "types": []}, "include": ["src"]}`,
		"src/a.ts":      "export const a = 1\nexport const b = 2\nexport const d = 3\n",
		"src/c.ts":      "import { a, b } from './a'\n\nexport const c = a + b\n",
	})
	b := batch(t, ws, mk(replacetext.New(), map[string]string{"file": "src/c.ts", "old": "a + b", "new": "a + d"}))
	if want := "import { a, d } from './a'\n\nexport const c = a + d\n"; !b.Applied || read(t, dir, "src/c.ts") != want {
		t.Fatalf("%s\nc.ts:\n%s\nwant:\n%s", b, read(t, dir, "src/c.ts"), want)
	}
}

func TestClears(t *testing.T) {
	for err, want := range map[string]string{
		"src/bed.ts:3:22: Cannot find name 'shine'.":                         "src/bed.ts",
		"src/c.ts:1:13: 'b' is declared but its value is never read.":        "src/c.ts",
		"src/c.ts:1:1: All imports in import declaration are unused.":        "src/c.ts",
		"src/bed.ts:3:22: Type 'string' is not assignable to type 'number'.": "",
		"calc/calc.go:3:2: undefined: strings":                               "",
	} {
		if got, _ := New().Clears(err); got != want {
			t.Errorf("Clears(%q) = %q, want %q", err, got, want)
		}
	}
}
