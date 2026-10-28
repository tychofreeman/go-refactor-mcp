package changesig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

var garden = map[string]string{
	"tsconfig.json": `{"compilerOptions": {"strict": true, "module": "ESNext", "moduleResolution": "Bundler", "noEmit": true, "types": []}, "include": ["src"]}`,
	"src/water.ts": `export function water(bed: string, liters: number): string {
  return ` + "`${bed}: ${liters} L`" + `;
}

export function prune(bed: string, height: number): string {
  return bed;
}

export class Sprinkler {
  spray(bed: string, minutes: number): string {
    return water(bed, minutes * 2);
  }
}
`,
	"src/garden.ts": `import { water, prune, Sprinkler } from "./water";

export const morning = water("roses", 2);
export const evening = new Sprinkler().spray("ferns", 5);
export const names = ["a", "b"].map((n) => water(n, 1));
export const short = prune("roses", 3);
`,
}

func load(t *testing.T, extra map[string]string) (*workspace.Workspace, string) {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{}
	for k, v := range garden {
		files[k] = v
	}
	for k, v := range extra {
		files[k] = v
	}
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

func run(t *testing.T, ws *workspace.Workspace, r refactor.RefactoringOf[*workspace.Workspace], args string) *refactor.Result {
	t.Helper()
	res, err := refactor.Run(ws, r, json.RawMessage(`{"file": "src/water.ts", `+args[1:]), refactor.OptionsOf[*workspace.Workspace]{})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func mustContain(t *testing.T, dir, file string, wants ...string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range wants {
		if !strings.Contains(string(b), w) {
			t.Errorf("%s lacks %q:\n%s", file, w, b)
		}
	}
}

func TestAddParameter(t *testing.T) {
	ws, dir := load(t, nil)
	res := run(t, ws, NewAdd(), `{"name": "water", "parameter": "unit", "type": "string", "value": "\"L\""}`)
	if !res.Applied {
		t.Fatalf("%s", res)
	}
	mustContain(t, dir, "src/water.ts", "function water(bed: string, liters: number, unit: string): string", `return water(bed, minutes * 2, "L");`)
	mustContain(t, dir, "src/garden.ts", `water("roses", 2, "L")`, `water(n, 1, "L")`)

	res = run(t, ws, NewAdd(), `{"name": "Sprinkler.spray", "parameter": "dry", "type": "boolean", "value": "false", "before": "bed"}`)
	if !res.Applied {
		t.Fatalf("%s", res)
	}
	mustContain(t, dir, "src/water.ts", "spray(dry: boolean, bed: string, minutes: number): string")
	mustContain(t, dir, "src/garden.ts", `spray(false, "ferns", 5)`)
}

// Where unused parameters are errors, a new one is named with an
// underscore, which TypeScript does not report.
func TestAddParameterWhereUnusedOnesAreErrors(t *testing.T) {
	ws, dir := load(t, map[string]string{"tsconfig.json": `{"compilerOptions": {"strict": true, "noUnusedParameters": true, "module": "ESNext", "moduleResolution": "Bundler", "noEmit": true, "types": []}, "include": ["src"]}`})
	res := run(t, ws, NewAdd(), `{"name": "water", "parameter": "unit", "type": "string", "value": "\"L\""}`)
	if want := "noUnusedParameters"; res.Applied || !strings.Contains(res.Refused, want) || !strings.Contains(res.Refused, "_unit") {
		t.Fatalf("want a refusal naming _unit, got %s", res)
	}
	if res := run(t, ws, NewAdd(), `{"name": "water", "parameter": "_unit", "type": "string", "value": "\"L\""}`); !res.Applied {
		t.Fatalf("%s", res)
	}
	mustContain(t, dir, "src/garden.ts", `water("roses", 2, "L")`)
}

func TestRemoveAndMoveParameter(t *testing.T) {
	ws, dir := load(t, nil)
	res := run(t, ws, NewMove(), `{"name": "prune", "parameter": "height", "before": "bed"}`)
	if !res.Applied {
		t.Fatalf("%s", res)
	}
	mustContain(t, dir, "src/water.ts", "function prune(height: number, bed: string): string")
	mustContain(t, dir, "src/garden.ts", `prune(3, "roses")`)

	res = run(t, ws, NewRemove(), `{"name": "prune", "parameter": "height"}`)
	if !res.Applied {
		t.Fatalf("%s", res)
	}
	mustContain(t, dir, "src/water.ts", "function prune(bed: string): string")
	mustContain(t, dir, "src/garden.ts", `prune("roses")`)

	res = run(t, ws, NewMove(), `{"name": "water", "parameter": "liters", "before": "bed"}`)
	if want := "the argument minutes * 2 of the call at water.ts:11 could have effects"; res.Applied || !strings.Contains(res.Refused, want) {
		t.Errorf("want a refusal with %q, got %s", want, res)
	}
}

func TestRefusals(t *testing.T) {
	ws, _ := load(t, map[string]string{"src/alias.ts": "import { water } from \"./water\";\n\nexport const pour = water;\n"})
	for _, tc := range []struct {
		r          refactor.RefactoringOf[*workspace.Workspace]
		args, want string
	}{
		{NewAdd(), `{"name": "water", "parameter": "unit", "type": "string", "value": "\"L\""}`, "water is used as a value at alias.ts:3"},
		{NewRemove(), `{"name": "water", "parameter": "bed"}`, "the body of water reads bed (water.ts:2)"},
		{NewAdd(), `{"name": "prune", "parameter": "bed", "type": "string", "value": "\"x\""}`, "already has a parameter bed"},
		{NewAdd(), `{"name": "prune", "parameter": "unit", "type": "string", "value": "nowhere"}`, "value names nowhere, which the call at garden.ts:6 cannot see"},
		{NewRemove(), `{"name": "Bed.trim", "parameter": "x"}`, "declares no function or method Bed.trim"},
		{NewAdd(), `{"name": "Sprinkler.constructor", "parameter": "x", "type": "number", "value": "1"}`, "constructor"},
	} {
		if res := run(t, ws, tc.r, tc.args); res.Applied || !strings.Contains(res.Refused, tc.want) {
			t.Errorf("%s %s: want a refusal with %q, got %s", tc.r.Name(), tc.args, tc.want, res)
		}
	}
}
