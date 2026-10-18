package shipping

import (
	"strings"
	"testing"

	"example.com/shop/order"
)

func TestValidAddress(t *testing.T) {
	if err := ValidAddress("1 Main St", "Springfield", "12345"); err != nil {
		t.Errorf("valid address refused: %v", err)
	}
	for _, tc := range []struct{ street, city, postcode, want string }{
		{"", "Springfield", "12345", "missing street"},
		{"1 Main St", "", "", "missing city, postcode"},
		{"1 Main St", "Springfield", "1234", "5 digits"},
		{"1 Main St", "Springfield", "1234a", "5 digits"},
	} {
		if err := ValidAddress(tc.street, tc.city, tc.postcode); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ValidAddress(%q, %q, %q) = %v, want an error with %q", tc.street, tc.city, tc.postcode, err, tc.want)
		}
	}
}

func TestShip(t *testing.T) {
	o := order.Order{ID: "A1", Customer: "Ann", Street: "1 Main St", City: "Springfield", Postcode: "12345"}
	label, err := Ship(o)
	if err != nil || label != "Ann\n1 Main St\n12345 Springfield\n" {
		t.Errorf("Ship = %q, %v", label, err)
	}
	o.Postcode = ""
	if _, err := Ship(o); err == nil || !strings.Contains(err.Error(), "order A1") {
		t.Errorf("Ship of a bad address = %v, want an error naming the order", err)
	}
}
