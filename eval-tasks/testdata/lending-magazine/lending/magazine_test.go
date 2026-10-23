package lending

import (
	"reflect"
	"testing"
)

func TestAcceptMagazine(t *testing.T) {
	m := Item{ID: "v", Title: "Vogue", Kind: "magazine"}
	if LoanDays(m) != 14 || DailyFee(m) != 20 {
		t.Errorf("LoanDays = %d, DailyFee = %d", LoanDays(m), DailyFee(m))
	}
	if Label(m) != "Magazine: Vogue" || Shelf(m) != "periodicals" || Renewable(m) {
		t.Errorf("Label = %q, Shelf = %q, Renewable = %v", Label(m), Shelf(m), Renewable(m))
	}
	l := Loan{Item: m, Member: Member{ID: "p"}, Day: 0}
	if got, want := Settle([]Loan{l}, 20), []Charge{{Member: "p", Item: "v", Fee: 80}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Settle = %v, want %v", got, want)
	}
	if !Warn(l, 15) || Warn(l, 17) {
		t.Errorf("Warn")
	}
	c := &Catalog{}
	if err := Import(c, "v|Vogue|magazine|3"); err != nil {
		t.Fatal(err)
	}
	if c.Items["v"].Kind != "magazine" {
		t.Errorf("Import = %v", c.Items)
	}
	b, d, a := Item{Title: "Emma", Kind: "book"}, Item{Title: "Heat", Kind: "dvd"}, Item{Title: "World", Kind: "atlas"}
	if LoanDays(b) != 21 || DailyFee(d) != 50 || Label(a) != "WORLD (reference)" || Shelf(b) != "stacks" || !Renewable(d) {
		t.Errorf("other kinds changed")
	}
}
