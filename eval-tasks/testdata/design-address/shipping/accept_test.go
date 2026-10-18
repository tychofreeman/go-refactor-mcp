package shipping

import (
	"strings"
	"testing"

	"example.com/shop/order"
)

func TestAcceptValidAddressCountry(t *testing.T) {
	us := order.Address{Street: "1 Main St", City: "Springfield", Postcode: "12345", Country: "US"}
	if err := ValidAddress(us); err != nil {
		t.Errorf("valid US address refused: %v", err)
	}
	gb := order.Address{Street: "10 Downing St", City: "London", Postcode: "SW1A 2AA", Country: "GB"}
	if err := ValidAddress(gb); err != nil {
		t.Errorf("valid GB address refused: %v", err)
	}
	noCountry := us
	noCountry.Country = ""
	if err := ValidAddress(noCountry); err == nil || !strings.Contains(err.Error(), "country") {
		t.Errorf("address without a country: %v, want an error naming the country", err)
	}
	shortUS := us
	shortUS.Postcode = "1234"
	if err := ValidAddress(shortUS); err == nil {
		t.Error("US postcode of 4 digits accepted")
	}
}

func TestAcceptLabelCountry(t *testing.T) {
	us := order.Order{ID: "A1", Customer: "Ann", Address: order.Address{Street: "1 Main St", City: "Springfield", Postcode: "12345", Country: "US"}}
	if got, err := Ship(us); err != nil || got != "Ann\n1 Main St\n12345 Springfield\n" {
		t.Errorf("US label %q, %v", got, err)
	}
	gb := order.Order{ID: "B2", Customer: "Bea", Address: order.Address{Street: "10 Downing St", City: "London", Postcode: "SW1A 2AA", Country: "GB"}}
	if got, err := Ship(gb); err != nil || got != "Bea\n10 Downing St\nSW1A 2AA London\nGB\n" {
		t.Errorf("GB label %q, %v", got, err)
	}
}
