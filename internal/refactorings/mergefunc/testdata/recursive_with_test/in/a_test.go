package m

import "testing"

func TestFactorial(t *testing.T) {
	if factorial(3) != 6 {
		t.Fatal()
	}
}
