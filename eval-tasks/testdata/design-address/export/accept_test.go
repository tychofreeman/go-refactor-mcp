package export

import (
	"strings"
	"testing"

	"example.com/shop/order"
)

func TestAcceptExportsShowTheCity(t *testing.T) {
	orders := []order.Order{{ID: "A1", Customer: "Ann", CustomerKind: "retail", Status: order.Paid,
		Address: order.Address{City: "Springfield", Country: "US"}, Lines: []order.Line{{SKU: "pen", Qty: 2, UnitCents: 150}}}}
	if got := CSV(orders); !strings.Contains(got, "A1,Ann,Springfield,3.60") {
		t.Errorf("CSV lost the city:\n%s", got)
	}
}
