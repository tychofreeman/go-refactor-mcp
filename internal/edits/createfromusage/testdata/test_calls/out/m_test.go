package m

import "testing"

func TestParse(t *testing.T) {
	got, err := Parse("3", 10)
	if err != nil {
		t.Fatal(err)
	}
	if got != 3 {
		t.Errorf("got %d", got)
	}
}
