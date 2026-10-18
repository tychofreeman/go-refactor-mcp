package mcpserver

import (
	"strings"
	"testing"
)

// Results show the changed lines and where they are, without context lines.
func TestAcceptDiffsAreCompact(t *testing.T) {
	cs, _ := setup(t)
	out, _ := call(t, cs, "add_parameter", map[string]any{"file": "calc/calc.go", "name": "Apply", "parameter": "k", "type": "int", "value": "0"})
	mustContain(t, "add_parameter", out, "APPLIED add_parameter", "-func Apply(n int) int", "+func Apply(n, k int) int", "@@")
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, " ") {
			t.Errorf("context line in the result: %q\n%s", l, out)
		}
	}
}
