package m

import "testing"

func TestLabel(t *testing.T) {
	q := point{3, 4}
	if label(q, q.x) != "x=3" {
		t.Fatal()
	}
}
