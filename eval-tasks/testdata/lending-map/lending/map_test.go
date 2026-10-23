package lending

import (
	"reflect"
	"testing"
)

func TestAcceptMap(t *testing.T) {
	m := Item{ID: "w", Title: "Wales", Kind: "map"}
	if LoanDays(m) != 7 || DailyFee(m) != 25 {
		t.Errorf("LoanDays = %d, DailyFee = %d", LoanDays(m), DailyFee(m))
	}
	if Label(m) != "Map: Wales" || Shelf(m) != "reference" || !Renewable(m) {
		t.Errorf("Label = %q, Shelf = %q, Renewable = %v", Label(m), Shelf(m), Renewable(m))
	}
	l := Loan{Item: m, Member: Member{ID: "p"}, Day: 0}
	if got, want := Settle([]Loan{l}, 12), []Charge{{Member: "p", Item: "w", Fee: 75}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Settle = %v, want %v", got, want)
	}
	if !Warn(l, 8) || Warn(l, 10) {
		t.Errorf("Warn")
	}
	c := &Catalog{}
	if err := Import(c, "w|Wales|map|2"); err != nil {
		t.Fatal(err)
	}
	if c.Items["w"].Kind != "map" {
		t.Errorf("Import = %v", c.Items)
	}
	b, d, a := Item{Title: "Emma", Kind: "book"}, Item{Title: "Heat", Kind: "dvd"}, Item{Title: "World", Kind: "atlas"}
	g := Item{Title: "Vogue", Kind: "magazine"}
	if LoanDays(b) != 21 || DailyFee(d) != 50 || Label(a) != "WORLD (reference)" || Shelf(b) != "stacks" || !Renewable(d) {
		t.Errorf("other kinds changed")
	}
	if LoanDays(g) != 14 || Label(g) != "Magazine: Vogue" || Renewable(g) {
		t.Errorf("magazine changed")
	}
}
