package m

import "testing"

func TestSize(t *testing.T) {
	if (mover{source: source{path: "ab"}, n: 1}).size() != 3 {
		t.Fatal()
	}
}
