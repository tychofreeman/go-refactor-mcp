package lending

import (
	"reflect"
	"testing"
)

func TestKinds(t *testing.T) {
	b, d, a := Item{Title: "Emma", Kind: "book"}, Item{Title: "Heat", Kind: "dvd"}, Item{Title: "World", Kind: "atlas"}
	if LoanDays(b) != 21 || LoanDays(d) != 7 || LoanDays(a) != 3 {
		t.Errorf("LoanDays")
	}
	if Label(d) != "DVD: Heat" || Label(a) != "WORLD (reference)" || Shelf(a) != "reference" || Renewable(a) {
		t.Errorf("Label, Shelf or Renewable")
	}
}

func TestSettle(t *testing.T) {
	l := Loan{Item: Item{ID: "i", Kind: "dvd"}, Member: Member{ID: "m"}, Day: 0}
	if got, want := Settle([]Loan{l}, 12), []Charge{{Member: "m", Item: "i", Fee: 150}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Settle = %v, want %v", got, want)
	}
	if got := Settle([]Loan{l}, 30); got[0].Fee != 500 {
		t.Errorf("Settle capped = %v", got)
	}
	if !Warn(l, 9) || Warn(l, 10) {
		t.Errorf("Warn")
	}
}

func TestImport(t *testing.T) {
	c := &Catalog{}
	if err := Import(c, "a|Emma|book|2\n\n# films\nb|Heat|dvd|1"); err != nil {
		t.Fatal(err)
	}
	if got := Titles(c); !reflect.DeepEqual(got, []string{"Emma", "Heat"}) {
		t.Errorf("Titles = %q", got)
	}
	for text, want := range map[string]string{
		"x|X|book":       "line 1: want 4 fields, got 3",
		"x|X|vinyl|1":    `line 1: unknown kind "vinyl"`,
		"\nx|X|book|0":   "line 2: no copies",
		"a|Again|book|1": "line 1: a is already there",
	} {
		if err := Import(c, text); err == nil || err.Error() != want {
			t.Errorf("Import(%q) = %v, want %s", text, err, want)
		}
	}
	if len(c.Items) != 2 {
		t.Errorf("a failed import added items: %v", c.Items)
	}
}

func TestPostal(t *testing.T) {
	if got := Postal(Member{Street: "1 Elm", City: "Oslo", Zip: "0150"}); got != "1 Elm, 0150 Oslo" {
		t.Errorf("Postal = %q", got)
	}
}
