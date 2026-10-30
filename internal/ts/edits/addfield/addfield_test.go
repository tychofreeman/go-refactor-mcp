package addfield

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tstest"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

const shapes = `export interface Plant {
  name: string;
  height: number;
}

export type Spot = { x: number; y: number };

export class Bed {
  plants: Plant[] = [];
}

export type Height = number;
`

func load(t *testing.T) (*workspace.Workspace, string) {
	t.Helper()
	dir := t.TempDir()
	ws := tstest.Load(t, dir, map[string]string{
		"tsconfig.json": `{"compilerOptions": {"strict": true, "module": "ESNext", "moduleResolution": "Bundler", "noEmit": true, "types": []}, "include": ["src"]}`,
		"src/shapes.ts": shapes,
	})
	return ws, dir
}

func TestAddField(t *testing.T) {
	for _, tc := range []struct{ name, args, want string }{
		{"after a member", `{"name": "Plant", "field": "/** colour is the flower's. */\ncolour: string;", "after": "name"}`,
			"export interface Plant {\n  name: string;\n  /** colour is the flower's. */\n  colour: string;\n  height: number;\n}"},
		{"before a member", `{"name": "Plant", "field": "colour: string;", "before": "name"}`,
			"export interface Plant {\n  colour: string;\n  name: string;"},
		{"names and a type, at the end", `{"name": "Plant", "field": "colour, scent", "type": "string"}`,
			"  height: number;\n  colour: string;\n  scent: string;\n}"},
		{"a type on one line", `{"name": "Spot", "field": "z: number;", "before": "y"}`,
			"export type Spot = {\n  x: number;\n  z: number;\n  y: number;\n};"},
		{"a class", `{"name": "Bed", "field": "rows = 2;"}`,
			"export class Bed {\n  plants: Plant[] = [];\n  rows = 2;\n}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws, dir := load(t)
			res, err := refactor.Run(ws, New(), json.RawMessage(`{"file": "src/shapes.ts", `+tc.args[1:]), refactor.OptionsOf[*workspace.Workspace]{})
			if err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(filepath.Join(dir, "src/shapes.ts"))
			if !res.Applied || !strings.Contains(string(got), tc.want) {
				t.Fatalf("%s\n%s\nwant it to contain:\n%s", res, got, tc.want)
			}
		})
	}
}

// uses has object literals of Plant, one on a line and one over lines,
// and one of another type.
const uses = `import type { Plant, Spot } from "./shapes";

export const rose: Plant = { name: "rose", height: 2 };
export const ferns: Plant[] = [
  {
    name: "fern",
    height: 1,
  },
];
export const spot: Spot = { x: 1, y: 2 };
`

// value gives a new required property to every object literal of the
// type, in the literal's layout.
func TestAddFieldWithValue(t *testing.T) {
	ws, dir := load(t)
	if err := os.WriteFile(filepath.Join(dir, "src/uses.ts"), []byte(uses), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	run := func(args string) *refactor.Result {
		res, err := refactor.Run(ws, New(), json.RawMessage(`{"file": "src/shapes.ts", `+args[1:]), refactor.OptionsOf[*workspace.Workspace]{})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := run(`{"name": "Plant", "field": "colour: string;"}`); res.Applied || !strings.Contains(res.Refused, "colour") {
		t.Errorf("without value: want a refusal naming colour, got %s", res)
	}
	if res := run(`{"name": "Plant", "field": "colour, scent", "type": "string", "value": "\"red\""}`); res.Applied || !strings.Contains(res.Refused, "value goes with one property") {
		t.Errorf("value with two properties: want a refusal, got %s", res)
	}
	if res := run(`{"name": "Plant", "field": "colour: string;", "value": "\"green\""}`); !res.Applied {
		t.Fatalf("with value: %s", res)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "src/uses.ts"))
	for _, want := range []string{`{ name: "rose", height: 2, colour: "green" }`, "    height: 1,\n    colour: \"green\",\n  },", "{ x: 1, y: 2 }"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("uses.ts:\n%s\nwant it to hold:\n%s", got, want)
		}
	}
}

func TestRefusals(t *testing.T) {
	ws, _ := load(t)
	for _, tc := range []struct{ args, want string }{
		{`{"name": "Height", "field": "x: number;"}`, "type Height is not an object type"},
		{`{"name": "Nope", "field": "x: number;"}`, "declares no interface, class or object type Nope"},
		{`{"name": "Plant", "field": "x: number;", "after": "nope"}`, "Plant has no member nope"},
		{`{"name": "Plant", "field": "name: string;"}`, "Duplicate identifier 'name'"},
		{`{"name": "Plant", "field": "x: number; }"}`, "field is not the members of an interface"},
	} {
		res, err := refactor.Run(ws, New(), json.RawMessage(`{"file": "src/shapes.ts", `+tc.args[1:]), refactor.OptionsOf[*workspace.Workspace]{})
		if err != nil {
			t.Fatal(err)
		}
		if res.Applied || !strings.Contains(res.Refused, tc.want) {
			t.Errorf("%s: want a refusal with %q, got %s", tc.args, tc.want, res)
		}
	}
}
