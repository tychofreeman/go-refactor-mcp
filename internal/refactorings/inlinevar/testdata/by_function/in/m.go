package m

func Total(price, qty int) int {
	subtotal := price * qty
	return subtotal + 5
}
