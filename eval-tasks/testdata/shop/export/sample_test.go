package export

import "example.com/shop/order"

// sample is a mix of customers and statuses.
var sample = []order.Order{
	{ID: "D4", Customer: "Di", CustomerKind: "retail", City: "Ogdenville", Status: order.Cancelled, Lines: []order.Line{{SKU: "pad", Qty: 1, UnitCents: 1000}}},
	{ID: "A1", Customer: "Ann", CustomerKind: "retail", City: "Springfield", Status: order.Paid, Lines: []order.Line{{SKU: "pen", Qty: 2, UnitCents: 150}}},
	{ID: "C3", Customer: "Cy", CustomerKind: "staff", City: "Springfield", Status: order.Pending, Lines: []order.Line{{SKU: "pen", Qty: 1, UnitCents: 150}}},
	{ID: "B2", Customer: "Bob", CustomerKind: "wholesale", City: "Shelbyville", Status: order.Shipped, Lines: []order.Line{{SKU: "ink", Qty: 12, UnitCents: 500}}},
}
