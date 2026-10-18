package pricing

import (
	"testing"

	"example.com/shop/order"
)

func TestTotal(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind string
		line order.Line
		want int
	}{
		{"retail", "retail", order.Line{Qty: 2, UnitCents: 150}, 360},
		{"retail, 10 cheap", "retail", order.Line{Qty: 10, UnitCents: 500}, 5700},
		{"retail, 10 dear", "retail", order.Line{Qty: 10, UnitCents: 2000}, 21600},
		{"retail, lower tax over 1000", "retail", order.Line{Qty: 100, UnitCents: 2000}, 212400},
		{"wholesale", "wholesale", order.Line{Qty: 2, UnitCents: 1000}, 1600},
		{"wholesale, 10", "wholesale", order.Line{Qty: 12, UnitCents: 500}, 4704},
		{"wholesale, 100", "wholesale", order.Line{Qty: 100, UnitCents: 1000}, 76000},
		{"staff", "staff", order.Line{Qty: 3, UnitCents: 1000}, 2310},
		{"staff, 10", "staff", order.Line{Qty: 10, UnitCents: 1000}, 7700},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := order.Order{CustomerKind: tc.kind, Lines: []order.Line{tc.line}}
			if got := Total(o); got != tc.want {
				t.Errorf("Total = %d, want %d", got, tc.want)
			}
		})
	}
}
