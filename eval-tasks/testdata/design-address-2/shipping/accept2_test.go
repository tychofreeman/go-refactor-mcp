package shipping

import (
	"testing"

	"example.com/shop/order"
)

func TestAccept2LabelRegion(t *testing.T) {
	us := order.Order{ID: "A1", Customer: "Ann", Address: order.Address{Street: "1 Main St", City: "Springfield", Region: "IL", Postcode: "12345", Country: "US"}}
	if got, err := Ship(us); err != nil || got != "Ann\n1 Main St\n12345 Springfield, IL\n" {
		t.Errorf("US label with region %q, %v", got, err)
	}
	gb := order.Order{ID: "B2", Customer: "Bea", Address: order.Address{Street: "10 Downing St", City: "London", Postcode: "SW1A 2AA", Country: "GB"}}
	if got, err := Ship(gb); err != nil || got != "Bea\n10 Downing St\nSW1A 2AA London\nGB\n" {
		t.Errorf("GB label without region %q, %v", got, err)
	}
}
