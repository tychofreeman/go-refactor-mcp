package m

import "testing"

func TestSize(t *testing.T) {
	if (&mover{decl: "ab"}).size(1) != 3 {
		t.Fatal()
	}
}
