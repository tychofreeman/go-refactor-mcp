package replacestmts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tstest"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

func garden(t *testing.T) (*workspace.Workspace, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("../../inspect/testdata/garden")); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return ws, dir
}

func run(t *testing.T, ws *workspace.Workspace, args string) *refactor.Result {
	t.Helper()
	res := tstest.Run(t, ws, New(), args)
	return res
}

func source(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "src/garden.ts"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLines(t *testing.T) {
	ws, dir := garden(t)
	res := run(t, ws, `{"file": "src/garden.ts", "start_line": 17, "end_line": 18, "content": "this.plants.unshift(p);"}`)
	if want := "  plant(p: Plant): void {\n    this.plants.unshift(p);\n  }\n"; !res.Applied || !strings.Contains(source(t, dir), want) {
		t.Fatalf("%s\n%s", res, source(t, dir))
	}
	res = run(t, ws, `{"file": "src/garden.ts", "start_line": 17, "end_line": 17, "content": ""}`)
	if want := "  plant(p: Plant): void {\n  }\n"; !res.Applied || !strings.Contains(source(t, dir), want) {
		t.Fatalf("empty content did not delete the statement: %s\n%s", res, source(t, dir))
	}
}

// function replaces a whole body: an empty one on the signature's line,
// and an arrow function's.
func TestFunction(t *testing.T) {
	ws, dir := garden(t)
	res := run(t, ws, `{"file": "src/garden.ts", "function": "Bed.constructor", "content": "if (soil === \"\") {\n  throw new Error(\"no soil\");\n}"}`)
	if want := "  constructor(public readonly soil: string) {\n    if (soil === \"\") {\n      throw new Error(\"no soil\");\n    }\n  }\n"; !res.Applied || !strings.Contains(source(t, dir), want) {
		t.Fatalf("%s\n%s", res, source(t, dir))
	}
	res = run(t, ws, "{\"file\": \"src/garden.ts\", \"function\": \"water\", \"content\": \"return `${liters} L`;\"}")
	if want := "export const water = (liters: number): string => {\n  return `${liters} L`;\n};\n"; !res.Applied || !strings.Contains(source(t, dir), want) {
		t.Fatalf("%s\n%s", res, source(t, dir))
	}
}

// function and case replace a case's body.
func TestCase(t *testing.T) {
	ws, dir := garden(t)
	res := run(t, ws, `{"file": "src/garden.ts", "function": "water", "content": "switch (liters) {\n  case 1:\n    return \"one\";\n  default:\n    return \"many\";\n}"}`)
	if !res.Applied {
		t.Fatalf("%s", res)
	}
	res = run(t, ws, `{"file": "src/garden.ts", "function": "water", "case": "1", "content": "return \"a liter\";"}`)
	if want := "    case 1:\n      return \"a liter\";\n    default:"; !res.Applied || !strings.Contains(source(t, dir), want) {
		t.Fatalf("%s\n%s", res, source(t, dir))
	}
}

func TestRefusals(t *testing.T) {
	ws, _ := garden(t)
	for _, tc := range []struct{ args, want string }{
		{`{"file": "src/garden.ts", "start_line": 16, "end_line": 18, "content": "x;"}`, "lines 16-18 do not hold whole statements of one block; lines 17-18 do"},
		{`{"file": "src/garden.ts", "start_line": 18, "end_line": 19, "content": "x;"}`, "do not hold whole statements"},
		{`{"file": "src/garden.ts", "start_line": 17, "end_line": 17, "content": "}"}`, "content is not TypeScript statements"},
		{`{"file": "src/garden.ts", "start_line": 17, "end_line": 17, "content": "this.plants.push(1);"}`, "not assignable"},
		{`{"file": "src/garden.ts", "function": "Bed.nothing", "content": "x;"}`, `declares no function "Bed.nothing"`},
		{`{"file": "src/garden.ts", "function": "Bed.plant", "start_line": 17, "end_line": 17, "content": "x;"}`, "not both"},
	} {
		if res := run(t, ws, tc.args); res.Applied || !strings.Contains(res.Refused, tc.want) {
			t.Errorf("%s: want a refusal with %q, got %s", tc.args, tc.want, res)
		}
	}
}

// wrap puts statements inside new ones, at the line $body: lines, a
// whole body, and a closure, whose return the result notes.
func TestWrap(t *testing.T) {
	ws, dir := garden(t)
	res := run(t, ws, `{"file": "src/garden.ts", "start_line": 17, "end_line": 18, "wrap": "if (p.height > 0) {\n$body\n}"}`)
	if want := "    if (p.height > 0) {\n      this.plants.push(p);\n      this.plants.sort((a, b) => a.height - b.height);\n    }\n  }\n"; !res.Applied || !strings.Contains(source(t, dir), want) {
		t.Fatalf("%s\n%s", res, source(t, dir))
	}
	res = run(t, ws, `{"file": "src/garden.ts", "function": "prune", "wrap": "if (by >= 0) {\n$body\n}\nreturn p;"}`)
	if want := "  if (by >= 0) {\n    const height = Math.max(0, p.height - by);\n    return { ...p, height };\n  }\n  return p;\n}\n"; !res.Applied || !strings.Contains(source(t, dir), want) {
		t.Fatalf("%s\n%s", res, source(t, dir))
	}
	res = run(t, ws, `{"file": "src/garden.ts", "function": "Bed.tallest", "wrap": "return (() => {\n$body\n})();"}`)
	if !res.Applied || !strings.Contains(res.String(), "return at line 25 now leaves the new function, not the one it was in") {
		t.Fatalf("%s\n%s", res, source(t, dir))
	}
}

func TestWrapRefusals(t *testing.T) {
	ws, _ := garden(t)
	for _, tc := range []struct{ args, want string }{
		{`{"file": "src/garden.ts", "start_line": 28, "end_line": 28, "wrap": "{\n$body\n}"}`, "ml is declared in the statements and used after them (garden.ts:29"},
		{`{"file": "src/garden.ts", "start_line": 17, "end_line": 17, "wrap": "if (true) { $body }"}`, "on a line of its own"},
		{`{"file": "src/garden.ts", "start_line": 17, "end_line": 17, "wrap": "if (true) {\n$body\n}", "content": "x;"}`, "give content or wrap, not both"},
		{`{"file": "src/garden.ts", "start_line": 17, "end_line": 17}`, "give content, or wrap"},
	} {
		if res := run(t, ws, tc.args); res.Applied || !strings.Contains(res.Refused, tc.want) {
			t.Errorf("%s: want a refusal with %q, got %s", tc.args, tc.want, res)
		}
	}
}
