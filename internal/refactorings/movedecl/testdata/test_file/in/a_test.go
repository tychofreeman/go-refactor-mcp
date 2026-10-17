package m

import "testing"

func TestX(t *testing.T) { check(t, 1) }

func check(t *testing.T, n int) {
	if n != 1 {
		t.Fatal(n)
	}
}
