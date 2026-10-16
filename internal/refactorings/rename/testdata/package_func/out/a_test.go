package m

import "testing"

func TestHelper(t *testing.T) {
	if twice(2) != 4 {
		t.Fatal("bad")
	}
}
