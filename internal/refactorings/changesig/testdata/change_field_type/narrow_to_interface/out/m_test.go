package m

import "testing"

func TestDays(t *testing.T) {
	if Days(Item{Kind: Book{}}) != 21 {
		t.Fail()
	}
}
