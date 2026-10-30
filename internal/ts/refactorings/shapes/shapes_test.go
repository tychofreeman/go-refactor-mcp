package shapes

import (
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tstest"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

const config = `{"compilerOptions": {"strict": true, "module": "ESNext", "moduleResolution": "Bundler", "noEmit": true, "types": []}, "include": ["src"]}`

// load writes files under a new directory and loads it.
func load(t *testing.T, files map[string]string) (*workspace.Workspace, string) {
	t.Helper()
	dir := t.TempDir()
	files["tsconfig.json"] = config
	ws := tstest.Load(t, dir, files)
	return ws, dir
}

func run(t *testing.T, ws *workspace.Workspace, r refactor.RefactoringOf[*workspace.Workspace], args string) *refactor.Result {
	t.Helper()
	res := tstest.Run(t, ws, r, args)
	return res
}

func want(t *testing.T, dir, file, text string) {
	t.Helper()
	b := tstest.Read(t, dir, file)
	if string(b) != text {
		t.Errorf("%s:\n%s\nwant:\n%s", file, b, text)
	}
}

// change runs one refactoring on files and checks the files after, or,
// with refused set, the refusal.
type change struct {
	name    string
	files   map[string]string
	r       refactor.RefactoringOf[*workspace.Workspace]
	args    string
	out     map[string]string
	refused string
}

func check(t *testing.T, cases []change) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ws, dir := load(t, tc.files)
			res := run(t, ws, tc.r, tc.args)
			if tc.refused != "" {
				if res.Applied || !strings.Contains(res.Refused, tc.refused) {
					t.Fatalf("want a refusal with %q, got %s", tc.refused, res)
				}
				return
			}
			if !res.Applied {
				t.Fatalf("%s", res)
			}
			for f, text := range tc.out {
				want(t, dir, f, text)
			}
		})
	}
}

const plant = `export interface Plant {
  name: string;
  height: number;
}
`

func TestFold(t *testing.T) {
	check(t, []change{
		{"expression", map[string]string{"src/p.ts": plant, "src/tall.ts": `import { Plant } from "./p";

export function tall(p: Plant, margin: number): number {
  return p.height * 2 + margin;
}

export const a = (q: Plant) => q.height * 2 + 1;
`, "src/use.ts": `import { Plant } from "./p";

export function b(r: Plant): number {
  return r.height * 2 + r.height;
}
`}, NewFold(), `{"file": "src/tall.ts", "name": "tall"}`, map[string]string{"src/tall.ts": `import { Plant } from "./p";

export function tall(p: Plant, margin: number): number {
  return p.height * 2 + margin;
}

export const a = (q: Plant) => tall(q, 1);
`, "src/use.ts": `import { Plant } from "./p";
import { tall } from "./tall";

export function b(r: Plant): number {
  return tall(r, r.height);
}
`}, ""},
		{"statements", map[string]string{"src/log.ts": `export const lines: string[] = [];

function note(bed: string): void {
  lines.push("water " + bed);
  lines.push("done");
}

export function morning(): void {
  lines.push("start");
  lines.push("water " + "roses");
  lines.push("done");
}
`}, NewFold(), `{"file": "src/log.ts", "name": "note"}`, map[string]string{"src/log.ts": `export const lines: string[] = [];

function note(bed: string): void {
  lines.push("water " + bed);
  lines.push("done");
}

export function morning(): void {
  lines.push("start");
  note("roses");
}
`}, ""},
		{"method", map[string]string{"src/bed.ts": `export class Bed {
  depth = 3;
  private twice(): number {
    return this.depth * 2;
  }
  grow(): number {
    return this.depth * 2 + 1;
  }
}
`}, NewFold(), `{"file": "src/bed.ts", "name": "Bed.twice"}`, map[string]string{"src/bed.ts": `export class Bed {
  depth = 3;
  private twice(): number {
    return this.depth * 2;
  }
  grow(): number {
    return this.twice() + 1;
  }
}
`}, ""},
		{"order", map[string]string{"src/n.ts": `let n = 0;
function next(): number {
  return n++;
}

function sum(a: number, b: number): number {
  return b + a;
}

export const x = next() + next();
`}, NewFold(), `{"file": "src/n.ts", "name": "sum"}`, nil, "no match of sum's body could become a call"},
		{"nothing", map[string]string{"src/n.ts": `export function sum(a: number, b: number): number {
  return a * b;
}
`}, NewFold(), `{"file": "src/n.ts", "name": "sum"}`, nil, "no code matches the body of sum"},
	})
}

func TestParameterize(t *testing.T) {
	check(t, []change{
		{"two", map[string]string{"src/log.ts": `export const lines: string[] = [];

export function mist(bed: string): void {
  lines.push("mist on " + bed);
}

export function drip(row: string): void {
  lines.push("drip on " + row);
}
`}, NewParameterize(), `{"file": "src/log.ts", "functions": ["mist", "drip"], "name": "on", "parameters": ["what"]}`, map[string]string{"src/log.ts": `export const lines: string[] = [];

export function mist(bed: string): void {
  on(bed, "mist on ");
}

export function drip(row: string): void {
  on(row, "drip on ");
}

function on(bed: string, what: string): void {
  lines.push(what + bed);
}
`}, ""},
		{"more than literals", map[string]string{"src/log.ts": `export function a(x: number): number {
  return x + 1;
}

export function b(x: number): number {
  return x - 1;
}
`}, NewParameterize(), `{"file": "src/log.ts", "functions": ["a", "b"], "name": "c", "parameters": ["k"]}`, nil, "differ in more than literals"},
		{"count", map[string]string{"src/log.ts": `export function a(x: number): number {
  return x * 2 + 1;
}

export function b(x: number): number {
  return x * 3 + 4;
}
`}, NewParameterize(), `{"file": "src/log.ts", "functions": ["a", "b"], "name": "c", "parameters": ["k"]}`, nil, "differ in 2 literals (2, 1 in a)"},
	})
}

func TestMergeDuplicate(t *testing.T) {
	check(t, []change{
		{"across files", map[string]string{"src/a.ts": `export function trim(s: string): string {
  return s.trim();
}

export const x = trim(" a ");
`, "src/b.ts": `export function clean(t: string): string {
  return t.trim();
}
`, "src/c.ts": `import { trim } from "./a";

export const y = trim(" b ");
`}, NewMergeDuplicate(), `{"file": "src/a.ts", "name": "trim", "keep_file": "src/b.ts", "keep_name": "clean"}`, map[string]string{"src/a.ts": `import { clean } from "./b";

export const x = clean(" a ");
`, "src/c.ts": `import { clean } from "./b";

export const y = clean(" b ");
`}, ""},
		{"differ", map[string]string{"src/a.ts": `export function up(s: string): string {
  return s.toUpperCase();
}

export function down(t: string): string {
  return t.toLowerCase();
}
`}, NewMergeDuplicate(), `{"file": "src/a.ts", "name": "up", "keep_file": "src/a.ts", "keep_name": "down"}`, nil, "the bodies differ: toLowerCase at a.ts:6 differs from toUpperCase"},
		{"signature", map[string]string{"src/a.ts": `export function one(s: string): number {
  return 1;
}

export function uno(n: number): number {
  return 1;
}
`}, NewMergeDuplicate(), `{"file": "src/a.ts", "name": "one", "keep_file": "src/a.ts", "keep_name": "uno"}`, nil, "different signatures"},
	})
}

func TestRemoveDeadCode(t *testing.T) {
	check(t, []change{
		{"function and its import", map[string]string{"src/p.ts": plant, "src/old.ts": `import { Plant } from "./p";

// oldLabel is no longer used.
function oldLabel(p: Plant): string {
  return p.name;
}

export const n = 1;
`}, NewRemoveDeadCode(), `{"file": "src/old.ts", "name": "oldLabel"}`, map[string]string{"src/old.ts": `export const n = 1;
`}, ""},
		{"member", map[string]string{"src/bed.ts": `export class Bed {
  depth = 3;
  colour = "brown";
  deep(): boolean {
    return this.depth > 2;
  }
}
`}, NewRemoveDeadCode(), `{"file": "src/bed.ts", "name": "Bed.colour"}`, map[string]string{"src/bed.ts": `export class Bed {
  depth = 3;
  deep(): boolean {
    return this.depth > 2;
  }
}
`}, ""},
		{"used", map[string]string{"src/a.ts": `function f(): number {
  return 1;
}

export const x = f();
`}, NewRemoveDeadCode(), `{"file": "src/a.ts", "name": "f"}`, nil, "f is still used at a.ts:5"},
		{"each other", map[string]string{"src/a.ts": `function f(n: number): number {
  return n > 0 ? g(n - 1) : 0;
}

function g(n: number): number {
  return f(n);
}

export const x = 1;
`}, NewRemoveDeadCode(), `{"file": "src/a.ts", "name": "f", "also": ["g"]}`, map[string]string{"src/a.ts": `export const x = 1;
`}, ""},
		{"effects", map[string]string{"src/a.ts": `let n = 0;
function next(): number {
  return n++;
}

const started = next();
export const x = n;
`}, NewRemoveDeadCode(), `{"file": "src/a.ts", "name": "started"}`, nil, "would no longer be evaluated"},
	})
}

func TestExtractInterface(t *testing.T) {
	check(t, []change{
		{"interface", map[string]string{"src/bed.ts": `export interface Bed {
  depth: number;
  width: number;
  name: string;
}
`}, NewExtractInterface(), `{"file": "src/bed.ts", "name": "Bed", "fields": ["depth", "width"], "new_type": "Size"}`, map[string]string{"src/bed.ts": `export interface Size {
  depth: number;
  width: number;
}

export interface Bed extends Size {
  name: string;
}
`}, ""},
		{"type alias", map[string]string{"src/bed.ts": `export type Bed = {
  depth: number;
  name: string;
};
`}, NewExtractInterface(), `{"file": "src/bed.ts", "name": "Bed", "fields": ["depth"], "new_type": "Size"}`, map[string]string{"src/bed.ts": `export interface Size {
  depth: number;
}

export type Bed = Size & {
  name: string;
};
`}, ""},
		{"class", map[string]string{"src/s.ts": `export class Sprinkler {
  minutes = 5;
  private log: string[] = [];
  spray(bed: string, times = 1): string {
    return bed + times;
  }
  get on(): boolean {
    return this.minutes > 0;
  }
}
`}, NewExtractInterface(), `{"file": "src/s.ts", "name": "Sprinkler", "fields": ["minutes", "spray", "on"], "new_type": "Watering"}`, map[string]string{"src/s.ts": `export interface Watering {
  minutes: number;
  spray(bed: string, times?: number): string;
  readonly on: boolean;
}

export class Sprinkler implements Watering {
  minutes = 5;
  private log: string[] = [];
  spray(bed: string, times = 1): string {
    return bed + times;
  }
  get on(): boolean {
    return this.minutes > 0;
  }
}
`}, ""},
		{"private", map[string]string{"src/s.ts": `export class Sprinkler {
  private log: string[] = [];
}
`}, NewExtractInterface(), `{"file": "src/s.ts", "name": "Sprinkler", "fields": ["log"], "new_type": "W"}`, nil, "private, protected or static"},
	})
}

const ferns = `export class Fern {
  readonly kind = "fern";
  mist = 2;
}

export class Cactus {
  readonly kind = "cactus";
}

export type Plant = Fern | Cactus;
`

func TestPrepareSwitchForTypes(t *testing.T) {
	plant := `export interface Plant {
  name: string;
  species: unknown;
}
`
	check(t, []change{
		{"returning", map[string]string{"src/water.ts": plant + `
export function water(p: Plant, days: number): number {
  switch (p.species) {
    case "fern":
      return days * 3;
    case "moss":
    case "cactus":
      return 1;
  }
  return 0;
}
`}, NewPrepareSwitchForTypes(), `{"file": "src/water.ts", "function": "water", "interface": "Species", "method": "water"}`, map[string]string{"src/water.ts": plant + `
export interface Species {
  water(days: number): number;
}

export function water(p: Plant, days: number): number {
  if (typeof p.species === "object" && p.species !== null && "water" in p.species) {
    return (p.species as Species).water(days);
  }
  switch (p.species) {
    case "fern":
      return days * 3;
    case "moss":
      return 1;
    case "cactus":
      return 1;
  }
  return 0;
}
`}, ""},
		{"in the else", map[string]string{"src/water.ts": plant + `
export interface Species {
  water(days: number): number;
}

export function label(p: Plant, out: string[]): void {
  switch (p.species) {
    case "fern":
      out.push("green");
      break;
    default:
      out.push("?");
  }
}
`}, NewPrepareSwitchForTypes(), `{"file": "src/water.ts", "function": "label", "interface": "Species", "method": "label"}`, map[string]string{"src/water.ts": plant + `
export interface Species {
  water(days: number): number;
  label(out: string[]): void;
}

export function label(p: Plant, out: string[]): void {
  if (typeof p.species === "object" && p.species !== null && "label" in p.species) {
    (p.species as Species).label(out);
  } else {
    switch (p.species) {
      case "fern":
        out.push("green");
        break;
      default:
        out.push("?");
    }
  }
}
`}, ""},
		{"no object", map[string]string{"src/water.ts": `export function water(species: string): number {
  switch (species) {
    case "fern":
      return 3;
  }
  return 0;
}
`}, NewPrepareSwitchForTypes(), `{"file": "src/water.ts", "function": "water", "interface": "Species", "method": "water"}`, nil, "holds no object; widen it first"},
		{"some return", map[string]string{"src/water.ts": plant + `
export function water(p: Plant, out: string[]): void {
  switch (p.species) {
    case "fern":
      return;
    default:
      out.push("?");
  }
}
`}, NewPrepareSwitchForTypes(), `{"file": "src/water.ts", "function": "water", "interface": "Species", "method": "water"}`, nil, "some cases return and some do not"},
	})
}

// The subclasses of an abstract class take a union's place; the base
// declares the method.
func TestUnionSwitchToMethodOnABaseClass(t *testing.T) {
	ws, dir := load(t, map[string]string{
		"src/plants.ts": "export abstract class Plant {\n  abstract readonly kind: string;\n}\n\nexport class Fern extends Plant {\n  readonly kind = \"fern\";\n  mist = 2;\n}\n\nexport class Cactus extends Plant {\n  readonly kind = \"cactus\";\n}\n",
		"src/water.ts":  "import { Plant, Fern, Cactus } from \"./plants\";\n\nexport function water(p: Plant, out: number[]): void {\n  if (p instanceof Fern) {\n    out.push(p.mist);\n  } else if (p instanceof Cactus) {\n    out.push(1);\n  }\n}\n",
	})
	res := run(t, ws, NewUnionSwitchToMethod(), `{"file": "src/water.ts", "function": "water", "method": "water"}`)
	if !res.Applied {
		t.Fatalf("%s", res)
	}
	want(t, dir, "src/plants.ts", "export abstract class Plant {\n  abstract readonly kind: string;\n  abstract water(out: number[]): void;\n}\n\nexport class Fern extends Plant {\n  readonly kind = \"fern\";\n  mist = 2;\n\n  water(out: number[]): void {\n    out.push(this.mist);\n  }\n}\n\nexport class Cactus extends Plant {\n  readonly kind = \"cactus\";\n\n  water(out: number[]): void {\n    out.push(1);\n  }\n}\n")
}

// A template's string parameter stands for constant text, and a +
// chain's for a run of operands, as Go's %s and concatenation do.
func TestFoldTextAndConcatenation(t *testing.T) {
	ws, dir := load(t, map[string]string{"src/s.ts": "const logged: string[] = [];\n\nfunction log(line: string): void {\n  logged.push(line);\n}\n\nfunction record(entries: string[], device: string, bed: string, text: string): void {\n  log(`${device} on ${bed}`);\n  entries.push(bed + \": \" + text);\n}\n\nexport class Sprinkler {\n  entries: string[] = [];\n\n  run(bed: string, note: string): void {\n    log(`sprinkler on ${bed}`);\n    this.entries.push(bed + \": \" + note + \"!\");\n  }\n}\n"})
	res := run(t, ws, NewFold(), `{"file": "src/s.ts", "name": "record"}`)
	if !res.Applied {
		t.Fatalf("%s", res)
	}
	want(t, dir, "src/s.ts", "const logged: string[] = [];\n\nfunction log(line: string): void {\n  logged.push(line);\n}\n\nfunction record(entries: string[], device: string, bed: string, text: string): void {\n  log(`${device} on ${bed}`);\n  entries.push(bed + \": \" + text);\n}\n\nexport class Sprinkler {\n  entries: string[] = [];\n\n  run(bed: string, note: string): void {\n    record(this.entries, \"sprinkler\", bed, note + \"!\");\n  }\n}\n")
}

func TestUnionSwitchToMethod(t *testing.T) {
	check(t, []change{
		{"switch", map[string]string{"src/plants.ts": ferns, "src/water.ts": `import { Plant } from "./plants";

export function water(p: Plant, days: number): number {
  switch (p.kind) {
    case "fern":
      return days * p.mist;
    case "cactus":
      return 1;
  }
}
`}, NewUnionSwitchToMethod(), `{"file": "src/water.ts", "function": "water", "method": "water"}`, map[string]string{"src/water.ts": `import { Plant } from "./plants";

export function water(p: Plant, days: number): number {
  return p.water(days);
}
`, "src/plants.ts": `export class Fern {
  readonly kind = "fern";
  mist = 2;

  water(days: number): number {
    return days * this.mist;
  }
}

export class Cactus {
  readonly kind = "cactus";

  water(days: number): number {
    return 1;
  }
}

export type Plant = Fern | Cactus;
`}, ""},
		{"instanceof", map[string]string{"src/plants.ts": ferns, "src/label.ts": `import { Plant, Fern, Cactus } from "./plants";

export const labels: string[] = [];

export function label(p: Plant): void {
  if (p instanceof Fern) {
    labels.push("fern " + p.mist);
  } else if (p instanceof Cactus) {
    labels.push("cactus");
  }
}
`}, NewUnionSwitchToMethod(), `{"file": "src/label.ts", "line": 6, "method": "label"}`, nil, "labels, which the body at label.ts:6 uses, is not seen in Fern"},
		{"instanceof returning", map[string]string{"src/plants.ts": ferns, "src/label.ts": `import { Plant, Fern, Cactus } from "./plants";

export function label(p: Plant): string {
  if (p instanceof Fern) {
    return "fern " + p.mist;
  } else if (p instanceof Cactus) {
    return "cactus";
  }
  throw new Error("no label");
}
`}, NewUnionSwitchToMethod(), `{"file": "src/label.ts", "line": 4, "method": "label"}`, map[string]string{"src/label.ts": `import { Plant, Fern, Cactus } from "./plants";

export function label(p: Plant): string {
  return p.label();
  throw new Error("no label");
}
`}, ""},
		{"missing case", map[string]string{"src/plants.ts": ferns, "src/water.ts": `import { Plant } from "./plants";

export function water(p: Plant): number {
  switch (p.kind) {
    case "fern":
      return 2;
  }
  return 0;
}
`}, NewUnionSwitchToMethod(), `{"file": "src/water.ts", "line": 4, "method": "water"}`, nil, "Cactus has no case"},
	})
}

// A parameter the body uses only by its properties matches what stands
// for each, and the call passes an object of them; a setter, { ...p, f: v },
// matches an object literal of p's type that ends with f.
func TestFoldObjectsAndSetters(t *testing.T) {
	check(t, []change{
		{"parameter object", map[string]string{"src/feed.ts": `export function feed(bed: string, plants: number, ml: number): string {
  return bed + plants * ml;
}

export interface Dose {
  plants: number;
  ml: number;
}

export function feed2(bed: string, d: Dose): string {
  return feed(bed, d.plants, d.ml);
}

export const a = feed("roses", 3, 20);

export function b(n: number): string {
  return feed("ferns", n, n + 1);
}
`}, NewFold(), `{"file": "src/feed.ts", "name": "feed2"}`, map[string]string{"src/feed.ts": `export function feed(bed: string, plants: number, ml: number): string {
  return bed + plants * ml;
}

export interface Dose {
  plants: number;
  ml: number;
}

export function feed2(bed: string, d: Dose): string {
  return feed(bed, d.plants, d.ml);
}

export const a = feed2("roses", { plants: 3, ml: 20 });

export function b(n: number): string {
  return feed2("ferns", { plants: n, ml: n + 1 });
}
`}, ""},
		{"setter", map[string]string{"src/p.ts": `export interface Plant {
  name: string;
  height?: number;
}

export function withHeight(p: Plant, h: number): Plant {
  return { ...p, height: h };
}

export function rose(n: string): Plant {
  return { name: n, height: 3 };
}

export const fern: Plant = { name: "fern", height: 1 + 1 };
export const moss: Plant = { height: 0, name: "moss" };
export const other = { name: "x", height: 2 };
`}, NewFold(), `{"file": "src/p.ts", "name": "withHeight"}`, map[string]string{"src/p.ts": `export interface Plant {
  name: string;
  height?: number;
}

export function withHeight(p: Plant, h: number): Plant {
  return { ...p, height: h };
}

export function rose(n: string): Plant {
  return withHeight({ name: n }, 3);
}

export const fern: Plant = withHeight({ name: "fern" }, 1 + 1);
export const moss: Plant = { height: 0, name: "moss" };
export const other = { name: "x", height: 2 };
`}, ""},
	})
}
