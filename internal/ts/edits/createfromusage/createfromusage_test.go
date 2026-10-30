package createfromusage

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/edits/adddecl"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/edits/fiximports"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/edits/replacetext"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tstest"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

type step = refactor.StepOf[*workspace.Workspace]

func load(t *testing.T, files map[string]string) (*workspace.Workspace, string) {
	t.Helper()
	dir := t.TempDir()
	files["tsconfig.json"] = `{"compilerOptions": {"strict": true, "module": "ESNext", "moduleResolution": "Bundler", "noEmit": true, "types": []}, "include": ["src"]}`
	ws := tstest.Load(t, dir, files)
	return ws, dir
}

func read(t *testing.T, dir, rel string) string {
	t.Helper()
	return tstest.Read(t, dir, rel)
}

func mk(r refactor.RefactoringOf[*workspace.Workspace], args map[string]string) step {
	raw, _ := json.Marshal(args)
	return step{Refactoring: r, Args: raw}
}

// batch runs steps checked at their end, with the stubs and imports the
// engine supplies, as the TypeScript language does.
func batch(t *testing.T, ws *workspace.Workspace, steps ...step) *refactor.BatchResult {
	t.Helper()
	b, err := refactor.RunBatch(ws, steps, refactor.OptionsOf[*workspace.Workspace]{Unchecked: true, Imports: fiximports.New(), Declare: New()})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

var garden = map[string]string{
	"src/plant.ts": "export function water(liters: number): string {\n  return `${liters} L`;\n}\n",
	"src/bed.ts":   "export class Bed {\n  plants: string[] = [];\n}\n",
}

func copyOf(m map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

// A test imports what its module does not export yet: the stub is
// exported from that module, typed from the call.
func TestStubInTheImportedModule(t *testing.T) {
	files := copyOf(garden)
	files["src/plant.test.ts"] = "import { water } from \"./plant\";\n\nexport const label: string = water(1);\n"
	ws, dir := load(t, files)
	b := batch(t, ws,
		mk(replacetext.New(), map[string]string{"file": "src/plant.test.ts", "old": "import { water }", "new": "import { water, shade }"}),
		mk(adddecl.New(), map[string]string{"file": "src/plant.test.ts", "content": "export const shaded: number = shade(10, \"noon\");"}),
	)
	if !b.Applied {
		t.Fatalf("not applied: %s", b)
	}
	want := "export function shade(n: number, s: string): number {\n  throw new Error(\"not implemented\");\n}\n"
	if got := read(t, dir, "src/plant.ts"); !strings.HasSuffix(got, "}\n\n"+want) {
		t.Fatalf("plant.ts:\n%s\nwant it to end:\n%s", got, want)
	}
	if !strings.Contains(b.String(), "create_from_usage: declared stubs for shade") {
		t.Errorf("the result does not name the stub:\n%s", b)
	}
}

// A name the file calls and nothing exports is declared in the file; a
// method is declared in the class it is called on, named after the
// arguments that are names.
func TestStubInTheFileAndInAClass(t *testing.T) {
	ws, dir := load(t, copyOf(garden))
	b := batch(t, ws,
		mk(adddecl.New(), map[string]string{"file": "src/plant.ts", "content": "import { Bed } from \"./bed\";\n\nexport const dry: boolean = dryness(2);\n\nexport function sow(rose: string): number {\n  return new Bed().count(rose);\n}"}),
	)
	if !b.Applied {
		t.Fatalf("not applied: %s", b)
	}
	if got, want := read(t, dir, "src/plant.ts"), "\nfunction dryness(n: number): boolean {\n  throw new Error(\"not implemented\");\n}\n"; !strings.HasSuffix(got, want) {
		t.Errorf("plant.ts:\n%s\nwant it to end:\n%s", got, want)
	}
	if got, want := read(t, dir, "src/bed.ts"), "export class Bed {\n  plants: string[] = [];\n  count(rose: string): number {\n    throw new Error(\"not implemented\");\n  }\n}\n"; got != want {
		t.Errorf("bed.ts:\n%s\nwant:\n%s", got, want)
	}
}

// A call passed where any value goes (a test's equal(got: unknown, ...))
// gets a stub returning unknown.
func TestStubWhereAnyValueGoes(t *testing.T) {
	ws, dir := load(t, copyOf(garden))
	b := batch(t, ws,
		mk(adddecl.New(), map[string]string{"file": "src/plant.ts", "content": "import { Bed } from \"./bed\";\n\nfunction same(got: unknown, want: unknown): boolean {\n  return got === want;\n}\n\nexport const ok = same(new Bed().fill(2), 2);"}),
	)
	if !b.Applied {
		t.Fatalf("not applied: %s", b)
	}
	if got, want := read(t, dir, "src/bed.ts"), "  fill(n: number): unknown {\n"; !strings.Contains(got, want) {
		t.Errorf("bed.ts:\n%s\nwant it to hold:\n%s", got, want)
	}
}

// A class a file makes with new and nothing declares gets a stub: its
// constructor takes the arguments' types; before the statement that makes
// it, or in the module the file imports it from, exported, if it does.
func TestStubAClass(t *testing.T) {
	files := copyOf(garden)
	files["src/tray.test.ts"] = "import { Bed } from \"./bed\";\n\nexport const b = new Bed();\n"
	ws, dir := load(t, files)
	b := batch(t, ws,
		mk(adddecl.New(), map[string]string{"file": "src/plant.ts", "content": "export const planter = new Pot(3, \"clay\");"}),
		mk(replacetext.New(), map[string]string{"file": "src/tray.test.ts", "old": "import { Bed }", "new": "import { Bed, Tray }"}),
		mk(adddecl.New(), map[string]string{"file": "src/tray.test.ts", "content": "export const tray = new Tray();"}),
	)
	if !b.Applied {
		t.Fatalf("not applied: %s", b)
	}
	if got, want := read(t, dir, "src/plant.ts"), "}\n\nclass Pot {\n  constructor(n: number, s: string) {}\n}\n\nexport const planter = new Pot(3, \"clay\");\n"; !strings.HasSuffix(got, want) {
		t.Errorf("plant.ts:\n%s\nwant it to end (a class goes before its first use):\n%s", got, want)
	}
	if got, want := read(t, dir, "src/bed.ts"), "\nexport class Tray {}\n"; !strings.HasSuffix(got, want) {
		t.Errorf("bed.ts:\n%s\nwant it to end:\n%s", got, want)
	}
}

// A property a file reads or writes and its class does not declare gets
// a stub, typed from where it is read or what is written to it.
func TestStubAProperty(t *testing.T) {
	ws, dir := load(t, copyOf(garden))
	b := batch(t, ws,
		mk(adddecl.New(), map[string]string{"file": "src/plant.ts", "content": "import { Bed } from \"./bed\";\n\nexport const size: number = new Bed().size;\n\nexport function deepen(b: Bed): void {\n  b.depth = 2;\n}"}),
	)
	if !b.Applied {
		t.Fatalf("not applied: %s", b)
	}
	got := read(t, dir, "src/bed.ts")
	for _, want := range []string{"  size!: number;\n", "  depth!: number;\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("bed.ts:\n%s\nwant it to hold:\n%s", got, want)
		}
	}
}

// results gives a result type the use does not tell; to_file puts the
// stubs of a file's own names in another file, exported and imported.
func TestStubResultsAndFile(t *testing.T) {
	ws, dir := load(t, copyOf(garden))
	cfu := step{Refactoring: New(), Args: json.RawMessage(`{"file": "src/plant.ts", "names": ["shade", "dryness"], "results": {"shade": "number"}, "to_file": "src/weather.ts"}`)}
	b := batch(t, ws,
		mk(adddecl.New(), map[string]string{"file": "src/plant.ts", "content": "export const vs = [shade(1)];\n\nexport const dry: boolean = dryness(2);"}),
		cfu,
	)
	if !b.Applied {
		t.Fatalf("not applied: %s", b)
	}
	want := "export function shade(n: number): number {\n  throw new Error(\"not implemented\");\n}\n\nexport function dryness(n: number): boolean {\n  throw new Error(\"not implemented\");\n}\n"
	if got := read(t, dir, "src/weather.ts"); got != want {
		t.Errorf("weather.ts:\n%s\nwant:\n%s", got, want)
	}
	if got := read(t, dir, "src/plant.ts"); !strings.Contains(got, "import { shade, dryness } from \"./weather\"") {
		t.Errorf("plant.ts does not import the stubs:\n%s", got)
	}
}

// A name not called, or whose result type its place does not tell, gets
// no stub, and the batch says why.
func TestNoStub(t *testing.T) {
	for _, tc := range []struct{ content, want string }{
		{"export const v = moisture;", "no stub was declared for moisture, which is not called"},
		{"export const vs = [shade(1)];", "no stub was declared for shade: cannot tell the results of shade"},
	} {
		ws, _ := load(t, copyOf(garden))
		b := batch(t, ws, mk(adddecl.New(), map[string]string{"file": "src/plant.ts", "content": tc.content}))
		if b.Applied || !strings.Contains(b.Refused, tc.want) {
			t.Errorf("%s: want a refusal with %q, got %s", tc.content, tc.want, b)
		}
	}
}

func TestMissing(t *testing.T) {
	ws, _ := load(t, copyOf(garden))
	for err, want := range map[string]string{
		"src/plant.ts:3:29: Cannot find name 'dryness'.":                               "src/plant.ts\x00dryness",
		"src/plant.ts:6:20: Property 'count' does not exist on type 'Bed'.":            "Bed.count",
		"src/plant.test.ts:1:17: Module '\"./plant\"' has no exported member 'shade'.": "src/plant.test.ts\x00shade",
	} {
		if _, _, key, _, ok := New().Missing(ws, err); !ok || key != want {
			t.Errorf("Missing(%q) = %q, %v; want %q", err, key, ok, want)
		}
	}
	if _, name, _, close, _ := New().Missing(ws, "src/plant.ts:3:29: Cannot find name 'watr'. Did you mean 'water'?"); name != "watr" || close != "water" {
		t.Errorf("a misspelling: %q close to %q", name, close)
	}
}
