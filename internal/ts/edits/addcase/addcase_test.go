package addcase

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

const plants = `export function water(kind: string): number {
  switch (kind) {
    case "fern":
      return 3;
    case "moss":
    default:
      return 0;
  }
}

export function size(height: number): string {
  if (height > 10) {
    return "tall";
  } else if (height > 1) {
    return "mid";
  } else {
    return "short";
  }
}
`

func TestAddCase(t *testing.T) {
	check(t, plants, []tcase{
		{"at the end, before the default", `{"line": 2, "content": "case \"cactus\":\n  return 1;"}`, `    case "fern":
      return 3;
    case "cactus":
      return 1;
    case "moss":
    default:`, ""},
		{"before a clause", `{"line": 3, "where": "before", "content": "case \"rose\": return 2;"}`, `  switch (kind) {
    case "rose": return 2;
    case "fern":`, ""},
		{"after a clause", `{"line": 3, "content": "case \"rose\": return 2;"}`, `      return 3;
    case "rose": return 2;
    case "moss":`, ""},
		{"a branch before the else", `{"line": 12, "content": "else if (height > 5) {\n  return \"high\";\n}"}`, `  } else if (height > 1) {
    return "mid";
  } else if (height > 5) {
    return "high";
  } else {`, ""},
		{"before an else if", `{"line": 14, "where": "before", "content": "else if (height > 5) {\n  return \"high\";\n}"}`, `    return "tall";
  } else if (height > 5) {
    return "high";
  } else if (height > 1) {`, ""},
		{"a second default", `{"line": 2, "content": "default:\n  return 9;"}`, "", "has a default already"},
		{"a second else", `{"line": 12, "content": "else {\n  return \"?\";\n}"}`, "", "has an else already"},
		{"not a switch", `{"line": 1, "content": "case 1: return 1;"}`, "", "no switch or case clause starts on line 1"},
	})
}
