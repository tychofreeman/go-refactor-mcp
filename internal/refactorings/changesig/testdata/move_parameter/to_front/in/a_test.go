package m

import "testing"

func TestLabel(t *testing.T) {
	if label(2, "y") != "y" {
		t.Fatal()
	}
}
