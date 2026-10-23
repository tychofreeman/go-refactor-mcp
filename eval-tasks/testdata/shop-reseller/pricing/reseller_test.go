package pricing

import (
	"testing"

	"example.com/shop/order"
)

func TestAccept4Reseller(t *testing.T) {
	for _, tc := range []struct {
		line order.Line
		want int
	}{
		{order.Line{Qty: 2, UnitCents: 1000}, 1680},
		{order.Line{Qty: 10, UnitCents: 1000}, 8232},
		{order.Line{Qty: 100, UnitCents: 1000}, 79800},
	} {
		o := order.Order{CustomerKind: "reseller", Lines: []order.Line{tc.line}}
		if got := Total(o); got != tc.want {
			t.Errorf("Total(%v) = %d, want %d", tc.line, got, tc.want)
		}
		if got := Shipping(o); got != 0 {
			t.Errorf("Shipping(%v) = %d, want 0", tc.line, got)
		}
	}
}
