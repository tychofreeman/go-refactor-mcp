// Package order holds the shop's orders.
package order

// Status is where an order is in its life.
type Status int

const (
	Pending Status = iota
	Paid
	Shipped
	Cancelled
)

// Line is one item of an order.
type Line struct {
	SKU       string
	Qty       int
	UnitCents int // price of one unit, before discounts and tax
}

// Order is a customer's order.
type Order struct {
	ID           string
	Customer     string
	CustomerKind string // "retail", "wholesale" or "staff"
	Street       string
	City         string
	Postcode     string
	Status       Status
	Lines        []Line
}
