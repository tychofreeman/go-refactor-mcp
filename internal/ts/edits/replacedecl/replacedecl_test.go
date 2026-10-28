package replacedecl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

// edited runs New on a fresh copy of src (as src/g.ts) with args, and
// returns the file after, or the refusal.
func edited(t *testing.T, src, args string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	for name, text := range map[string]string{
		"tsconfig.json": `{"compilerOptions": {"strict": true, "module": "ESNext", "moduleResolution": "Bundler", "noEmit": true, "types": []}, "include": ["src"]}`,
		"src/g.ts":      src,
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
	res, err := refactor.Run(ws, New(), json.RawMessage(`{"file": "src/g.ts", `+args[1:]), refactor.OptionsOf[*workspace.Workspace]{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Applied {
		return "", res.Refused
	}
	b, err := os.ReadFile(filepath.Join(dir, "src/g.ts"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b), ""
}

type tcase struct{ name, args, want, refused string }

func check(t *testing.T, src string, cases []tcase) {
	t.Helper()
	for _, tc := range cases {
		got, refused := edited(t, src, tc.args)
		switch {
		case tc.refused != "" && !strings.Contains(refused, tc.refused):
			t.Errorf("%s: want a refusal with %q, got %q\n%s", tc.name, tc.refused, refused, got)
		case tc.refused == "" && refused != "":
			t.Errorf("%s: refused: %s", tc.name, refused)
		case tc.refused == "" && !strings.Contains(got, tc.want):
			t.Errorf("%s:\n%s\nwant it to hold:\n%s", tc.name, got, tc.want)
		}
	}
}

const plants = `export class Bed {
  base = 1;

  /** How deep it is. */
  depth(): number {
    return this.base;
  }
}

/** Waters a bed. */
export function water(b: Bed): number {
  return b.depth();
}
`

func TestReplaceDeclaration(t *testing.T) {
	check(t, plants, []tcase{
		{"a method, keeping its doc comment", `{"name": "Bed.depth", "content": "depth(extra = 0): number {\n  return this.base + extra;\n}"}`, `  /** How deep it is. */
  depth(extra = 0): number {
    return this.base + extra;
  }
}`, ""},
		{"a function, with a doc comment of its own", `{"name": "water", "content": "/** Waters a bed twice. */\nexport function water(b: Bed): number {\n  return b.depth() * 2;\n}"}`, "\n/** Waters a bed twice. */\nexport function water(b: Bed): number {\n  return b.depth() * 2;\n}\n", ""},
		{"no such name", `{"name": "dig", "content": "function dig() {}"}`, "", "declares no dig"},
	})
}
