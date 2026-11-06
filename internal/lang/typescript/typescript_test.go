package typescript

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/history"
)

// A Go tool's name runs the TypeScript tool that does its operation.
func TestGoNames(t *testing.T) {
	for goName, want := range goNames {
		r, ok := lookup(goName)
		if !ok || r.Name() != want {
			t.Errorf("lookup(%q) = %v, %v; want %s", goName, r, ok, want)
		}
	}
	if _, ok := lookup("no_such_tool"); ok {
		t.Errorf("lookup(no_such_tool) found a tool")
	}
}

// The tests are package.json's test script when it has one, else
// node --test; a pattern other than ./... names the files.
func TestTestCommand(t *testing.T) {
	plain, scripted := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(scripted, "package.json"), []byte(`{"scripts": {"test": "node --test"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		dir, pattern string
		want         []string
	}{
		{plain, "./...", []string{"node", "--test"}},
		{plain, "", []string{"node", "--test"}},
		{plain, "src/a.test.ts", []string{"node", "--test", "src/a.test.ts"}},
		{scripted, "./...", []string{"npm", "test", "--silent"}},
		{scripted, "src/a.test.ts", []string{"npm", "test", "--silent", "--", "src/a.test.ts"}},
	} {
		if got := testCommand(c.dir, c.pattern); !slices.Equal(got, c.want) {
			t.Errorf("testCommand(%s, %q) = %q; want %q", filepath.Base(c.dir), c.pattern, got, c.want)
		}
	}
}

// A batch's checkpoint and check run the project's tests, not go test.
func TestCheckpointRunsTheProjectsTests(t *testing.T) {
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm is not installed")
	}
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("tsconfig.json", `{"compilerOptions": {"strict": true, "types": []}, "include": ["a.ts"]}`)
	write("a.ts", "export const a = 1;\n")
	write("package.json", `{"scripts": {"test": "node -e \"process.exit(require('fs').existsSync('fail') ? 1 : 0)\""}}`)
	l := New(dir, &history.Stack{})
	batch := json.RawMessage(`{"steps": [{"tool": "check", "args": {"run_tests": true}}]}`)
	if out, err := l.Batch(batch); err != nil || !strings.Contains(out, "tests pass") {
		t.Fatalf("passing tests: %v\n%s", err, out)
	}
	if out, err := l.Check(context.Background(), json.RawMessage(`{"run_tests": true}`)); err != nil || !strings.Contains(out, "TESTS PASSED") {
		t.Fatalf("check, passing tests: %v\n%s", err, out)
	}
	write("fail", "")
	if out, err := l.Batch(batch); err != nil || !strings.Contains(out, "tests of ./... failed") {
		t.Fatalf("failing tests: %v\n%s", err, out)
	}
	if out, err := l.Check(context.Background(), json.RawMessage(`{"run_tests": true}`)); err != nil || !strings.Contains(out, "TESTS FAILED") {
		t.Fatalf("check, failing tests: %v\n%s", err, out)
	}
}

// A call that names a file of another solution, beside the directory's or
// inside it but in no project of its own, works in that solution, and undo
// reverts it; a call that names files of two solutions is refused.
func TestOtherSolutions(t *testing.T) {
	root := t.TempDir()
	config := `{"compilerOptions": {"strict": true, "noEmit": true, "types": []}, "include": ["*.ts", "src"]}`
	for name, text := range map[string]string{
		"home/tsconfig.json":       `{"compilerOptions": {"strict": true, "noEmit": true, "types": []}, "include": ["src"]}`,
		"home/src/a.ts":            "export function a(): number {\n  return 1;\n}\n",
		"home/tools/tsconfig.json": config,
		"home/tools/t.ts":          "export function t(): number {\n  return 2;\n}\n",
		"other/tsconfig.json":      config,
		"other/b.ts":               "export function b(): number {\n  return 3;\n}\n",
	} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	l := New(filepath.Join(root, "home"), &history.Stack{})

	text, refused, err := l.Step("replace_text", []byte(`{"file": "../other/b.ts", "old": "return 3", "new": "return 4"}`))
	if err != nil || refused {
		t.Fatalf("step in a sibling solution: %v %v\n%s", err, refused, text)
	}
	if !strings.HasPrefix(text, "In the solution at ../other") {
		t.Errorf("result does not say which solution:\n%s", text)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "other/b.ts")); !strings.Contains(string(b), "return 4") {
		t.Errorf("other/b.ts:\n%s", b)
	}

	out, err := l.Inspect([]byte(`{"file": "tools/t.ts", "function": "t"}`))
	if err != nil || !strings.Contains(out, "return 2") {
		t.Errorf("inspect in a nested solution: %v\n%s", err, out)
	}

	out, err = l.Batch([]byte(`{"steps": [{"tool": "replace_text", "args": {"file": "src/a.ts", "old": "return 1", "new": "return 5"}}, {"tool": "replace_text", "args": {"file": "tools/t.ts", "old": "return 2", "new": "return 6"}}]}`))
	if err != nil || !strings.Contains(out, "more than one solution") {
		t.Errorf("batch across solutions: %v\n%s", err, out)
	}

	if out, err := l.Undo(nil); err != nil || !strings.Contains(out, "UNDONE replace_text") || !strings.HasPrefix(out, "In the solution at ../other") {
		t.Fatalf("undo: %v\n%s", err, out)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "other/b.ts")); !strings.Contains(string(b), "return 3") {
		t.Errorf("after undo, other/b.ts:\n%s", b)
	}
}

// A call from a worktree carries the worktree, as cmd/worktree-paths
// adds it: a call that names no file works in its solution, one that
// names a file ignores it, and undo refuses a change made elsewhere.
func TestWorktreeCalls(t *testing.T) {
	root := t.TempDir()
	config := `{"compilerOptions": {"strict": true, "noEmit": true, "types": []}, "include": ["src"]}`
	for name, text := range map[string]string{
		"tsconfig.json":                     config,
		"src/a.ts":                          "export function a(): number {\n  return 1;\n}\n",
		".claude/worktrees/w/tsconfig.json": config,
		".claude/worktrees/w/src/a.ts":      "export function a(): number {\n  return 1;\n}\n",
	} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tree := filepath.Join(root, ".claude/worktrees/w")
	l := New(root, &history.Stack{})

	out, err := l.Inspect([]byte(`{"worktree": "` + tree + `"}`))
	if err != nil || !strings.HasPrefix(out, "In the solution at .claude/worktrees/w") {
		t.Errorf("inspect from the worktree: %v\n%s", err, out)
	}
	out, err = l.Check(context.Background(), []byte(`{"worktree": "`+tree+`"}`))
	if err != nil || !strings.HasPrefix(out, "In the solution at .claude/worktrees/w") {
		t.Errorf("check from the worktree: %v\n%s", err, out)
	}
	text, refused, err := l.Step("replace_text", []byte(`{"file": "src/a.ts", "old": "return 1", "new": "return 3"}`))
	if err != nil || refused {
		t.Fatalf("step in the home solution: %v %v\n%s", err, refused, text)
	}
	out, err = l.Undo([]byte(`{"worktree": "` + tree + `"}`))
	if err != nil || !strings.Contains(out, "not in this worktree") {
		t.Errorf("undo from the worktree of a change in the home solution: %v\n%s", err, out)
	}
	// A relative path with the worktree is the worktree's file, as when the
	// server takes the worktree from the client's roots.
	text, refused, err = l.Step("replace_text", []byte(`{"file": "src/a.ts", "old": "return 1", "new": "return 4", "worktree": "`+tree+`"}`))
	if err != nil || refused {
		t.Fatalf("step by a relative path from the worktree: %v %v\n%s", err, refused, text)
	}
	if b, _ := os.ReadFile(filepath.Join(tree, "src/a.ts")); !strings.Contains(string(b), "return 4") {
		t.Errorf("the worktree's src/a.ts:\n%s", b)
	}
}

// move is published in place of move_declaration, move_method and
// move_file, and runs the one its arguments ask for.
func TestMove(t *testing.T) {
	root := t.TempDir()
	for name, text := range map[string]string{
		"tsconfig.json":  `{"compilerOptions": {"strict": true, "noEmit": true, "types": []}, "include": ["src"]}`,
		"src/garden.ts":  "export class Soil {\n  ph = 7;\n  acid(): boolean {\n    return this.ph < 6;\n  }\n}\n\nexport class Bed extends Soil {}\n\nexport const maxBeds = 4;\n\nexport function sour(b: Bed): boolean {\n  return b.acid();\n}\n",
		"src/count.ts":   "import { maxBeds } from \"./garden\";\n\nexport const beds = maxBeds;\n",
		"notes/beds.txt": "beds\n",
	} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	l := New(root, &history.Stack{})
	for _, args := range []string{
		`{"file": "src/garden.ts", "name": "maxBeds", "to_file": "src/limits.ts"}`,
		`{"file": "src/garden.ts", "name": "Soil.acid", "to": "Bed"}`,
		`{"file": "notes/beds.txt", "to": "docs/beds.txt"}`,
	} {
		if text, refused, err := l.Step("move", []byte(args)); err != nil || refused {
			t.Fatalf("move %s: %v %v\n%s", args, err, refused, text)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(root, "src/count.ts")); !strings.Contains(string(b), "./limits") {
		t.Errorf("count.ts does not import from limits:\n%s", b)
	}
	if _, err := os.Stat(filepath.Join(root, "docs/beds.txt")); err != nil {
		t.Errorf("beds.txt was not moved: %v", err)
	}
	if text, refused, _ := l.Step("move", []byte(`{"file": "src/garden.ts", "name": "sour"}`)); !refused {
		t.Errorf("move without a destination is not refused:\n%s", text)
	}
	var names []string
	for _, tool := range l.Tools() {
		names = append(names, tool.Name)
	}
	if !slices.Contains(names, "move") || slices.Contains(names, "move_declaration") || slices.Contains(names, "move_method") || slices.Contains(names, "move_file") {
		t.Errorf("published: %v", names)
	}
}

// change_type is published in place of change_field_type and
// change_parameter_type, and runs the one its arguments ask for.
func TestChangeTypeCovers(t *testing.T) {
	root := t.TempDir()
	for name, text := range map[string]string{
		"tsconfig.json": `{"compilerOptions": {"strict": true, "noEmit": true, "types": []}, "include": ["src"]}`,
		"src/a.ts":      "export type Height = number;\n\nexport interface Plant {\n  height: number;\n}\n\nexport function grow(n: number): number {\n  return n + 1;\n}\n",
	} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	l := New(root, &history.Stack{})
	for _, args := range []string{
		`{"file": "src/a.ts", "name": "Plant.height", "type": "Height"}`,
		`{"file": "src/a.ts", "name": "grow", "parameter": "n", "type": "Height"}`,
	} {
		if text, refused, err := l.Step("change_type", []byte(args)); err != nil || refused {
			t.Fatalf("change_type %s: %v %v\n%s", args, err, refused, text)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(root, "src/a.ts")); !strings.Contains(string(b), "height: Height") || !strings.Contains(string(b), "n: Height") {
		t.Errorf("a.ts:\n%s", b)
	}
	var names []string
	for _, tool := range l.Tools() {
		names = append(names, tool.Name)
	}
	if !slices.Contains(names, "change_type") || slices.Contains(names, "change_field_type") || slices.Contains(names, "change_parameter_type") {
		t.Errorf("published: %v", names)
	}
}

// create_file with declaration writes a class from its header, with a
// member for each that its interfaces and abstract base require, and the
// imports they need.
func TestCreateFromDeclaration(t *testing.T) {
	root := t.TempDir()
	for name, text := range map[string]string{
		"tsconfig.json": `{"compilerOptions": {"strict": true, "noEmit": true, "types": []}, "include": ["src"]}`,
		"src/garden.ts": "export interface Plant {\n  name: string;\n}\n\nexport interface Planter {\n  plant(p: Plant): void;\n  readonly size: number;\n  label?: string;\n}\n\nexport abstract class Soil {\n  depth = 1;\n  abstract ph(): number;\n}\n",
	} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	l := New(root, &history.Stack{})
	if text, refused, err := l.Step("create_file", []byte(`{"file": "src/bed.ts", "declaration": "export class Bed extends Soil implements Planter"}`)); err != nil || refused {
		t.Fatalf("create_file: %v %v\n%s", err, refused, text)
	}
	b, _ := os.ReadFile(filepath.Join(root, "src/bed.ts"))
	for _, want := range []string{"\"./garden\"", "export class Bed extends Soil implements Planter {", "ph(): number {", "plant(p: Plant): void {", "throw new Error(\"not implemented\");", "readonly size!: number;"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("bed.ts lacks %q:\n%s", want, b)
		}
	}
	if strings.Contains(string(b), "label") || strings.Contains(string(b), "depth") {
		t.Errorf("bed.ts has an optional or concrete member:\n%s", b)
	}
	for _, args := range []string{`{"file": "src/pot.ts", "declaration": "export class Pot implements Missing"}`, `{"file": "src/pot.ts", "declaration": "export class Pot {}"}`, `{"file": "src/pot.ts", "declaration": "export class Pot", "content": "x"}`} {
		if text, refused, _ := l.Step("create_file", []byte(args)); !refused {
			t.Errorf("%s: want a refusal, got\n%s", args, text)
		}
	}
}

// test_for writes a red test of a method or function, importing test from
// the framework package.json names (vitest here), once per file.
func TestTestFor(t *testing.T) {
	root := t.TempDir()
	for name, text := range map[string]string{
		"tsconfig.json":                    `{"compilerOptions": {"strict": true, "noEmit": true, "module": "ESNext", "moduleResolution": "Bundler", "types": []}, "include": ["src"]}`,
		"package.json":                     `{"devDependencies": {"vitest": "^3.0.0"}}`,
		"node_modules/vitest/package.json": `{"name": "vitest", "types": "index.d.ts"}`,
		"node_modules/vitest/index.d.ts":   "export declare function test(name: string, fn: () => void | Promise<void>): void;\n",
		"src/garden.ts":                    "export class Bed {\n  constructor(public soil: string, public depth: number) {}\n  plant(name: string, count: number): number {\n    return count;\n  }\n}\n\nexport async function water(liters: number): Promise<string> {\n  return `${liters} L`;\n}\n",
	} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	l := New(root, &history.Stack{})
	if text, refused, err := l.Step("create_file", []byte(`{"file": "src/garden.test.ts", "test_for": "Bed.plant"}`)); err != nil || refused {
		t.Fatalf("create_file: %v %v\n%s", err, refused, text)
	}
	if text, refused, err := l.Step("add_declaration", []byte(`{"file": "src/garden.test.ts", "test_for": "water"}`)); err != nil || refused {
		t.Fatalf("add_declaration: %v %v\n%s", err, refused, text)
	}
	b, _ := os.ReadFile(filepath.Join(root, "src/garden.test.ts"))
	got := string(b)
	for _, want := range []string{"from \"vitest\"", "test(\"Bed.plant\", () => {", "const bed = new Bed(\"\", 0);", "const plant = bed.plant(\"\", 0);", "write the assertion: plant returned", "test(\"water\", async () => {", "const result = await water(0);"} {
		if !strings.Contains(got, want) {
			t.Errorf("garden.test.ts lacks %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "from \"vitest\"") != 1 {
		t.Errorf("vitest is not imported once:\n%s", got)
	}
	if text, refused, _ := l.Step("add_declaration", []byte(`{"file": "src/garden.test.ts", "test_for": "Bed.dig"}`)); !refused {
		t.Errorf("a missing method is not refused:\n%s", text)
	}
}

// With SkeletonOnly, create_file refuses TypeScript with function bodies,
// and takes classes with fields, interfaces and types, and declaration.
func TestSkeletonOnly(t *testing.T) {
	root := t.TempDir()
	for name, text := range map[string]string{
		"tsconfig.json": `{"compilerOptions": {"strict": true, "noEmit": true, "types": []}, "include": ["src"]}`,
		"src/a.ts":      "export const a = 1;\n",
	} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	l := New(root, &history.Stack{})
	l.SkeletonOnly = true
	if text, refused, _ := l.Step("create_file", []byte(`{"file": "src/bed.ts", "content": "export class Bed {\n  depth = 1;\n  dig(): void {}\n}\n"}`)); !refused || !strings.Contains(text, "declaration") {
		t.Errorf("a method body is not refused, naming declaration:\n%s", text)
	}
	for _, args := range []string{`{"file": "src/bed.ts", "content": "export class Bed {\n  depth = 1;\n}\n\nexport interface Pot {\n  size: number;\n}\n"}`, `{"file": "src/pot.ts", "declaration": "export class Planter"}`} {
		if text, refused, err := l.Step("create_file", []byte(args)); err != nil || refused {
			t.Errorf("%s: %v %v\n%s", args, err, refused, text)
		}
	}
}

// inline is published in place of inline_function and inline_variable,
// and runs the one that what it names asks for.
func TestInline(t *testing.T) {
	root := t.TempDir()
	for name, text := range map[string]string{
		"tsconfig.json": `{"compilerOptions": {"strict": true, "noEmit": true, "types": []}, "include": ["src"]}`,
		"src/a.ts":      "function double(n: number): number {\n  return n * 2;\n}\n\nexport function use(): number {\n  const x = 3;\n  return double(x);\n}\n",
	} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	l := New(root, &history.Stack{})
	for _, args := range []string{`{"file": "src/a.ts", "line": 6, "name": "x"}`, `{"file": "src/a.ts", "line": 6, "name": "double"}`} {
		if text, refused, err := l.Step("inline", []byte(args)); err != nil || refused {
			t.Fatalf("inline %s: %v %v\n%s", args, err, refused, text)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(root, "src/a.ts")); strings.Contains(string(b), "double") || strings.Contains(string(b), "const x") {
		t.Errorf("not both inlined:\n%s", b)
	}
	var names []string
	for _, tool := range l.Tools() {
		names = append(names, tool.Name)
	}
	if !slices.Contains(names, "inline") || slices.Contains(names, "inline_function") || slices.Contains(names, "inline_variable") {
		t.Errorf("published: %v", names)
	}
}

// rename with a file and no name renames the file, updating its imports.
func TestRenameFile(t *testing.T) {
	root := t.TempDir()
	for name, text := range map[string]string{
		"tsconfig.json": `{"compilerOptions": {"strict": true, "noEmit": true, "types": []}, "include": ["src"]}`,
		"src/plant.ts":  "export const water = 1;\n",
		"src/bed.ts":    "import { water } from \"./plant\";\n\nexport const morning = water;\n",
	} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	l := New(root, &history.Stack{})
	if text, refused, err := l.Step("rename", []byte(`{"file": "src/plant.ts", "new_name": "water.ts"}`)); err != nil || refused {
		t.Fatalf("rename of a file: %v %v\n%s", err, refused, text)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "src/bed.ts")); !strings.Contains(string(b), "./water") {
		t.Errorf("bed.ts does not import from ./water:\n%s", b)
	}
}
