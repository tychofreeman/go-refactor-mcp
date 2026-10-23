package lending

import (
	"reflect"
	"testing"
)

func TestAcceptEbook(t *testing.T) {
	e := Item{ID: "e", Title: "Emma", Kind: "ebook"}
	if LoanDays(e) != 21 || DailyFee(e) != 5 || MaxRenewals(e) != 5 || !Renewable(e) {
		t.Errorf("LoanDays = %d, DailyFee = %d, MaxRenewals = %d", LoanDays(e), DailyFee(e), MaxRenewals(e))
	}
	if Label(e) != "eBook: Emma" || Shelf(e) != "online" {
		t.Errorf("Label = %q, Shelf = %q", Label(e), Shelf(e))
	}
	l := Loan{Item: e, Member: Member{ID: "p"}, Day: 0}
	if got, want := Settle([]Loan{l}, 30), []Charge{{Member: "p", Item: "e", Fee: 35}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Settle = %v, want %v", got, want)
	}
	if !Warn(l, 22) || Warn(l, 24) {
		t.Errorf("Warn")
	}
	c := &Catalog{}
	if err := Import(c, "e|Emma|ebook|1"); err != nil {
		t.Fatal(err)
	}
	if MaxRenewals(Item{Kind: "map"}) != 2 || Label(Item{Title: "Wales", Kind: "map"}) != "Map: Wales" {
		t.Errorf("map changed")
	}
}
