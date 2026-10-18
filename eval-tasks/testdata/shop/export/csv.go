// Package export writes orders out for other systems and for people.
package export

import (
	"fmt"
	"sort"
	"strings"

	"example.com/shop/order"
	"example.com/shop/pricing"
)

// CSV lists the orders that are not pending, one per line, by ID.
func CSV(orders []order.Order) string {
	var list []order.Order
	for _, o := range orders {
		if o.Status == order.Pending {
			continue
		}
		list = append(list, o)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	var sb strings.Builder
	sb.WriteString("id,customer,city,total\n")
	for _, o := range list {
		total := pricing.Total(o)
		fmt.Fprintf(&sb, "%s,%s,%s,%d.%02d\n", o.ID, o.Customer, o.City, total/100, total%100)
	}
	return sb.String()
}
