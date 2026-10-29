package m

import "testing"

func TestLabel(t *testing.T) {
	r := rec{"a"}
	if label(r) != "a" {
		t.Errorf("got %+v", r)
	}
}
