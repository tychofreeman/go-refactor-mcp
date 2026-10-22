package lending

import (
	"reflect"
	"testing"
)

func TestAcceptKinds(t *testing.T) {
	var _ Kind = Book{}
	var _ Kind = DVD{}
	var _ Kind = Atlas{}
	b, d, a := Item{Title: "Emma", Kind: Book{}}, Item{Title: "Heat", Kind: DVD{}}, Item{Title: "World", Kind: Atlas{}}
	if LoanDays(b) != 21 || LoanDays(d) != 7 || LoanDays(a) != 3 {
		t.Errorf("LoanDays")
	}
	if DailyFee(b) != 10 || DailyFee(d) != 50 || DailyFee(a) != 100 {
		t.Errorf("DailyFee")
	}
	if Label(b) != "Emma" || Label(d) != "DVD: Heat" || Label(a) != "WORLD (reference)" {
		t.Errorf("Label")
	}
	if Shelf(b) != "stacks" || Shelf(d) != "media" || Shelf(a) != "reference" {
		t.Errorf("Shelf")
	}
	if !Renewable(b) || !Renewable(d) || Renewable(a) {
		t.Errorf("Renewable")
	}
	c := &Catalog{}
	if err := Import(c, "x|Emma|book|1\ny|Heat|dvd|1\nz|World|atlas|1"); err != nil {
		t.Fatal(err)
	}
	if c.Items["x"].Kind != (Book{}) || c.Items["y"].Kind != (DVD{}) || c.Items["z"].Kind != (Atlas{}) {
		t.Errorf("Import made kinds %v", c.Items)
	}
	if err := Import(c, "q|Q|vinyl|1"); err == nil || err.Error() != `line 1: unknown kind "vinyl"` {
		t.Errorf("Import of an unknown kind: %v", err)
	}
	got := Settle([]Loan{{Item: Item{ID: "i", Kind: DVD{}}, Member: Member{ID: "m"}, Day: 0}}, 12)
	if want := []Charge{{Member: "m", Item: "i", Fee: 150}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Settle = %v, want %v", got, want)
	}
}
