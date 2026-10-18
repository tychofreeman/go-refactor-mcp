package export

import (
	"encoding/json"
	"fmt"
	"sort"

	"example.com/shop/order"
	"example.com/shop/pricing"
)

// JSON lists the orders that are not pending as a JSON array, by ID.
func JSON(orders []order.Order) string {
	var list []order.Order
	for _, o := range orders {
		if o.Status != order.Pending {
			list = append(list, o)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	type row struct {
		ID       string `json:"id"`
		Customer string `json:"customer"`
		City     string `json:"city"`
		Total    string `json:"total"`
	}
	rows := []row{}
	for _, o := range list {
		t := pricing.Total(o)
		rows = append(rows, row{o.ID, o.Customer, o.City, fmt.Sprintf("%d.%02d", t/100, t%100)})
	}
	b, _ := json.MarshalIndent(rows, "", "  ")
	return string(b) + "\n"
}
