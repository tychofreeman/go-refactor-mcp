package m

func Price(qty, unit, discount int) int {
	total := 0
	total = qty*unit - discount
	return total
}
