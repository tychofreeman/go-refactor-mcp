package p

import "testing"

func TestShout(t *testing.T) {
	if Shout("a") != "A!" {
		t.Fatal()
	}
}
