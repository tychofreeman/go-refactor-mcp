package typescript

import "testing"

// A Go tool's name runs the TypeScript tool that does its operation.
func TestGoNames(t *testing.T) {
	for goName, want := range goNames {
		r, ok := lookup(goName)
		if !ok || r.Name() != want {
			t.Errorf("lookup(%q) = %v, %v; want %s", goName, r, ok, want)
		}
	}
	if _, ok := lookup("no_such_tool"); ok {
		t.Errorf("lookup(no_such_tool) found a tool")
	}
}
