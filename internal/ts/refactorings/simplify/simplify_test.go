package simplify

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

const config = `{"compilerOptions": {"strict": true, "module": "ESNext", "moduleResolution": "Bundler", "noEmit": true, "types": []}, "include": ["src"]}`

// load writes files under a new directory and loads it.
func load(t *testing.T, files map[string]string) (*workspace.Workspace, string) {
	t.Helper()
	dir := t.TempDir()
	files["tsconfig.json"] = config
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
	res, err := refactor.Run(ws, r, json.RawMessage(args), refactor.OptionsOf[*workspace.Workspace]{})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// want checks that the file now reads exactly text.
func want(t *testing.T, dir, file, text string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != text {
		t.Errorf("%s:\n%s\nwant:\n%s", file, b, text)
	}
}

// change runs one refactoring on a file holding src and checks that it
// then holds out; with out empty, that it refuses with a reason holding
// refused.
type change struct {
	name    string
	src     string
	r       refactor.RefactoringOf[*workspace.Workspace]
	args    string
	out     string
	refused string
}

func check(t *testing.T, cases []change) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ws, dir := load(t, map[string]string{"src/g.ts": tc.src})
			res := run(t, ws, tc.r, `{"file": "src/g.ts", `+tc.args[1:])
			if tc.out == "" {
				if res.Applied || !strings.Contains(res.Refused, tc.refused) {
					t.Fatalf("want a refusal with %q, got %s", tc.refused, res)
				}
				return
			}
			if !res.Applied {
				t.Fatalf("%s", res)
			}
			want(t, dir, "src/g.ts", tc.out)
		})
	}
}

const plants = `// A garden.
export interface Plant {
  name: string;
  height: number;
}

let watered = 0;

export function water(): number {
  watered++;
  return watered;
}
`

func TestExtractVariable(t *testing.T) {
	check(t, []change{
		{"plain", plants + `
export function tall(p: Plant, margin: number): number {
  return p.height * 2 + margin;
}
`, NewExtractVariable(), `{"line": 15, "expression": "p.height * 2", "name": "twice"}`, plants + `
export function tall(p: Plant, margin: number): number {
  const twice = p.height * 2;
  return twice + margin;
}
`, ""},
		{"all occurrences", plants + `
export function tall(p: Plant): number {
  const a = p.height * 2;
  const b = p.height * 2 + 1;
  return a + b;
}
`, NewExtractVariable(), `{"line": 15, "expression": "p.height * 2", "name": "twice", "all_occurrences": true}`, plants + `
export function tall(p: Plant): number {
  const twice = p.height * 2;
  const a = twice;
  const b = twice + 1;
  return a + b;
}
`, ""},
		{"not past a call", plants + `
export function tall(p: Plant): number {
  const a = p.height * 2;
  water();
  const b = p.height * 2 + 1;
  return a + b;
}
`, NewExtractVariable(), `{"line": 15, "expression": "p.height * 2", "name": "twice", "all_occurrences": true}`, plants + `
export function tall(p: Plant): number {
  const twice = p.height * 2;
  const a = twice;
  water();
  const b = p.height * 2 + 1;
  return a + b;
}
`, ""},
		{"shorthand", plants + `
export function named(name: string): Plant {
  return { name, height: 1 };
}
`, NewExtractVariable(), `{"line": 15, "expression": "name", "name": "n"}`, plants + `
export function named(name: string): Plant {
  const n = name;
  return { name: n, height: 1 };
}
`, ""},
		{"after a call", plants + `
export function next(p: Plant): number {
  return water() + p.height;
}
`, NewExtractVariable(), `{"line": 15, "expression": "p.height", "name": "h"}`, "", "the call water() at g.ts:15 may change state, and p.height at g.ts:15 reads state"},
		{"conditional", plants + `
export function next(p: Plant | undefined): number {
  return p !== undefined && water() > 0 ? 1 : 0;
}
`, NewExtractVariable(), `{"line": 15, "expression": "water()", "name": "w"}`, "", "would then not always be evaluated"},
		{"assigned", plants + `
export function grow(p: Plant): void {
  p.height = 3;
}
`, NewExtractVariable(), `{"line": 15, "expression": "p.height", "name": "h"}`, "", "is assigned to"},
		{"method", plants + `
export function shout(p: Plant): string {
  return p.name.toUpperCase();
}
`, NewExtractVariable(), `{"line": 15, "expression": "p.name.toUpperCase", "name": "up"}`, "", "lose its this"},
		{"arrow body", plants + `
export const h = (p: Plant) => p.height;
`, NewExtractVariable(), `{"line": 14, "expression": "p.height", "name": "x"}`, "", "in no statement of a block"},
		{"loop condition", plants + `
export function drain(p: Plant): void {
  while (p.height > 0) {
    p.height--;
  }
}
`, NewExtractVariable(), `{"line": 15, "expression": "p.height > 0", "name": "high"}`, "", "may change state, and p.height at g.ts:15 reads state"},
		{"taken", plants + `
export function tall(p: Plant, margin: number): number {
  return p.height + margin;
}
`, NewExtractVariable(), `{"line": 15, "expression": "p.height", "name": "margin"}`, "", "margin is already a name"},
	})
}

func TestInlineVariable(t *testing.T) {
	check(t, []change{
		{"one use", plants + `
export function tall(p: Plant, margin: number): number {
  const twice = p.height * 2;
  return twice + margin;
}
`, NewInlineVariable(), `{"line": 15, "name": "twice"}`, plants + `
export function tall(p: Plant, margin: number): number {
  return p.height * 2 + margin;
}
`, ""},
		{"parentheses", plants + `
export function tall(p: Plant): number {
  const sum = p.height + 1;
  return sum * 2;
}
`, NewInlineVariable(), `{"function": "tall", "name": "sum"}`, plants + `
export function tall(p: Plant): number {
  return (p.height + 1) * 2;
}
`, ""},
		{"several uses of a call", plants + `
export function twice(): number {
  const w = water();
  return w + w;
}
`, NewInlineVariable(), `{"line": 15, "name": "w"}`, "", "would be evaluated another number of times"},
		{"past a write", plants + `
export function grow(p: Plant): number {
  const h = p.height;
  p.height = 3;
  return h;
}
`, NewInlineVariable(), `{"line": 15, "name": "h"}`, "", "cannot move past what runs between"},
		{"assigned", plants + `
export function grow(): number {
  let h = 1;
  h = 2;
  return h;
}
`, NewInlineVariable(), `{"line": 15, "name": "h"}`, "", "is assigned at"},
		{"one of several", plants + `
export function pair(): number {
  const a = 1, b = 2;
  return a + b;
}
`, NewInlineVariable(), `{"line": 15, "name": "a"}`, plants + `
export function pair(): number {
  const b = 2;
  return 1 + b;
}
`, ""},
		{"constant in a closure", plants + `
export function later(): () => number {
  const one = 1;
  return () => one + 1;
}
`, NewInlineVariable(), `{"line": 15, "name": "one"}`, plants + `
export function later(): () => number {
  return () => 1 + 1;
}
`, ""},
		{"used in a loop", plants + `
export function sum(p: Plant, xs: number[]): number {
  const h = p.height;
  let total = 0;
  for (const x of xs) {
    total += x * h;
  }
  return total;
}
`, NewInlineVariable(), `{"line": 15, "name": "h"}`, plants + `
export function sum(p: Plant, xs: number[]): number {
  let total = 0;
  for (const x of xs) {
    total += x * p.height;
  }
  return total;
}
`, ""},
		{"read in a closure", plants + `
export function later(p: Plant): () => number {
  const h = p.height;
  return () => h;
}
`, NewInlineVariable(), `{"line": 15, "name": "h"}`, "", "runs at another time"},
	})
}

func TestMoveStatement(t *testing.T) {
	check(t, []change{
		{"down", plants + `
export function count(beds: Plant[]): number {
  // first
  const n = beds.length;
  const m = 2;
  return n + m;
}
`, NewMoveDown(), `{"line": 16}`, plants + `
export function count(beds: Plant[]): number {
  const m = 2;
  // first
  const n = beds.length;
  return n + m;
}
`, ""},
		{"up past a use", plants + `
export function count(beds: Plant[]): number {
  const n = beds.length;
  const m = n + 2;
  return m;
}
`, NewMoveUp(), `{"line": 16}`, "", "uses n, which const n = beds.length; at g.ts:15 declares"},
		{"two calls", plants + `
export function twice(): void {
  water();
  water();
}
`, NewMoveDown(), `{"line": 15}`, "", "may change state, and the call water() at g.ts:16 may change state"},
		{"past a return", plants + `
export function stop(p: Plant): number {
  if (p.height > 2) {
    return 1;
  }
  water();
  return 0;
}
`, NewMoveUp(), `{"line": 18}`, "", "leaves"},
		{"first", plants + `
export function one(): number {
  return 1;
}
`, NewMoveUp(), `{"line": 15}`, "", "is the first statement of its block"},
	})
}

func TestInvertIfAndRemoveElse(t *testing.T) {
	check(t, []change{
		{"else that returns", plants + `
export function plant(p: Plant, wet: boolean): number {
  if (wet) {
    water();
    return p.height;
  } else {
    return 0;
  }
}
`, NewInvertIf(), `{"line": 15}`, plants + `
export function plant(p: Plant, wet: boolean): number {
  if (!wet) {
    return 0;
  }
  water();
  return p.height;
}
`, ""},
		{"keep else", plants + `
export function pick(p: Plant): string {
  if (p.name === "fern" && p.height < 2) {
    return "a";
  } else {
    return "b";
  }
}
`, NewInvertIf(), `{"line": 15, "keep_else": true}`, plants + `
export function pick(p: Plant): string {
  if (p.name !== "fern" || !(p.height < 2)) {
    return "b";
  } else {
    return "a";
  }
}
`, ""},
		{"guard in a loop", plants + `
export function total(xs: number[]): number {
  let sum = 0;
  for (const x of xs) {
    if (x > 0) {
      sum += x;
    }
  }
  return sum;
}
`, NewInvertIf(), `{"line": 17}`, plants + `
export function total(xs: number[]): number {
  let sum = 0;
  for (const x of xs) {
    if (!(x > 0)) {
      continue;
    }
    sum += x;
  }
  return sum;
}
`, ""},
		{"negation dropped", plants + `
export function f(done: boolean): number {
  if (!done) {
    water();
  } else {
    return 0;
  }
  return 1;
}
`, NewInvertIf(), `{"line": 15}`, plants + `
export function f(done: boolean): number {
  if (done) {
    return 0;
  }
  water();
  return 1;
}
`, ""},
		{"no else in the middle", plants + `
export function f(x: number): number {
  if (x > 0) {
    water();
  }
  return x;
}
`, NewInvertIf(), `{"line": 15}`, "", "an if without else is inverted only"},
		{"remove else", plants + `
export function depth(p: Plant | undefined): number {
  if (!p) {
    return 0;
  } else {
    const h = p.height;
    return h * 2;
  }
}
`, NewRemoveElse(), `{"line": 15}`, plants + `
export function depth(p: Plant | undefined): number {
  if (!p) {
    return 0;
  }
  const h = p.height;
  return h * 2;
}
`, ""},
		{"remove else-if", plants + `
export function size(x: number): string {
  if (x > 9) {
    return "big";
  } else if (x > 3) {
    return "mid";
  }
  return "small";
}
`, NewRemoveElse(), `{"line": 15}`, plants + `
export function size(x: number): string {
  if (x > 9) {
    return "big";
  }
  if (x > 3) {
    return "mid";
  }
  return "small";
}
`, ""},
		{"falls through", plants + `
export function f(x: number): number {
  if (x > 0) {
    water();
  } else {
    return 1;
  }
  return 0;
}
`, NewRemoveElse(), `{"line": 15}`, "", "use invert_if"},
		{"clash", plants + `
export function f(x: number, h: number): number {
  if (x > 0) {
    return 1;
  } else {
    const h = 2;
    return h;
  }
}
`, NewRemoveElse(), `{"line": 15}`, "", "h, declared at g.ts:18, would then be seen"},
	})
}

func TestExtractFunction(t *testing.T) {
	check(t, []change{
		{"result", plants + `
export function report(beds: Plant[]): string {
  let total = 0;
  for (const b of beds) {
    total += b.height;
  }
  return "total " + total;
}
`, NewExtractFunction(), `{"start_line": 15, "end_line": 18, "name": "sum"}`, plants + `
export function report(beds: Plant[]): string {
  let total = sum(beds);
  return "total " + total;
}

function sum(beds: Plant[]) {
  let total = 0;
  for (const b of beds) {
    total += b.height;
  }
  return total;
}
`, ""},
		{"assigned", plants + `
export function grow(p: Plant): number {
  let h = p.height;
  h = h * 2;
  h = h + 1;
  return h;
}
`, NewExtractFunction(), `{"start_line": 16, "end_line": 17, "name": "bump", "doc": "bump doubles h and adds one."}`, plants + `
export function grow(p: Plant): number {
  let h = p.height;
  h = bump(h);
  return h;
}

/** bump doubles h and adds one. */
function bump(h: number) {
  h = h * 2;
  h = h + 1;
  return h;
}
`, ""},
		{"several results", plants + `
export function both(p: Plant): number {
  const a = p.height;
  const b = a * 2;
  return a + b;
}
`, NewExtractFunction(), `{"start_line": 15, "end_line": 16, "name": "pair"}`, plants + `
export function both(p: Plant): number {
  const { a, b } = pair(p);
  return a + b;
}

function pair(p: Plant) {
  const a = p.height;
  const b = a * 2;
  return { a, b };
}
`, ""},
		{"returns at the end", plants + `
export function pick(p: Plant): string {
  water();
  if (p.height > 1) {
    return "tall";
  }
  return "short";
}
`, NewExtractFunction(), `{"start_line": 16, "end_line": 19, "name": "size"}`, plants + `
export function pick(p: Plant): string {
  water();
  return size(p);
}

function size(p: Plant) {
  if (p.height > 1) {
    return "tall";
  }
  return "short";
}
`, ""},
		{"method", plants + `
export class Bed {
  depth = 3;
  plant(p: Plant): number {
    const d = this.depth * 2;
    return d + p.height;
  }
}
`, NewExtractFunction(), `{"start_line": 17, "end_line": 17, "name": "twice"}`, plants + `
export class Bed {
  depth = 3;
  plant(p: Plant): number {
    const d = this.twice();
    return d + p.height;
  }

  private twice() {
    const d = this.depth * 2;
    return d;
  }
}
`, ""},
		{"async", plants + `
export async function load(p: Promise<Plant>): Promise<number> {
  const got = await p;
  return got.height;
}
`, NewExtractFunction(), `{"start_line": 15, "end_line": 15, "name": "get"}`, plants + `
export async function load(p: Promise<Plant>): Promise<number> {
  const got = await get(p);
  return got.height;
}

async function get(p: Promise<Plant>) {
  const got = await p;
  return got;
}
`, ""},
		{"early return", plants + `
export function pick(p: Plant): string {
  if (p.height > 1) {
    return "tall";
  }
  water();
  return "short";
}
`, NewExtractFunction(), `{"start_line": 15, "end_line": 17, "name": "size"}`, "", "only lines that end its body may return"},
		{"break out", plants + `
export function first(xs: number[]): number {
  for (const x of xs) {
    if (x > 0) {
      break;
    }
  }
  return 0;
}
`, NewExtractFunction(), `{"start_line": 16, "end_line": 18, "name": "f"}`, "", "jumps out of the lines"},
		{"mixed results", plants + `
export function mix(p: Plant): number {
  let h = 1;
  h = 2;
  const k = 3;
  return h + k;
}
`, NewExtractFunction(), `{"start_line": 16, "end_line": 17, "name": "f"}`, "", "declare k and assign h"},
		{"top level", plants + `
export const x = water();
`, NewExtractFunction(), `{"start_line": 14, "end_line": 14, "name": "f"}`, "", "the module's top level"},
	})
}

func TestInlineFunction(t *testing.T) {
	area := plants + `
function area(p: Plant): number {
  return p.height * p.height;
}
`
	check(t, []change{
		{"expression", area + `
export function size(p: Plant): number {
  return area(p) + 1;
}
`, NewInlineFunction(), `{"line": 19, "name": "area"}`, plants + `
export function size(p: Plant): number {
  return p.height * p.height + 1;
}
`, ""},
		{"argument declared", area + `
export function size(): number {
  return area({ name: "fern", height: water() });
}
`, NewInlineFunction(), `{"line": 19, "name": "area"}`, plants + `
export function size(): number {
  const p: Plant = { name: "fern", height: water() };
  return p.height * p.height;
}
`, ""},
		{"statements", plants + `
function grow(p: Plant, by: number): number {
  const h = p.height + by;
  water();
  return h * 2;
}

export function use(p: Plant): number {
  const n = grow(p, 1);
  return n;
}
`, NewInlineFunction(), `{"line": 21, "name": "grow"}`, plants + `
export function use(p: Plant): number {
  const h = p.height + 1;
  water();
  const n = h * 2;
  return n;
}
`, ""},
		{"all calls", area + `
export function both(a: Plant, b: Plant): number {
  return area(a) + area(b);
}
`, NewInlineFunction(), `{"name": "area", "all_calls": true}`, plants + `
export function both(a: Plant, b: Plant): number {
  return a.height * a.height + b.height * b.height;
}
`, ""},
		{"keep", area + `
export function size(p: Plant): number {
  return area(p) + 1;
}
`, NewInlineFunction(), `{"line": 19, "name": "area", "keep_function": true}`, area + `
export function size(p: Plant): number {
  return p.height * p.height + 1;
}
`, ""},
		{"static method", plants + `
export class Box {
  static size = 2;
  static twice(n: number): number {
    return n * this.size;
  }
}

export const t = Box.twice(3);
`, NewInlineFunction(), `{"line": 21, "name": "twice", "keep_function": true}`, plants + `
export class Box {
  static size = 2;
  static twice(n: number): number {
    return n * this.size;
  }
}

export const t = 3 * Box.size;
`, ""},
		{"early return", plants + `
function f(x: number): number {
  if (x > 1) {
    return 1;
  }
  water();
  return 0;
}

export const y = f(2);
`, NewInlineFunction(), `{"line": 22, "name": "f"}`, "", "returns early"},
		{"recursive", plants + `
function f(x: number): number {
  return x > 0 ? f(x - 1) : 0;
}

export const y = f(2);
`, NewInlineFunction(), `{"line": 18, "name": "f"}`, "", "calls itself"},
		{"overridable", plants + `
export class Bed {
  depth(): number {
    return 3;
  }
}

export const d = new Bed().depth();
`, NewInlineFunction(), `{"line": 20, "name": "depth"}`, "", "a subclass could override"},
	})
}
