package lending

import "testing"

func TestAcceptAddress(t *testing.T) {
	m := Member{ID: "m", Name: "Ann", Home: Address{Street: "1 Elm", City: "Oslo", Zip: "0150"}}
	if m.Home.City != "Oslo" {
		t.Errorf("Home.City = %q", m.Home.City)
	}
	if got := Postal(m); got != "1 Elm, 0150 Oslo" {
		t.Errorf("Postal = %q", got)
	}
}
