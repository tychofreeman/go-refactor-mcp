package desk

import (
	"reflect"
	"testing"

	"example.com/lending/lending"
)

func TestDesk(t *testing.T) {
	ls := []lending.Loan{
		{Item: lending.Item{ID: "i", Kind: "atlas"}, Member: lending.Member{ID: "m"}, Day: 0},
		{Item: lending.Item{ID: "j", Kind: "book"}, Member: lending.Member{ID: "m"}, Day: 0},
	}
	if got, want := Rare(ls, 7), []lending.Charge{{Member: "m", Item: "i", Fee: 200}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Rare = %v, want %v", got, want)
	}
	if got := Owed(ls, 30); got["m"] != 570 {
		t.Errorf("Owed = %v", got)
	}
	if got := North(ls, 5); len(got) != 1 || got[0].Fee != 200 {
		t.Errorf("North = %v", got)
	}
}
