package q

import (
	"testing"

	"example.com/m/p"
)

func TestY(t *testing.T) {
	if p.Other() != 2 {
		t.Fail()
	}
}
