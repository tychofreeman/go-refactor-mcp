package m_test

import (
	"testing"

	"example.com/m"
)

func TestA(t *testing.T) {
	if m.A() != 1 {
		t.Fatal()
	}
}
