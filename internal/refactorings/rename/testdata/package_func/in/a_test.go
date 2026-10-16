package m

import "testing"

func TestHelper(t *testing.T) {
	if helper(2) != 4 {
		t.Fatal("bad")
	}
}
