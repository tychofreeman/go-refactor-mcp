package addelement

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tstest"
)

// edited runs New on a fresh copy of src (as src/g.ts) with args, and
// returns the file after, or the refusal.
func edited(t *testing.T, src, args string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	ws := tstest.Load(t, dir, map[string]string{
		"tsconfig.json": `{"compilerOptions": {"strict": true, "module": "ESNext", "moduleResolution": "Bundler", "noEmit": true, "types": []}, "include": ["src"]}`,
		"src/g.ts":      src,
	})
	res := tstest.Run(t, ws, New(), `{"file": "src/g.ts", `+args[1:])
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

const plants = `export const sizes = {
  fern: 3,
  moss: 1,
};

export const names = ["fern", "moss"];

export const none: string[] = [];
`

func TestAddElement(t *testing.T) {
	check(t, plants, []tcase{
		{"at the end, with a trailing comma", `{"line": 1, "element": "cactus: 2"}`, "  moss: 1,\n  cactus: 2,\n};", ""},
		{"before a key", `{"line": 1, "element": "rose: 4", "before": "moss"}`, "  fern: 3,\n  rose: 4,\n  moss: 1,", ""},
		{"at the end of a line", `{"line": 6, "element": "\"cactus\""}`, `["fern", "moss", "cactus"]`, ""},
		{"after an element", `{"line": 6, "element": "\"rose\"", "after": "\"fern\""}`, `["fern", "rose", "moss"]`, ""},
		{"into an empty one", `{"line": 8, "element": "\"x\""}`, `none: string[] = ["x"];`, ""},
		{"no such key", `{"line": 1, "element": "x: 1", "after": "oak"}`, "", "has no element oak"},
		{"no literal", `{"line": 2, "element": "x: 1"}`, "", "no object or array literal starts on line 2"},
	})
}

const beds = `export const soils = {
  fern: "peat",
  moss: "loam",
};

export const beds: Record<string, { soil: string }> = {
  oak: { soil: "clay" },
};

export const kinds = ["fern", "moss"] as const;

export const tags: string[] = ["tall"];
`

// name finds the literal by the variable it initializes; from adds the
// elements of another's, written by element as a template.
func TestFrom(t *testing.T) {
	check(t, beds, []tcase{
		{"by name", `{"name": "soils", "element": "cactus: \"sand\""}`, "  moss: \"loam\",\n  cactus: \"sand\",\n};", ""},
		{"by name, through as const", `{"name": "kinds", "element": "\"oak\""}`, `["fern", "moss", "oak"] as const`, ""},
		{"from, by a template", `{"name": "beds", "from": "soils", "element": "$key: { soil: $value }"}`, "  oak: { soil: \"clay\" },\n  fern: { soil: \"peat\" },\n  moss: { soil: \"loam\" },\n};", ""},
		{"from, copied", `{"name": "tags", "from": "kinds"}`, `["tall", "fern", "moss"]`, ""},
		{"from itself rewrites", `{"name": "soils", "from": "soils", "element": "$key: $value.toUpperCase()"}`, "  fern: \"peat\".toUpperCase(),\n  moss: \"loam\".toUpperCase(),\n};", ""},
		{"no keys", `{"name": "tags", "from": "kinds", "element": "$key"}`, "", "the elements of kinds have no keys; element may use $value only"},
		{"no such variable", `{"name": "roses", "element": "x"}`, "", "no variable roses at the top level of the file"},
		{"line and name", `{"line": 1, "name": "soils", "element": "x: 1"}`, "", "give line, where the literal starts, or name"},
	})
}
