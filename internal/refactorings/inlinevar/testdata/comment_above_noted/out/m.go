package m

func Fee(amount int) int {
	// The fee is a tenth of the amount.
	return amount + amount/10
}
