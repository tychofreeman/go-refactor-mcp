package m

import "testing"

func TestFactorial(t *testing.T) {
	if fact(3) != 6 {
		t.Fatal()
	}
}
