package m

import "testing"

func check(t *testing.T, n int) {
	if n != 1 {
		t.Fatal(n)
	}
}
