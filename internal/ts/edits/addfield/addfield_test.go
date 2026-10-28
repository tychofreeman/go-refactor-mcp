package addfield

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
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
	for name, text := range map[string]string{
		"tsconfig.json": `{"compilerOptions": {"strict": true, "module": "ESNext", "moduleResolution": "Bundler", "noEmit": true, "types": []}, "include": ["src"]}`,
		"src/shapes.ts": shapes,
	} {
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
