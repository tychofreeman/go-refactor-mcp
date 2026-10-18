package m

import "testing"

func TestSize(t *testing.T) {
	mv := &mover{decl: "ab"}
	if size(mv, 1) != 3 {
		t.Fatal()
	}
}
