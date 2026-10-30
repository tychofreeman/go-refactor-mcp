package changesig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/edits/changetype"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/edits/replacetext"
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

// retyped is a file of types whose declared types the next test changes.
const retyped = `export type Liters = number;

export type Size = "small" | "big";

export interface Plant {
  name: string;
  height?: number;
}

export class Bed {
  depth = 2;
  plants: Plant[] = [];
}

export function grow(p: Plant, by: number): Plant {
  return { ...p, height: (p.height ?? 0) + by };
}

export const halve: (n: number) => number = n => n / 2;

export const label = (p: Plant): string => p.name;
`

func TestChangeTypes(t *testing.T) {
	ws, dir := load(t, map[string]string{"src/plant.ts": retyped})
	in := func(r refactor.RefactoringOf[*workspace.Workspace], args string) *refactor.Result {
		t.Helper()
		res, err := refactor.Run(ws, r, json.RawMessage(`{"file": "src/plant.ts", `+args[1:]), refactor.OptionsOf[*workspace.Workspace]{})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	for _, tc := range []struct {
		r    refactor.RefactoringOf[*workspace.Workspace]
		args string
	}{
		{NewChangeFieldType(), `{"name": "Plant.height", "type": "Liters"}`},
		{NewChangeFieldType(), `{"name": "Bed.depth", "type": "Liters"}`},
		{NewChangeParameterType(), `{"name": "grow", "parameter": "by", "type": "Liters"}`},
		{NewChangeParameterType(), `{"name": "halve", "parameter": "n", "type": "Liters"}`},
		{changetype.New(), `{"name": "Size", "type": "\"small\" | \"big\" | \"huge\""}`},
	} {
		if res := in(tc.r, tc.args); !res.Applied {
			t.Fatalf("%s %s: %s", tc.r.Name(), tc.args, res)
		}
	}
	mustContain(t, dir, "src/plant.ts", "  height?: Liters;", "  depth: Liters = 2;", "grow(p: Plant, by: Liters)", "= (n: Liters) => n / 2;", `type Size = "small" | "big" | "huge";`)
	for _, tc := range []struct {
		r          refactor.RefactoringOf[*workspace.Workspace]
		args, want string
	}{
		{NewChangeFieldType(), `{"name": "Plant.name", "type": "number"}`, "Type 'number' is not assignable to type 'string'"},
		{NewChangeFieldType(), `{"name": "Bed.soil", "type": "string"}`, "Bed has no property soil"},
		{NewChangeFieldType(), `{"name": "Size.small", "type": "string"}`, "change it with change_type"},
		{NewChangeParameterType(), `{"name": "grow", "parameter": "q", "type": "Plant"}`, "grow has no parameter named q"},
		{changetype.New(), `{"name": "Plant", "type": "string"}`, "change its members with change_field_type"},
	} {
		if res := in(tc.r, tc.args); res.Applied || !strings.Contains(res.Refused, tc.want) {
			t.Errorf("%s %s: want a refusal with %q, got %s", tc.r.Name(), tc.args, tc.want, res)
		}
	}
}

// hoses is a file of an interface, two classes that implement it, a
// subclass that overrides one, and calls through each.
const hoses = `export interface Waterer {
  pour(bed: string, liters: number): string;
}

export class Hose implements Waterer {
  pour(bed: string, liters: number): string {
    return bed + liters;
  }
}

export class Can implements Waterer {
  pour(bed: string, amount: number): string {
    return bed;
  }
}

export class Sprayer extends Hose {
  pour(bed: string, liters: number): string {
    return super.pour(bed, liters * 2);
  }
}

export function all(w: Waterer): string {
  return w.pour("roses", 1) + new Can().pour("ferns", 2) + new Sprayer().pour("moss", 3);
}
`

// A method that implements an interface's method, or overrides one,
// changes with its family: the interface, every class that implements
// it, every override, and the calls through each.
func TestMethodFamilies(t *testing.T) {
	in := func(t *testing.T, ws *workspace.Workspace, r refactor.RefactoringOf[*workspace.Workspace], args string) *refactor.Result {
		t.Helper()
		res, err := refactor.Run(ws, r, json.RawMessage(`{"file": "src/hoses.ts", `+args[1:]), refactor.OptionsOf[*workspace.Workspace]{})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	t.Run("add", func(t *testing.T) {
		for _, name := range []string{"Hose.pour", "Waterer.pour", "Sprayer.pour"} {
			ws, dir := load(t, map[string]string{"src/hoses.ts": hoses})
			if res := in(t, ws, NewAdd(), `{"name": "`+name+`", "parameter": "unit", "type": "string", "value": "\"L\""}`); !res.Applied {
				t.Fatalf("%s: %s", name, res)
			}
			mustContain(t, dir, "src/hoses.ts",
				"  pour(bed: string, liters: number, unit: string): string;\n",
				"  pour(bed: string, liters: number, unit: string): string {\n    return bed + liters;",
				"  pour(bed: string, amount: number, unit: string): string {\n    return bed;",
				`return super.pour(bed, liters * 2, "L");`,
				`w.pour("roses", 1, "L") + new Can().pour("ferns", 2, "L") + new Sprayer().pour("moss", 3, "L")`)
		}
	})
	t.Run("move", func(t *testing.T) {
		ws, dir := load(t, map[string]string{"src/hoses.ts": hoses})
		if res := in(t, ws, NewMove(), `{"name": "Can.pour", "parameter": "amount", "before": "bed"}`); !res.Applied {
			t.Fatalf("%s", res)
		}
		mustContain(t, dir, "src/hoses.ts", "pour(liters: number, bed: string): string;", "pour(amount: number, bed: string): string {", "super.pour(liters * 2, bed)", `w.pour(1, "roses")`)
	})
	t.Run("remove", func(t *testing.T) {
		ws, _ := load(t, map[string]string{"src/hoses.ts": hoses})
		res := in(t, ws, NewRemove(), `{"name": "Can.pour", "parameter": "amount"}`)
		if res.Applied || !strings.Contains(res.Refused, "the body of Hose.pour, which changes with Can.pour, reads liters") {
			t.Errorf("want a refusal naming Hose.pour's body, got %s", res)
		}
	})
}

// A function becomes a method of its first parameter's class and back;
// the calls in other files follow, with their imports.
func TestFunctionToMethodAndBack(t *testing.T) {
	ws, dir := load(t, map[string]string{
		"src/bed.ts": `export class Bed {
  depth = 2;
  plants: string[] = [];

  size(): number {
    return this.plants.length * this.depth;
  }
}

/** Liters a bed takes. */
export function liters(b: Bed, per: number): number {
  return b.size() * per;
}
`,
		"src/use.ts": `import { Bed, liters } from "./bed";

export const l = liters(new Bed(), 2);
export const twice = (b: Bed) => liters(b, 1) + b.size();
`,
	})
	in := func(r refactor.RefactoringOf[*workspace.Workspace], args string) {
		t.Helper()
		res, err := refactor.Run(ws, r, json.RawMessage(`{"file": "src/bed.ts", `+args[1:]), refactor.OptionsOf[*workspace.Workspace]{})
		if err != nil {
			t.Fatal(err)
		}
		if !res.Applied {
			t.Fatalf("%s %s: %s", r.Name(), args, res)
		}
	}
	in(NewFunctionToMethod(), `{"name": "liters"}`)
	want := func(file, text string) {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != text {
			t.Fatalf("%s:\n%s\nwant:\n%s", file, b, text)
		}
	}
	want("src/bed.ts", `export class Bed {
  depth = 2;
  plants: string[] = [];

  size(): number {
    return this.plants.length * this.depth;
  }

  /** Liters a bed takes. */
  liters(per: number): number {
    return this.size() * per;
  }
}
`)
	want("src/use.ts", `import { Bed } from "./bed";

export const l = (new Bed()).liters(2);
export const twice = (b: Bed) => b.liters(1) + b.size();
`)
	in(NewMethodToFunction(), `{"name": "Bed.size", "new_name": "area"}`)
	want("src/bed.ts", `export class Bed {
  depth = 2;
  plants: string[] = [];

  /** Liters a bed takes. */
  liters(per: number): number {
    return area(this) * per;
  }
}

export function area(bed: Bed): number {
  return bed.plants.length * bed.depth;
}
`)
	want("src/use.ts", `import { Bed } from "./bed";
import { area } from "./bed";

export const l = (new Bed()).liters(2);
export const twice = (b: Bed) => b.liters(1) + area(b);
`)
}

func TestFunctionToMethodRefusals(t *testing.T) {
	ws, _ := load(t, map[string]string{"src/bed.ts": `export class Bed {
  depth = 2;
  size(): number {
    return this.depth;
  }
}

export class Raised extends Bed {
  size(): number {
    return 3;
  }
}

export function shade(n: number): number {
  return n;
}

export function depth(b: Bed): number {
  return b.depth;
}

export function deeper(b: Bed): Bed {
  b = new Bed();
  return b;
}

export const all = [new Bed()].map(depth);
`})
	for _, tc := range []struct {
		r          refactor.RefactoringOf[*workspace.Workspace]
		args, want string
	}{
		{NewFunctionToMethod(), `{"name": "shade"}`, "not of a class of the solution"},
		{NewFunctionToMethod(), `{"name": "depth"}`, "Bed already has a member depth"},
		{NewFunctionToMethod(), `{"name": "depth", "new_name": "deep"}`, "used as a value"},
		{NewFunctionToMethod(), `{"name": "deeper"}`, "assigns its parameter b"},
		{NewMethodToFunction(), `{"name": "Bed.size"}`, "Raised overrides Bed.size"},
		{NewMethodToFunction(), `{"name": "Raised.size"}`, "implements or overrides a member of Bed"},
	} {
		res, err := refactor.Run(ws, tc.r, json.RawMessage(`{"file": "src/bed.ts", `+tc.args[1:]), refactor.OptionsOf[*workspace.Workspace]{})
		if err != nil {
			t.Fatal(err)
		}
		if res.Applied || !strings.Contains(res.Refused, tc.want) {
			t.Errorf("%s %s: want a refusal with %q, got %s", tc.r.Name(), tc.args, tc.want, res)
		}
	}
}

// A change that breaks a use goes through in a batch checked at its end,
// together with the change to the use.
func TestChangeTypeInABatch(t *testing.T) {
	ws, dir := load(t, map[string]string{"src/plant.ts": retyped})
	step := func(r refactor.RefactoringOf[*workspace.Workspace], args string) refactor.StepOf[*workspace.Workspace] {
		return refactor.StepOf[*workspace.Workspace]{Refactoring: r, Args: json.RawMessage(`{"file": "src/plant.ts", ` + args[1:])}
	}
	b, err := refactor.RunBatch(ws, []refactor.StepOf[*workspace.Workspace]{
		step(NewChangeFieldType(), `{"name": "Plant.name", "type": "number"}`),
		step(replacetext.New(), `{"old": "=> p.name;", "new": "=> String(p.name);"}`),
	}, refactor.OptionsOf[*workspace.Workspace]{Unchecked: true})
	if err != nil {
		t.Fatal(err)
	}
	if !b.Applied {
		t.Fatalf("not applied: %s", b)
	}
	mustContain(t, dir, "src/plant.ts", "  name: number;", "=> String(p.name);")
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

	// minutes * 2 is arithmetic on numbers, which may move.
	if res := run(t, ws, NewMove(), `{"name": "water", "parameter": "liters", "before": "bed"}`); !res.Applied {
		t.Fatalf("%s", res)
	}
	mustContain(t, dir, "src/water.ts", "return water(minutes * 2, bed);")
}

// TestEffects checks what removing and moving arguments may drop or
// reorder, judged from their types.
func TestEffects(t *testing.T) {
	head := "import { water, prune } from \"./water\";\nlet n = 0;\nfunction next(): number { return n++; }\n"
	for _, tc := range []struct{ name, use, tool, args, want string }{
		{"pure", "export const a = prune(\"x\", Math.max(1, 2));\nexport const b = prune(\"y\", \"ab\".length + [1, 2].map((x) => x * 2).length);\n", "remove", `{"name": "prune", "parameter": "height"}`, ""},
		{"call", "export const a = prune(\"x\", next());\n", "remove", `{"name": "prune", "parameter": "height"}`, "would drop the call next() at use.ts:4"},
		{"nullable", "declare const p: { h: number } | undefined;\nexport const a = prune(\"x\", p!.h);\n", "remove", `{"name": "prune", "parameter": "height"}`, "p!.h, which reads a property of what may be null or undefined"},
		{"getter", "const bed = { get h() { return n; } };\nexport const a = prune(\"x\", bed.h);\n", "remove", `{"name": "prune", "parameter": "height"}`, "the getter bed.h at use.ts:5"},
		{"arithmetic", "export const a = water(`${n}`, n * 2);\n", "move", `{"name": "water", "parameter": "liters", "before": "bed"}`, ""},
		{"constant", "export const a = water(\"x\", next());\n", "move", `{"name": "water", "parameter": "liters", "before": "bed"}`, ""},
		{"order", "export const a = water(`${n}`, next());\n", "move", `{"name": "water", "parameter": "liters", "before": "bed"}`, "their order matters: the call next() at use.ts:4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws, _ := load(t, map[string]string{"src/use.ts": head + tc.use})
			var r refactor.RefactoringOf[*workspace.Workspace] = NewRemove()
			if tc.tool == "move" {
				r = NewMove()
			}
			res := run(t, ws, r, tc.args)
			switch {
			case tc.want == "" && !res.Applied:
				t.Errorf("want applied, got %s", res)
			case tc.want != "" && (res.Applied || !strings.Contains(res.Refused, tc.want)):
				t.Errorf("want a refusal with %q, got %s", tc.want, res)
			}
		})
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

export function log(p: Plant): void {
  p.height++;
}

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
		`{"function": "measure", "expression": "p.height", "parameter": "h"}`: "reads state the body may change before it reads it (at label.ts:22)",
		`{"function": "label", "expression": "p.weight", "parameter": "w"}`:   "has no expression p.weight",
		`{"function": "measure", "expression": "log(p)", "parameter": "l"}`:   "every call would evaluate log(p) instead of the body, and it may not be moved: the call log(p) at label.ts:22",
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

var overloaded = map[string]string{
	"src/grow.ts": `export function grow(p: string): string;
export function grow(p: number, by: number): number;
export function grow(p: string | number, by = 1): string | number {
  return typeof p === "string" ? p + "!" : p + by;
}

export function shade(p: string, x: number): string;
export function shade(p: number): number;
export function shade(p: string | number, x?: number): string | number {
  return p;
}
`,
	"src/use.ts": `import { grow, shade } from "./grow";

export const s = grow("rose");
export const n = grow(2, 3);
export const a = shade("a", 1);
export const b = shade(2);
`,
}

// Overloads change with their implementation, each at the same
// position, where they have the parameter.
func TestOverloads(t *testing.T) {
	ws, dir := load(t, overloaded)
	do := func(r refactor.RefactoringOf[*workspace.Workspace], args string) *refactor.Result {
		t.Helper()
		res, err := refactor.Run(ws, r, json.RawMessage(`{"file": "src/grow.ts", `+args[1:]), refactor.OptionsOf[*workspace.Workspace]{})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := do(NewAdd(), `{"name": "grow", "parameter": "unit", "type": "string", "value": "\"cm\""}`); res.Applied || !strings.Contains(res.Refused, "the overload at grow.ts:1 has 1 parameters") {
		t.Errorf("a parameter added where an overload cannot take it: %s", res)
	}
	if res := do(NewMove(), `{"name": "grow", "parameter": "by", "before": "p"}`); res.Applied || !strings.Contains(res.Refused, "the overload at grow.ts:1 has only some of the parameters") {
		t.Errorf("a move an overload only partly has: %s", res)
	}
	if res := do(NewAdd(), `{"name": "grow", "parameter": "unit", "type": "string", "value": "\"cm\"", "before": "by"}`); !res.Applied {
		t.Fatalf("%s", res)
	}
	mustContain(t, dir, "src/grow.ts", "grow(p: string, unit: string): string;", "grow(p: number, unit: string, by: number): number;", "grow(p: string | number, unit: string, by = 1): string | number {")
	mustContain(t, dir, "src/use.ts", `grow("rose", "cm")`, `grow(2, "cm", 3)`)

	if res := do(NewRemove(), `{"name": "shade", "parameter": "x"}`); !res.Applied {
		t.Fatalf("%s", res)
	}
	mustContain(t, dir, "src/grow.ts", "shade(p: string): string;", "shade(p: number): number;", "shade(p: string | number): string | number {")
	mustContain(t, dir, "src/use.ts", `shade("a")`, "shade(2)")

	if res := do(NewAddResult(), `{"name": "grow", "field": "ok", "type": "boolean", "value": "true"}`); !res.Applied {
		t.Fatalf("%s", res)
	}
	mustContain(t, dir, "src/grow.ts", "unit: string): { value: string; ok: boolean };", "by: number): { value: number; ok: boolean };", "by = 1): { value: string | number; ok: boolean } {")
	mustContain(t, dir, "src/use.ts", `grow("rose", "cm").value`)

	if res := do(NewParamsToObject(), `{"name": "shade"}`); !res.Applied {
		t.Fatalf("%s", res)
	}
	mustContain(t, dir, "src/grow.ts", "export function shade({ p }: { p: string }): string;", "export function shade({ p }: { p: number }): number;", "export function shade({ p }: { p: string | number }): string | number {")
	mustContain(t, dir, "src/use.ts", `shade({ p: "a" })`, "shade({ p: 2 })")
}

var passed = map[string]string{
	"src/label.ts": `export interface Plant {
  name: string;
  height: number;
}

export function label(p: Plant, height: number): string {
  return p.name + " " + height;
}

export function tall(p: Plant, height: number): boolean {
  return height > 2;
}

export function show(p: Plant, height: number): string {
  return label(p, height) + "!";
}

export class Bed {
  depth = 3;
  describe(p: Plant, depth: number): string {
    return p.name + depth;
  }
  soak(depth: number): number {
    this.depth++;
    return depth;
  }
}
`,
	"src/use.ts": `import { label, tall, show, Bed, Plant } from "./label";

const rose: Plant = { name: "rose", height: 2 };
export const a = label(rose, rose.height);
export const t = tall(rose, 5);
export const s = show(rose, rose.height);
const bed = new Bed();
export const b = bed.describe(rose, bed.depth);
export const k = bed.soak(bed.depth);
`,
}

// value removes a parameter the body reads when every call passes that
// expression: the inverse of introduce_parameter; also takes it from
// functions that pass it on in the same step.
func TestRemoveParameterWithValue(t *testing.T) {
	remove := func(ws *workspace.Workspace, args string) *refactor.Result {
		t.Helper()
		res, err := refactor.Run(ws, NewRemove(), json.RawMessage(`{"file": "src/label.ts", `+args[1:]), refactor.OptionsOf[*workspace.Workspace]{})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	ws, dir := load(t, passed)
	if res := remove(ws, `{"name": "label", "parameter": "height", "value": "p.height", "also": ["show"]}`); !res.Applied {
		t.Fatalf("%s", res)
	}
	mustContain(t, dir, "src/label.ts", "export function label(p: Plant): string {\n  return p.name + \" \" + p.height;", "export function show(p: Plant): string {\n  return label(p) + \"!\";")
	mustContain(t, dir, "src/use.ts", "label(rose);", "show(rose);")
	if res := remove(ws, `{"name": "Bed.describe", "parameter": "depth", "value": "this.depth"}`); !res.Applied {
		t.Fatalf("%s", res)
	}
	mustContain(t, dir, "src/label.ts", "describe(p: Plant): string {\n    return p.name + this.depth;")
	mustContain(t, dir, "src/use.ts", "bed.describe(rose);")

	ws, _ = load(t, passed)
	for args, want := range map[string]string{
		`{"name": "label", "parameter": "height", "value": "p.height"}`:     "the call at label.ts:15 passes height, not p.height",
		`{"name": "tall", "parameter": "height", "value": "p.height"}`:      "the call at use.ts:5 passes 5, not rose.height",
		`{"name": "Bed.soak", "parameter": "depth", "value": "this.depth"}`: "the body of Bed.soak assigns this.depth (label.ts:24)",
		`{"name": "label", "parameter": "height", "value": "height + 0"}`:   "value height + 0 uses height itself",
		`{"name": "label", "parameter": "height", "also": ["show"]}`:        "also goes with value",
	} {
		if res := remove(ws, args); res.Applied || !strings.Contains(res.Refused, want) {
			t.Errorf("%s: want a refusal with %q, got %s", args, want, res)
		}
	}
}

var rates = map[string]string{
	"src/rate.ts": `export let rate = 2;
export const UNIT = "L";

export function cost(n: number): number {
  return n * rate;
}

export function label(n: number): string {
  return n + UNIT;
}

export function bump(): void {
  rate = 3;
}

export function spend(n: number): number {
  rate = n;
  return rate;
}
`,
	"src/use.ts": `import { cost, label, rate, UNIT } from "./rate";

export const c = cost(1);
export const l = label(2);
export const both = [rate, UNIT];
`,
}

// replace_variable makes a variable at a module's top level that the body
// reads a parameter, which takes its place; a call proven to pass what
// the body read is a refactoring, others are listed as edits.
func TestAddParameterReplacingAVariable(t *testing.T) {
	add := func(ws *workspace.Workspace, args string) *refactor.Result {
		t.Helper()
		res, err := refactor.Run(ws, NewAdd(), json.RawMessage(`{"file": "src/rate.ts", `+args[1:]), refactor.OptionsOf[*workspace.Workspace]{})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	ws, dir := load(t, rates)
	res := add(ws, `{"name": "label", "parameter": "UNIT", "type": "string", "value": "UNIT", "replace_variable": true}`)
	if !res.Applied || strings.Contains(res.String(), "note:") {
		t.Fatalf("want it applied without a note: %s", res)
	}
	mustContain(t, dir, "src/rate.ts", "export function label(n: number, UNIT: string): string {\n  return n + UNIT;")
	mustContain(t, dir, "src/use.ts", "label(2, UNIT)")
	res = add(ws, `{"name": "cost", "parameter": "rate", "type": "number", "value": "rate", "replace_variable": true}`)
	if !res.Applied || !strings.Contains(res.String(), "rate is assigned at rate.ts:13, so no call is proven to pass what the body read") {
		t.Fatalf("want it applied with a note: %s", res)
	}
	mustContain(t, dir, "src/use.ts", "cost(1, rate)")

	ws, _ = load(t, rates)
	res = add(ws, `{"name": "label", "parameter": "UNIT", "type": "string", "value": "\"kg\"", "replace_variable": true}`)
	if !res.Applied || !strings.Contains(res.String(), `every call passes "kg", not UNIT`) {
		t.Fatalf("want it applied with a note: %s", res)
	}
	for args, want := range map[string]string{
		`{"name": "spend", "parameter": "rate", "type": "number", "value": "rate", "replace_variable": true}`: "the body of spend assigns rate (rate.ts:17); a parameter would take that write from the variable",
		`{"name": "cost", "parameter": "rate", "type": "string", "value": "rate", "replace_variable": true}`:  "the parameter's type string is not the type number of the variable rate it would replace",
		`{"name": "cost", "parameter": "rate", "type": "number", "value": "rate"}`:                            "with replace_variable, the parameter takes its place",
	} {
		if res := add(ws, args); res.Applied || !strings.Contains(res.Refused, want) {
			t.Errorf("%s: want a refusal with %q, got %s", args, want, res)
		}
	}
}

// add_result, change_parameter_type and convert_params_to_object change
// a method's family with it, as the other signature changes do.
func TestMoreMethodFamilies(t *testing.T) {
	in := func(t *testing.T, ws *workspace.Workspace, r refactor.RefactoringOf[*workspace.Workspace], args string) *refactor.Result {
		t.Helper()
		res, err := refactor.Run(ws, r, json.RawMessage(`{"file": "src/hoses.ts", `+args[1:]), refactor.OptionsOf[*workspace.Workspace]{})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	t.Run("add_result", func(t *testing.T) {
		ws, dir := load(t, map[string]string{"src/hoses.ts": hoses})
		if res := in(t, ws, NewAddResult(), `{"name": "Hose.pour", "field": "ok", "type": "boolean", "value": "true"}`); !res.Applied {
			t.Fatalf("%s", res)
		}
		mustContain(t, dir, "src/hoses.ts",
			"  pour(bed: string, liters: number): { value: string; ok: boolean };\n",
			"  pour(bed: string, liters: number): { value: string; ok: boolean } {\n    return { value: bed + liters, ok: true };",
			"  pour(bed: string, amount: number): { value: string; ok: boolean } {\n    return { value: bed, ok: true };",
			"return { value: super.pour(bed, liters * 2).value, ok: true };",
			`w.pour("roses", 1).value + new Can().pour("ferns", 2).value + new Sprayer().pour("moss", 3).value`)
	})
	t.Run("change_parameter_type", func(t *testing.T) {
		ws, dir := load(t, map[string]string{"src/hoses.ts": hoses})
		if res := in(t, ws, NewChangeParameterType(), `{"name": "Waterer.pour", "parameter": "liters", "type": "(number)"}`); !res.Applied {
			t.Fatalf("%s", res)
		}
		mustContain(t, dir, "src/hoses.ts",
			"  pour(bed: string, liters: (number)): string;\n",
			"  pour(bed: string, liters: (number)): string {\n    return bed + liters;",
			"  pour(bed: string, amount: (number)): string {",
			"  pour(bed: string, liters: (number)): string {\n    return super.pour")
	})
	t.Run("convert_params_to_object", func(t *testing.T) {
		ws, dir := load(t, map[string]string{"src/hoses.ts": hoses})
		if res := in(t, ws, NewParamsToObject(), `{"name": "Hose.pour"}`); !res.Applied {
			t.Fatalf("%s", res)
		}
		mustContain(t, dir, "src/hoses.ts",
			"  pour({ bed, liters }: { bed: string; liters: number }): string;\n",
			"  pour({ bed, liters }: { bed: string; liters: number }): string {\n    return bed + liters;",
			"  pour({ bed, liters: amount }: { bed: string; liters: number }): string {\n    return bed;",
			"return super.pour({ bed, liters: liters * 2 });",
			`w.pour({ bed: "roses", liters: 1 })`)
	})
}
