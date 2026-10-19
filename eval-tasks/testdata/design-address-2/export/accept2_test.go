package export

import (
	"encoding/json"
	"testing"

	"example.com/shop/order"
)

func TestAccept2ExportsShowTheRegion(t *testing.T) {
	orders := []order.Order{{ID: "A1", Customer: "Ann", CustomerKind: "retail", Status: order.Paid,
		Address: order.Address{City: "Springfield", Region: "IL", Country: "US"}, Lines: []order.Line{{SKU: "pen", Qty: 2, UnitCents: 150}}}}
	if got, want := CSV(orders), "id,customer,city,region,total\nA1,Ann,Springfield,IL,3.60\n"; got != want {
		t.Errorf("CSV got:\n%s\nwant:\n%s", got, want)
	}
	var rows []struct{ City, Region string }
	if err := json.Unmarshal([]byte(JSON(orders)), &rows); err != nil || len(rows) != 1 || rows[0].Region != "IL" || rows[0].City != "Springfield" {
		t.Errorf("JSON rows %+v (%v), want city Springfield and region IL", rows, err)
	}
}
