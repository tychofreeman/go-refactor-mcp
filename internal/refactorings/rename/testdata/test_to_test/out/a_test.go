package m

import "testing"

func TestSumOfTwo(t *testing.T) { check(t) }

func check(t *testing.T) {
	if Sum(1, 2) != 3 {
		t.Error("wrong")
	}
}
