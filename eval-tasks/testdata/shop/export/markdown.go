package export

import (
	"fmt"
	"sort"
	"strings"

	"example.com/shop/order"
	"example.com/shop/pricing"
)

// Markdown lists the orders that are not pending as a table, by ID, with
// the grand total under it.
func Markdown(orders []order.Order) string {
	var list []order.Order
	for _, o := range orders {
		if o.Status == order.Pending {
			continue
		}
		list = append(list, o)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	var sb strings.Builder
	sb.WriteString("| Order | Customer | Total |\n|---|---|---|\n")
	sum := 0
	for _, o := range list {
		t := pricing.Total(o)
		sum += t
		fmt.Fprintf(&sb, "| %s | %s | $%d.%02d |\n", o.ID, o.Customer, t/100, t%100)
	}
	fmt.Fprintf(&sb, "\nTotal: $%d.%02d\n", sum/100, sum%100)
	return sb.String()
}
