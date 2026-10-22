package lending

import (
	"reflect"
	"testing"
)

func TestAcceptDryRun(t *testing.T) {
	c := &Catalog{Items: map[string]Item{"a": {ID: "a", Title: "Old", Kind: "book", Copies: 1}}}
	titles, err := DryRun(c, "b|Emma|book|2\n# films\nd|Heat|dvd|1")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Emma", "Heat"}; !reflect.DeepEqual(titles, want) {
		t.Errorf("DryRun = %q, want %q", titles, want)
	}
	if len(c.Items) != 1 {
		t.Errorf("DryRun added items: %v", c.Items)
	}
	for text, want := range map[string]string{
		"a|Again|book|1":   "line 1: a is already there",
		"b|Emma|book":      "line 1: want 4 fields, got 3",
		"\nb|Emma|tape|1": `line 2: unknown kind "tape"`,
	} {
		if _, err := DryRun(c, text); err == nil || err.Error() != want {
			t.Errorf("DryRun(%q) = %v, want %s", text, err, want)
		}
	}
}
