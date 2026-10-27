package insertstmts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

func garden(t *testing.T) (*workspace.Workspace, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("../../inspect/testdata/garden")); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return ws, dir
}

func source(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "src/garden.ts"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInsert(t *testing.T) {
	for _, tc := range []struct{ name, args, want string }{
		{"before", `{"line": 18, "where": "before", "content": "this.plants.reverse();"}`,
			"    this.plants.push(p);\n    this.plants.reverse();\n    this.plants.sort("},
		{"after", `{"line": 18, "where": "after", "content": "this.plants.reverse();"}`,
			"b.height);\n    this.plants.reverse();\n  }\n"},
		{"after a top-level statement", `{"line": 30, "where": "after", "content": "export const rain = water(5);"}`,
			"  return `${ml} ml`;\n};\nexport const rain = water(5);\n\nexport function prune"},
		{"into an empty block", `{"line": 13, "where": "into", "content": "if (soil === \"\") throw new Error(\"no soil\");"}`,
			"  constructor(public readonly soil: string) {\n    if (soil === \"\") throw new Error(\"no soil\");\n  }\n"},
		{"into a block", `{"line": 32, "where": "into", "content": "if (by < 0) return p;"}`,
			"Plant {\n  if (by < 0) return p;\n  const height"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws, dir := garden(t)
			args := `{"file": "src/garden.ts", ` + tc.args[1:]
			res, err := refactor.Run(ws, New(), json.RawMessage(args), refactor.OptionsOf[*workspace.Workspace]{})
			if err != nil {
				t.Fatal(err)
			}
			if !res.Applied || !strings.Contains(source(t, dir), tc.want) {
				t.Fatalf("%s\n%s", res, source(t, dir))
			}
		})
	}
}

func TestRefusals(t *testing.T) {
	ws, _ := garden(t)
	for _, tc := range []struct{ args, want string }{
		{`{"file": "src/garden.ts", "line": 19, "where": "before", "content": "x;"}`, "no statement starts line 19"},
		{`{"file": "src/garden.ts", "line": 17, "where": "into", "content": "x;"}`, "no block opens on line 17"},
		{`{"file": "src/garden.ts", "line": 17, "where": "inside", "content": "x;"}`, "where must be before, after or into"},
		{`{"file": "src/garden.ts", "line": 17, "where": "before", "content": "// a note"}`, "content has no statements"},
		{`{"file": "src/garden.ts", "line": 17, "where": "before", "content": "this.plants.push(1);"}`, "not assignable"},
	} {
		res, err := refactor.Run(ws, New(), json.RawMessage(tc.args), refactor.OptionsOf[*workspace.Workspace]{})
		if err != nil {
			t.Fatal(err)
		}
		if res.Applied || !strings.Contains(res.Refused, tc.want) {
			t.Errorf("%s: want a refusal with %q, got %s", tc.args, tc.want, res)
		}
	}
}
