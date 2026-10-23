package lending

import "testing"

func TestAcceptDeposit(t *testing.T) {
	for kind, want := range map[string]int{"dvd": 200, "atlas": 500, "map": 300, "book": 0, "magazine": 0, "ebook": 0} {
		if got := Deposit(Item{Title: "T", Kind: kind}); got != want {
			t.Errorf("Deposit(%s) = %d, want %d", kind, got, want)
		}
	}
	if LoanDays(Item{Kind: "ebook"}) != 21 || Shelf(Item{Kind: "dvd"}) != "media" {
		t.Errorf("other rules changed")
	}
}
