package pricing

import (
	"testing"

	"example.com/shop/order"
)

func TestAccept2Student(t *testing.T) {
	for _, tc := range []struct {
		name string
		line order.Line
		want int
	}{
		{"retail price, 15% off, 10% tax", order.Line{Qty: 2, UnitCents: 1000}, 1870},
		{"retail quantity discount first", order.Line{Qty: 10, UnitCents: 2000}, 16830},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := order.Order{CustomerKind: "student", Lines: []order.Line{tc.line}}
			if got := Total(o); got != tc.want {
				t.Errorf("Total = %d, want %d", got, tc.want)
			}
		})
	}
	partner := order.Order{CustomerKind: "partner", Lines: []order.Line{{Qty: 2, UnitCents: 1000}}}
	if got := Total(partner); got != 1920 {
		t.Errorf("partner Total = %d, want 1920 as before", got)
	}
}
