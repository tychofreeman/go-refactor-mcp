package lending

import (
	"reflect"
	"testing"
)

func TestAcceptPhases(t *testing.T) {
	recs, err := ParseRecords("a|Emma|book|2\n\n# films\nb|Heat|dvd|1")
	if err != nil {
		t.Fatal(err)
	}
	want := []Record{{Line: 1, ID: "a", Title: "Emma", Kind: "book", Copies: 2}, {Line: 4, ID: "b", Title: "Heat", Kind: "dvd", Copies: 1}}
	if !reflect.DeepEqual(recs, want) {
		t.Errorf("ParseRecords = %v, want %v", recs, want)
	}
	if _, err := ParseRecords("a|Emma|book"); err == nil || err.Error() != "line 1: want 4 fields, got 3" {
		t.Errorf("ParseRecords of 3 fields: %v", err)
	}
	if _, err := ParseRecords("a|Emma|book|x"); err == nil || err.Error()[:14] != "line 1: copie" + "s" {
		t.Errorf("ParseRecords of bad copies: %v", err)
	}
	c := &Catalog{Items: map[string]Item{"a": {ID: "a", Title: "Old", Kind: "book", Copies: 1}}}
	if err := ValidateRecords(c, recs); err == nil || err.Error() != "line 1: a is already there" {
		t.Errorf("ValidateRecords of a duplicate: %v", err)
	}
	for _, r := range []struct {
		rec  Record
		want string
	}{
		{Record{Line: 3, ID: "z", Kind: "vinyl", Copies: 1}, `line 3: unknown kind "vinyl"`},
		{Record{Line: 5, Kind: "book", Copies: 1}, "line 5: no id"},
		{Record{Line: 6, ID: "y", Kind: "book"}, "line 6: no copies"},
	} {
		if err := ValidateRecords(&Catalog{}, []Record{r.rec}); err == nil || err.Error() != r.want {
			t.Errorf("ValidateRecords(%v) = %v, want %s", r.rec, err, r.want)
		}
	}
	empty := &Catalog{}
	if err := ValidateRecords(empty, recs); err != nil {
		t.Fatal(err)
	}
	ApplyRecords(empty, recs)
	if got := empty.Items["b"]; got != (Item{ID: "b", Title: "Heat", Kind: "dvd", Copies: 1}) {
		t.Errorf("ApplyRecords added %v", got)
	}
	if err := Import(empty, "c|Odes|book|1\nc|Odes|book|1"); err == nil || err.Error() != "line 2: c is already there" {
		t.Errorf("Import of a repeated id: %v", err)
	}
	if len(empty.Items) != 2 {
		t.Errorf("a failed Import added items")
	}
}
