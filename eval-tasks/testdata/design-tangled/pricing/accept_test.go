package pricing

import (
	"testing"

	"example.com/shop/order"
)

func TestAcceptPartner(t *testing.T) {
	for _, tc := range []struct {
		name string
		line order.Line
		want int
	}{
		{"wholesale price, retail tax", order.Line{Qty: 2, UnitCents: 1000}, 1920},
		{"no quantity discount", order.Line{Qty: 12, UnitCents: 500}, 5760},
		{"retail's lower tax over 1000", order.Line{Qty: 200, UnitCents: 1000}, 188800},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := order.Order{CustomerKind: "partner", Lines: []order.Line{tc.line}}
			if got := Total(o); got != tc.want {
				t.Errorf("Total = %d, want %d", got, tc.want)
			}
		})
	}
}
