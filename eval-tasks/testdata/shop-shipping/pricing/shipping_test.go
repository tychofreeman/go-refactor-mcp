package pricing

import (
	"testing"

	"example.com/shop/order"
)

func TestAccept3Shipping(t *testing.T) {
	for _, tc := range []struct {
		kind string
		line order.Line
		want int
	}{
		{"retail", order.Line{Qty: 2, UnitCents: 1000}, 500},
		{"retail", order.Line{Qty: 10, UnitCents: 600}, 0},
		{"student", order.Line{Qty: 2, UnitCents: 1000}, 500},
		{"student", order.Line{Qty: 10, UnitCents: 600}, 500},
		{"wholesale", order.Line{Qty: 2, UnitCents: 1000}, 0},
		{"partner", order.Line{Qty: 2, UnitCents: 1000}, 0},
		{"staff", order.Line{Qty: 2, UnitCents: 1000}, 0},
	} {
		o := order.Order{CustomerKind: tc.kind, Lines: []order.Line{tc.line}}
		if got := Shipping(o); got != tc.want {
			t.Errorf("Shipping(%s, %v) = %d, want %d", tc.kind, tc.line, got, tc.want)
		}
	}
	o := order.Order{CustomerKind: "retail", Lines: []order.Line{{Qty: 2, UnitCents: 1000}}}
	if got := Total(o); got != 2400 {
		t.Errorf("retail Total = %d, want 2400 as before", got)
	}
}
