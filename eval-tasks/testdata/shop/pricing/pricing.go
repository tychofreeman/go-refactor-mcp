// Package pricing computes what an order costs.
package pricing

import "example.com/shop/order"

// Total is what the customer pays for o, in cents: the lines' prices after
// discounts, plus tax.
func Total(o order.Order) int {
	subtotal := 0
	for _, l := range o.Lines {
		price := l.UnitCents
		if o.CustomerKind == "wholesale" {
			price = price * 80 / 100
			if l.Qty >= 100 {
				price = price * 95 / 100
			} else if l.Qty >= 10 {
				price = price * 98 / 100
			}
		} else if o.CustomerKind == "staff" {
			price = price * 70 / 100
		} else {
			if l.Qty >= 10 {
				if l.UnitCents > 1000 {
					price = price * 90 / 100
				} else {
					price = price * 95 / 100
				}
			}
		}
		subtotal += price * l.Qty
	}
	tax := 0
	if o.CustomerKind == "wholesale" {
		tax = 0
	} else if o.CustomerKind == "staff" {
		tax = subtotal * 10 / 100
	} else {
		tax = subtotal * 20 / 100
		if subtotal > 100000 {
			tax = subtotal * 18 / 100
		}
	}
	return subtotal + tax
}
