package m

import "testing"

func TestSize(t *testing.T) {
	if size(&mover{decl: "ab"}, 1) != 3 {
		t.Fatal()
	}
}
