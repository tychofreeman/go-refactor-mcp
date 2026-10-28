package move

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

var garden = map[string]string{
	"src/bed.ts": `export interface Bed {
  depth: number;
}
`,
	"src/plant.ts": `import { Bed } from "./bed";

// liters is how much a bed takes.
function liters(b: Bed): number {
  return b.depth * 2;
}

export function water(b: Bed): string {
  return ` + "`${liters(b)} L`" + `;
}

export const morning = water({ depth: 1 });
`,
	"src/garden.ts": `import { water } from "./plant";
import type { Bed } from "./bed";

export function tend(b: Bed): string {
  return water(b);
}
`,
	"data/rows.json": "[1, 2]\n",
}

// load writes the garden, with extra files, under a new directory and
// loads it.
func load(t *testing.T, extra map[string]string) (*workspace.Workspace, string) {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{"tsconfig.json": config}
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
	res, err := refactor.Run(ws, r, json.RawMessage(args), refactor.OptionsOf[*workspace.Workspace]{})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// want checks that file reads text, or is gone when text is "".
func want(t *testing.T, dir, file, text string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, file))
	if text == "" {
		if err == nil {
			t.Errorf("%s is still there", file)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != text {
		t.Errorf("%s:\n%s\nwant:\n%s", file, b, text)
	}
}

func refused(t *testing.T, res *refactor.Result, reason string) {
	t.Helper()
	if res.Applied || !strings.Contains(res.Refused, reason) {
		t.Errorf("want a refusal with %q, got %s", reason, res)
	}
}

func TestMoveFile(t *testing.T) {
	ws, dir := load(t, nil)
	res := run(t, ws, NewMoveFile(), `{"file": "src/plant.ts", "to": "src/lib/plant.ts"}`)
	if !res.Applied {
		t.Fatalf("%s", res)
	}
	want(t, dir, "src/plant.ts", "")
	want(t, dir, "src/garden.ts", strings.Replace(garden["src/garden.ts"], `"./plant"`, `"./lib/plant"`, 1))
	want(t, dir, "src/lib/plant.ts", strings.Replace(garden["src/plant.ts"], `"./bed"`, `"../bed"`, 1))

	res = run(t, ws, NewMoveFile(), `{"file": "src/lib", "to": "src/util"}`)
	if !res.Applied {
		t.Fatalf("%s", res)
	}
	want(t, dir, "src/garden.ts", strings.Replace(garden["src/garden.ts"], `"./plant"`, `"./util/plant"`, 1))
	want(t, dir, "src/util/plant.ts", strings.Replace(garden["src/plant.ts"], `"./bed"`, `"../bed"`, 1))

	if res := run(t, ws, NewMoveFile(), `{"file": "data/rows.json", "to": "data/cols.json"}`); !res.Applied {
		t.Fatalf("%s", res)
	}
	want(t, dir, "data/cols.json", "[1, 2]\n")
	refused(t, run(t, ws, NewMoveFile(), `{"file": "src/bed.ts", "to": "src/garden.ts"}`), "already exists")
}

func TestDeleteFile(t *testing.T) {
	ws, dir := load(t, map[string]string{"src/old.ts": "export const x = 1;\n"})
	refused(t, run(t, ws, NewDeleteFile(), `{"file": "src/bed.ts"}`), "src/bed.ts is imported at src/garden.ts:2, src/plant.ts:1")
	if res := run(t, ws, NewDeleteFile(), `{"file": "src/old.ts"}`); !res.Applied {
		t.Fatalf("%s", res)
	}
	want(t, dir, "src/old.ts", "")
	if res := run(t, ws, NewDeleteFile(), `{"file": "data"}`); !res.Applied {
		t.Fatalf("%s", res)
	}
	want(t, dir, "data/rows.json", "")
}

func TestMoveDeclaration(t *testing.T) {
	ws, dir := load(t, nil)
	refused(t, run(t, ws, NewMoveDeclaration(), `{"file": "src/plant.ts", "name": "water", "to_file": "src/water.ts"}`),
		"uses liters (plant.ts:9), which stays in plant.ts and is not exported")

	res := run(t, ws, NewMoveDeclaration(), `{"file": "src/plant.ts", "name": "water", "also": ["liters"], "to_file": "src/water.ts"}`)
	if !res.Applied {
		t.Fatalf("%s", res)
	}
	want(t, dir, "src/water.ts", `import { Bed } from "./bed";

// liters is how much a bed takes.
function liters(b: Bed): number {
  return b.depth * 2;
}

export function water(b: Bed): string {
  return `+"`${liters(b)} L`"+`;
}
`)
	want(t, dir, "src/plant.ts", `import { water } from "./water";

export const morning = water({ depth: 1 });
`)
	want(t, dir, "src/garden.ts", strings.Replace(garden["src/garden.ts"], `"./plant"`, `"./water"`, 1))
}

func TestMoveDeclarationIntoAFileThatImportsIt(t *testing.T) {
	ws, dir := load(t, nil)
	res := run(t, ws, NewMoveDeclaration(), `{"file": "src/garden.ts", "line": 5, "name": "water", "with_dependencies": true, "to_file": "src/garden.ts"}`)
	if !res.Applied {
		t.Fatalf("%s", res)
	}
	want(t, dir, "src/garden.ts", `import type { Bed } from "./bed";

export function tend(b: Bed): string {
  return water(b);
}

// liters is how much a bed takes.
function liters(b: Bed): number {
  return b.depth * 2;
}

export function water(b: Bed): string {
  return `+"`${liters(b)} L`"+`;
}
`)
	want(t, dir, "src/plant.ts", `import { water } from "./garden";

export const morning = water({ depth: 1 });
`)
}

func TestMoveDeclarationExportsWhatStaysUses(t *testing.T) {
	ws, dir := load(t, map[string]string{"src/sizes.ts": `function big(n: number): boolean {
  return n > 9;
}

export function label(n: number): string {
  return big(n) ? "big" : "small";
}
`})
	res := run(t, ws, NewMoveDeclaration(), `{"file": "src/sizes.ts", "name": "big", "to_file": "src/big.ts"}`)
	if !res.Applied {
		t.Fatalf("%s", res)
	}
	want(t, dir, "src/big.ts", `export function big(n: number): boolean {
  return n > 9;
}
`)
	want(t, dir, "src/sizes.ts", `import { big } from "./big";

export function label(n: number): string {
  return big(n) ? "big" : "small";
}
`)
}

func TestMoveDeclarationSplitsAnImport(t *testing.T) {
	ws, dir := load(t, map[string]string{"src/both.ts": `import { water, morning } from "./plant";

export const both = water({ depth: 2 }) + morning;
`})
	res := run(t, ws, NewMoveDeclaration(), `{"file": "src/plant.ts", "name": "water", "with_dependencies": true, "to_file": "src/water.ts"}`)
	if !res.Applied {
		t.Fatalf("%s", res)
	}
	want(t, dir, "src/both.ts", `import { morning } from "./plant";
import { water } from "./water";

export const both = water({ depth: 2 }) + morning;
`)
}

func TestMoveDeclarationRefusals(t *testing.T) {
	ws, _ := load(t, map[string]string{"src/dup.ts": "export const morning = 2;\n", "src/ns.ts": `import * as p from "./plant";

export const y = p.water({ depth: 2 });
`})
	refused(t, run(t, ws, NewMoveDeclaration(), `{"file": "src/plant.ts", "name": "water", "with_dependencies": true, "to_file": "src/water.ts"}`),
		"imports plant.ts as a namespace")
	refused(t, run(t, ws, NewMoveDeclaration(), `{"file": "src/plant.ts", "name": "morning", "to_file": "src/dup.ts"}`), "dup.ts already declares morning (dup.ts:1)")
	refused(t, run(t, ws, NewMoveDeclaration(), `{"file": "src/garden.ts", "name": "tend", "to_file": "src/plant.ts", "new_name": "x"}`), "new_name is for Go")
}

// Imports are written in each file's own quotes and semicolons.
func TestMoveDeclarationKeepsEachFilesStyle(t *testing.T) {
	ws, dir := load(t, map[string]string{
		"src/size.ts": "export const big = (n: number): boolean => n > 9\n\nexport const label = 'size'\n",
		"src/use.ts":  "import { big } from './size'\n\nexport const b = big(3)\n",
	})
	res := run(t, ws, NewMoveDeclaration(), `{"file": "src/size.ts", "name": "big", "to_file": "src/big.ts"}`)
	if !res.Applied {
		t.Fatalf("%s", res)
	}
	want(t, dir, "src/use.ts", "import { big } from './big'\n\nexport const b = big(3)\n")
	want(t, dir, "src/size.ts", "export const label = 'size'\n")
}
