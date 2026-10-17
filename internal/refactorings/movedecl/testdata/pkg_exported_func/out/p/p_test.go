package p

import (
	"example.com/m/q"
	"testing"
)

func TestShout(t *testing.T) {
	if q.Shout("a") != "A!" {
		t.Fatal()
	}
}
