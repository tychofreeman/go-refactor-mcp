package export

import (
	"encoding/json"
	"testing"

	"example.com/shop/order"
)

var accept2Orders = []order.Order{
	{ID: "B2", Customer: "Bob", CustomerKind: "retail", City: "X", Status: order.Paid, Lines: []order.Line{{SKU: "b", Qty: 1, UnitCents: 500}}},
	{ID: "E5", Customer: "Ann", CustomerKind: "retail", City: "X", Status: order.Paid},
	{ID: "A1", Customer: "Ann", CustomerKind: "retail", City: "X", Status: order.Shipped, Lines: []order.Line{{SKU: "a", Qty: 1, UnitCents: 100}}},
	{ID: "C1", Customer: "Al", CustomerKind: "retail", City: "X", Status: order.Cancelled, Lines: []order.Line{{SKU: "c", Qty: 1, UnitCents: 700}}},
}

func TestAccept2ExportsLeaveOutEmptyOrders(t *testing.T) {
	if got, want := CSV(accept2Orders), "id,customer,city,total\nA1,Ann,X,1.20\nB2,Bob,X,6.00\n"; got != want {
		t.Errorf("CSV got:\n%s\nwant:\n%s", got, want)
	}
	var rows []struct{ ID string }
	if err := json.Unmarshal([]byte(JSON(accept2Orders)), &rows); err != nil || len(rows) != 2 || rows[0].ID != "A1" || rows[1].ID != "B2" {
		t.Errorf("JSON rows %+v (%v), want A1 and B2", rows, err)
	}
	if got, want := Markdown(accept2Orders), "| Order | Customer | Total |\n|---|---|---|\n| A1 | Ann | $1.20 |\n| B2 | Bob | $6.00 |\n\nTotal: $7.20\n"; got != want {
		t.Errorf("Markdown got:\n%s\nwant:\n%s", got, want)
	}
}
