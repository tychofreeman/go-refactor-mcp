package m

import "testing"

func TestJob(t *testing.T) {
	j := job{name: "x", owner: "me"}
	if j.owner != "me" {
		t.Fatal()
	}
}
