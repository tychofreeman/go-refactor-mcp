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

var beds = map[string]string{"src/bed.ts": `export class Bed {
  constructor(public readonly soil: string, rows: number) {}
}

export class RaisedBed extends Bed {
  constructor(height: number) {
    super("loam", height);
  }
}

export const bed = new Bed("clay", 3);
`}

// A constructor's calls are the news of its class and the super calls of
// the classes extending it.
func TestConstructor(t *testing.T) {
	ws, dir := load(t, beds)
	runBed := func(r refactor.RefactoringOf[*workspace.Workspace], args string) *refactor.Result {
		t.Helper()
		res, err := refactor.Run(ws, r, json.RawMessage(`{"file": "src/bed.ts", `+args[1:]), refactor.OptionsOf[*workspace.Workspace]{})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := runBed(NewAdd(), `{"name": "Bed.constructor", "parameter": "width", "type": "number", "value": "1"}`); !res.Applied {
		t.Fatalf("%s", res)
	}
	mustContain(t, dir, "src/bed.ts", "constructor(public readonly soil: string, rows: number, width: number) {}", `super("loam", height, 1);`, `new Bed("clay", 3, 1)`)
	if res := runBed(NewRemove(), `{"name": "Bed.constructor", "parameter": "rows"}`); !res.Applied {
		t.Fatalf("%s", res)
	}
	mustContain(t, dir, "src/bed.ts", "constructor(public readonly soil: string, width: number) {}", `super("loam", 1);`, `new Bed("clay", 1)`)
	if res := runBed(NewRemove(), `{"name": "Bed.constructor", "parameter": "soil"}`); res.Applied || !strings.Contains(res.Refused, "soil is a parameter property") {
		t.Errorf("a parameter property was removed: %s", res)
	}

	for extra, want := range map[string]string{
		"export class Plot extends Bed {}\n":                                "Plot extends Bed without a constructor of its own",
		"export const make = () => [Bed].map((C) => new C(\"a\", 1, 1));\n": "Bed is used as a value at plot.ts:2",
	} {
		ws, _ := load(t, map[string]string{"src/bed.ts": beds["src/bed.ts"], "src/plot.ts": "import { Bed } from \"./bed\";\n" + extra})
		res, err := refactor.Run(ws, NewAdd(), json.RawMessage(`{"file": "src/bed.ts", "name": "Bed.constructor", "parameter": "width", "type": "number", "value": "1"}`), refactor.OptionsOf[*workspace.Workspace]{})
		if err != nil {
			t.Fatal(err)
		}
		if res.Applied || !strings.Contains(res.Refused, want) {
			t.Errorf("want a refusal with %q, got %s", want, res)
		}
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
		{NewAdd(), `{"name": "Sprinkler.constructor", "parameter": "x", "type": "number", "value": "1"}`, "Sprinkler has no constructor of its own"},
	} {
		if res := run(t, ws, tc.r, tc.args); res.Applied || !strings.Contains(res.Refused, tc.want) {
			t.Errorf("%s %s: want a refusal with %q, got %s", tc.r.Name(), tc.args, tc.want, res)
		}
	}
}

var labels = map[string]string{
	"src/label.ts": `export interface Plant {
  name: string;
  height: number;
}

export const UNIT = "cm";

export function label(p: Plant): string {
  return p.name + " " + p.height + UNIT;
}

export function grow(p: Plant): number {
  const k = p.height;
  return k;
}

export function log(p: Plant): void {}

export function measure(p: Plant): number {
  log(p);
  return p.height;
}

export class Bed {
  depth = 3;
  describe(p: Plant): string {
    return p.name + this.depth;
  }
}
`,
	"src/use.ts": `import { label, grow, measure, Bed, Plant } from "./label";

const rose: Plant = { name: "rose", height: 2 };
export const a = label(rose);
export const g = grow(rose);
export const m = measure(rose);
const bed = new Bed();
export const b = bed.describe(rose);
`,
}

func TestIntroduceParameter(t *testing.T) {
	ws, dir := load(t, labels)
	intro := func(args string) *refactor.Result {
		t.Helper()
		res, err := refactor.Run(ws, NewIntroduce(), json.RawMessage(`{"file": "src/label.ts", `+args[1:]), refactor.OptionsOf[*workspace.Workspace]{})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := intro(`{"function": "label", "expression": "p.height", "parameter": "height"}`); !res.Applied {
		t.Fatalf("%s", res)
	}
	mustContain(t, dir, "src/label.ts", "export function label(p: Plant, height: number): string {\n  return p.name + \" \" + height + UNIT;")
	mustContain(t, dir, "src/use.ts", "label(rose, rose.height)")
	if res := intro(`{"function": "Bed.describe", "expression": "this.depth", "parameter": "depth"}`); !res.Applied {
		t.Fatalf("%s", res)
	}
	mustContain(t, dir, "src/label.ts", "describe(p: Plant, depth: number): string {\n    return p.name + depth;")
	mustContain(t, dir, "src/use.ts", "bed.describe(rose, bed.depth)")

	for args, want := range map[string]string{
		`{"function": "label", "expression": "UNIT", "parameter": "unit"}`:    "UNIT in UNIT is not the same at the call at use.ts:4",
		`{"function": "grow", "expression": "k", "parameter": "h"}`:           "reads k, a local of grow",
		`{"function": "measure", "expression": "p.height", "parameter": "h"}`: "reads state the body may change before it reads it (at label.ts:20)",
		`{"function": "label", "expression": "p.weight", "parameter": "w"}`:   "has no expression p.weight",
	} {
		if res := intro(args); res.Applied || !strings.Contains(res.Refused, want) {
			t.Errorf("%s: want a refusal with %q, got %s", args, want, res)
		}
	}
}

func TestParamsToObject(t *testing.T) {
	ws, dir := load(t, map[string]string{"src/plant.ts": "export function plant(...names: string[]): number {\n  return names.length;\n}\n"})
	if res := run(t, ws, NewParamsToObject(), `{"name": "water"}`); !res.Applied {
		t.Fatalf("%s", res)
	}
	mustContain(t, dir, "src/water.ts", "export function water({ bed, liters }: { bed: string; liters: number }): string {", "return water({ bed, liters: minutes * 2 });")
	mustContain(t, dir, "src/garden.ts", `water({ bed: "roses", liters: 2 })`, "water({ bed: n, liters: 1 })")
	if res := run(t, ws, NewParamsToObject(), `{"name": "prune", "type_name": "PruneOptions"}`); !res.Applied {
		t.Fatalf("%s", res)
	}
	mustContain(t, dir, "src/water.ts", "export interface PruneOptions {\n  bed: string;\n  height: number;\n}\n\nexport function prune({ bed, height }: PruneOptions): string {")
	mustContain(t, dir, "src/garden.ts", `prune({ bed: "roses", height: 3 })`)

	res, err := refactor.Run(ws, NewParamsToObject(), json.RawMessage(`{"file": "src/plant.ts", "name": "plant"}`), refactor.OptionsOf[*workspace.Workspace]{})
	if err != nil {
		t.Fatal(err)
	}
	if want := "plant has a rest parameter"; res.Applied || !strings.Contains(res.Refused, want) {
		t.Errorf("want a refusal with %q, got %s", want, res)
	}
}

func TestAddResult(t *testing.T) {
	ws, dir := load(t, map[string]string{
		"src/load.ts": `export function load(id: string): string {
  if (id === "") {
    return "none";
  }
  return id.toUpperCase();
}

export const count = (n: number): number => (n <= 0 ? 0 : count(n - 1) + 1);

export function noop(): void {}
`,
		"src/use.ts": `import { load, count } from "./load";

export const v = load("a");
export const len = load("b").length;
load("c");
export const c = count(3);
`,
	})
	add := func(args string) *refactor.Result {
		t.Helper()
		res, err := refactor.Run(ws, NewAddResult(), json.RawMessage(`{"file": "src/load.ts", `+args[1:]), refactor.OptionsOf[*workspace.Workspace]{})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := add(`{"name": "load", "field": "error", "type": "string | null", "value": "null"}`); !res.Applied {
		t.Fatalf("%s", res)
	}
	mustContain(t, dir, "src/load.ts", "export function load(id: string): { value: string; error: string | null } {", `return { value: "none", error: null };`, "return { value: id.toUpperCase(), error: null };")
	mustContain(t, dir, "src/use.ts", `export const v = load("a").value;`, `load("b").value.length`, "\nload(\"c\");\n")
	if res := add(`{"name": "count", "field": "steps", "type": "number", "value": "0", "kept": "total"}`); !res.Applied {
		t.Fatalf("%s", res)
	}
	mustContain(t, dir, "src/load.ts", "export const count = (n: number): { total: number; steps: number } => ({ total: (n <= 0 ? 0 : count(n - 1).total + 1), steps: 0 });")
	mustContain(t, dir, "src/use.ts", "count(3).total")
	for args, want := range map[string]string{
		`{"name": "noop", "field": "e", "type": "number", "value": "0"}`:          "noop returns void",
		`{"name": "load", "field": "x", "type": "number", "value": "Date.now()"}`: "could have effects",
	} {
		if res := add(args); res.Applied || !strings.Contains(res.Refused, want) {
			t.Errorf("%s: want a refusal with %q, got %s", args, want, res)
		}
	}
}
