package m

import "testing"

func TestScale(t *testing.T) {
	if scale(2, 3, "") != 6 {
		t.Fatal()
	}
}
