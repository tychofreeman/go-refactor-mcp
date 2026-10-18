package export

import (
	"encoding/json"
	"testing"

	"example.com/shop/order"
)

var acceptOrders = []order.Order{
	{ID: "Z9", Customer: "Ann", CustomerKind: "retail", City: "X", Status: order.Paid, Lines: []order.Line{{SKU: "a", Qty: 1, UnitCents: 1000}}},
	{ID: "A1", Customer: "Bob", CustomerKind: "retail", City: "X", Status: order.Paid, Lines: []order.Line{{SKU: "b", Qty: 1, UnitCents: 500}}},
	{ID: "C1", Customer: "Al", CustomerKind: "retail", City: "X", Status: order.Cancelled, Lines: []order.Line{{SKU: "c", Qty: 1, UnitCents: 700}}},
	{ID: "B1", Customer: "Ann", CustomerKind: "retail", City: "X", Status: order.Shipped, Lines: []order.Line{{SKU: "d", Qty: 1, UnitCents: 100}}},
	{ID: "D1", Customer: "Aaron", CustomerKind: "retail", City: "X", Status: order.Pending, Lines: []order.Line{{SKU: "e", Qty: 1, UnitCents: 900}}},
}

func TestAcceptCSVLeavesOutCancelledAndSortsByCustomer(t *testing.T) {
	want := "id,customer,city,total\nB1,Ann,X,1.20\nZ9,Ann,X,12.00\nA1,Bob,X,6.00\n"
	if got := CSV(acceptOrders); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestAcceptJSONLeavesOutCancelledAndSortsByCustomer(t *testing.T) {
	var rows []struct{ ID string }
	if err := json.Unmarshal([]byte(JSON(acceptOrders)), &rows); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	if len(ids) != 3 || ids[0] != "B1" || ids[1] != "Z9" || ids[2] != "A1" {
		t.Errorf("ids %v, want [B1 Z9 A1]", ids)
	}
}

func TestAcceptMarkdownLeavesOutCancelledAndSortsByCustomer(t *testing.T) {
	want := "| Order | Customer | Total |\n|---|---|---|\n| B1 | Ann | $1.20 |\n| Z9 | Ann | $12.00 |\n| A1 | Bob | $6.00 |\n\nTotal: $19.20\n"
	if got := Markdown(acceptOrders); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}
