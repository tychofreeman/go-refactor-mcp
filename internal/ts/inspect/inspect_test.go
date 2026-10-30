package inspect

import (
	"os"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

func garden(t *testing.T) *workspace.Workspace {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("testdata/garden")); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

func mustContain(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in:\n%s", w, got)
		}
	}
}

// A file is shown with its function bodies elided: functions, methods,
// getters and arrow functions bound to a const, each in place of its body
// a line with the lines it takes.
func TestFileWithoutBodies(t *testing.T) {
	out, err := RunWith(garden(t), Query{File: "src/garden.ts"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out,
		"src/garden.ts  (TypeScript, 35 lines)",
		"source, function bodies elided (inspect with function, or start_line/end_line, to read one):",
		"10  export class Bed {",
		"16    plant(p: Plant): void {",
		"… lines 17-18   [2 lines]",
		"… lines 22-23   [2 lines]",
		"27  export const water = (liters: number): string => {",
		"… lines 28-29   [2 lines]",
		"… lines 33-34   [2 lines]",
		"13    constructor(public readonly soil: string) {}",
	)
	for _, body := range []string{"this.plants.push", "liters * 1000", "Math.max"} {
		if strings.Contains(out, body) {
			t.Errorf("a body is shown: %q", body)
		}
	}
}

func TestFunctionAndLines(t *testing.T) {
	ws := garden(t)
	out, err := RunWith(ws, Query{File: "src/garden.ts", Function: "Bed.plant"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "15-19", "Bed.plant", "15    /** plant adds p to the bed. */", "17      this.plants.push(p);")
	out, err = RunWith(ws, Query{File: "src/garden.ts", StartLine: 27, EndLine: 28}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "source (lines 27-28):", "28    const ml = liters * 1000;")
	if _, err := RunWith(ws, Query{File: "src/garden.ts", StartLine: 27, EndLine: 28}, Options{ByName: true}); !refactor.IsRefusal(err) {
		t.Errorf("a range read by name only was not refused: %v", err)
	}
}

// Names are found in every file of the solution; a name two files declare
// is refused, saying how to qualify it.
func TestNames(t *testing.T) {
	ws := garden(t)
	out, err := RunWith(ws, Query{Names: []string{"water", "Bed.tallest"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "28    const ml = liters * 1000;", "22      const n = this.plants.length;")
	_, err = RunWith(ws, Query{Names: []string{"prune"}}, Options{})
	if !refactor.IsRefusal(err) || !strings.Contains(err.Error(), "src/garden.ts:prune") || !strings.Contains(err.Error(), "src/shed.ts:prune") {
		t.Errorf("a name two files declare: %v", err)
	}
	out, err = RunWith(ws, Query{Names: []string{"src/shed.ts:prune"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "src/shed.ts", "1  export function prune(): void {}")
	out, err = RunWith(ws, Query{Names: []string{"Shed.#lock", "Shed.[key]"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "Shed.#lock(): void", "12      console.log(\"locked\");", "Shed.[key](): string")
}

func TestOverviewTargetsAndUses(t *testing.T) {
	ws := garden(t)
	out, err := RunWith(ws, Query{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "2 files", "src  garden.ts, shed.ts")
	out, err = RunWith(ws, Query{Targets: []Query{{File: "src/shed.ts"}, {File: "src/garden.ts", Function: "prune"}}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "src/shed.ts  (TypeScript, 15 lines)", "… lines 7-8   [2 lines]", "33    const height = Math.max(0, p.height - by);")
	out, err = RunWith(ws, Query{Uses: []string{"Plant", "Plant.height", "water", "prune", "Nope"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out,
		"Plant (interface in src/garden.ts): 5 uses\nused as a type (5):\n  src/garden.ts:11  private plants: Plant[] = [];\n",
		"Plant.height (property of Plant in src/garden.ts): 3 uses\nread (3):\n  src/garden.ts:18  this.plants.sort((a, b) => a.height - b.height);\n  src/garden.ts:18  this.plants.sort((a, b) => a.height - b.height);\n  src/garden.ts:33  const height = Math.max(0, p.height - by);\n",
		"water (variable in src/garden.ts): no uses",
		"prune: declared in src/garden.ts, src/shed.ts; inspect it by file",
		"Nope: no declaration of that name in the solution")
	if _, err := RunWith(ws, Query{File: "src/garden.ts", Uses: []string{"Plant"}}, Options{}); !refactor.IsRefusal(err) {
		t.Errorf("uses with a file was not refused: %v", err)
	}
}
