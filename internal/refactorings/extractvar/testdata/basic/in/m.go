package m

func Price(qty, unit, discount int) int {
	return qty*unit - discount
}
