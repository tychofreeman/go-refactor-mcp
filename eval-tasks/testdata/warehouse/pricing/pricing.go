// Package pricing prices storage and handling for customers.
package pricing

import "math"

// margin is the share added to every cost.
var margin = 0.25

// Rate is a customer's price list.
type Rate struct {
	Customer string
	PerDay   float64
	markup   float64 // a handling fee added to each order
}

// NewRate returns a price list with a handling fee.
func NewRate(customer string, perDay, fee float64) Rate {
	return Rate{Customer: customer, PerDay: perDay, markup: fee}
}

// Price adds the margin to cost.
func Price(cost float64) float64 {
	return round(cost * (1 + margin))
}

// Storage prices days of storage at r.
func (r Rate) Storage(days int) float64 {
	return Price(float64(days)*r.PerDay) + r.markup
}

// Quote prices cost and adds a handling fee.
func Quote(cost, markup float64) float64 {
	return Price(cost) + markup
}

// Bulk prices n units at cost each, with the margin on the whole order, and
// adds a handling fee once.
func Bulk(cost float64, n int, markup float64) float64 {
	total := float64(n) * cost
	return round(total*(1+margin)) + markup
}

// Reduced prices cost with the margin reduced by off, and never below cost.
func Reduced(cost, off float64) float64 {
	markup := margin - off
	if markup < 0 {
		markup = 0
	}
	return round(cost * (1 + markup))
}

// Floor is the least price for cost: the margin on cost, at least one unit.
func Floor(cost float64) float64 {
	return math.Max(1, round(cost*margin))
}

func round(x float64) float64 {
	return math.Round(x*100) / 100
}
