package refactor

import "testing"

// A tool may be called by another name: add_property is add_field.
func TestToolName(t *testing.T) {
	for name, want := range map[string]string{"add_property": "add_field", "add_field": "add_field", "change_return_type": "add_result", "rename": "rename"} {
		if got := ToolName(name); got != want {
			t.Errorf("ToolName(%q) = %q, want %q", name, got, want)
		}
	}
}
