package pricing

import (
	"testing"

	"example.com/shop/order"
)

func TestAccept5Invoiced(t *testing.T) {
	for kind, want := range map[string]bool{
		"wholesale": true, "partner": true, "reseller": true,
		"retail": false, "staff": false, "student": false,
	} {
		o := order.Order{CustomerKind: kind, Lines: []order.Line{{Qty: 1, UnitCents: 100}}}
		if got := Invoiced(o); got != want {
			t.Errorf("Invoiced(%s) = %v, want %v", kind, got, want)
		}
	}
}
