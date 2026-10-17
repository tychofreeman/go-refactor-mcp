package m

import "testing"

func TestArea(t *testing.T) {
	if area(2, 3) != 6 {
		t.Fatal()
	}
}
