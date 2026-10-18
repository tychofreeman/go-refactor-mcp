package mcpserver

import (
	"strings"
	"testing"
)

// The model wrote the code it adds, so the result says how many lines went
// where instead of repeating them; lines the tool adds itself are shown.
func TestAcceptAddedCodeIsNotEchoed(t *testing.T) {
	cs, _ := setup(t)
	content := "package calc\n\n// Twice doubles n.\nfunc Twice(n int) int {\n\treturn 2 * n\n}\n"
	out, _ := call(t, cs, "create_file", map[string]any{"file": "calc/twice.go", "content": content})
	mustContain(t, "create_file", out, "APPLIED create_file", "1-6")
	if strings.Contains(out, "return 2 * n") || strings.Contains(out, "// Twice doubles n.") {
		t.Errorf("create_file repeated the content:\n%s", out)
	}

	decl := "// Label names n.\nfunc Label(n int) string {\n\treturn strconv.Itoa(n)\n}"
	out, _ = call(t, cs, "add_declaration", map[string]any{"file": "calc/twice.go", "content": decl})
	mustContain(t, "add_declaration", out, "APPLIED add_declaration", `"strconv"`)
	if strings.Contains(out, "strconv.Itoa(n)") || strings.Contains(out, "// Label names n.") {
		t.Errorf("add_declaration repeated the content:\n%s", out)
	}

	// Other tools still show what changed.
	out, _ = call(t, cs, "rename", map[string]any{"file": "calc/calc.go", "line": 6, "name": "Apply", "new_name": "Scale"})
	mustContain(t, "rename", out, "APPLIED rename", "Scale")
}
