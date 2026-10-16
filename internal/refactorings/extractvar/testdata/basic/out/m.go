package m

func Price(qty, unit, discount int) int {
	subtotal := qty * unit
	return subtotal - discount
}
