package m

import "testing"

func TestSize(t *testing.T) {
	mv := &mover{decl: "ab"}
	if mv.size(1) != 3 {
		t.Fatal()
	}
}
