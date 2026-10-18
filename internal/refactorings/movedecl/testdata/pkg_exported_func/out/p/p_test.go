package p

import (
	"testing"

	"example.com/m/q"
)

func TestShout(t *testing.T) {
	if q.Shout("a") != "A!" {
		t.Fatal()
	}
}
