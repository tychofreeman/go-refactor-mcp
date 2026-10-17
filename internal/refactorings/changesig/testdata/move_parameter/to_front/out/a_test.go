package m

import "testing"

func TestLabel(t *testing.T) {
	if label("y", 2) != "y" {
		t.Fatal()
	}
}
