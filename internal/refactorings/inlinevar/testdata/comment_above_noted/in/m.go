package m

func Fee(amount int) int {
	// The fee is a tenth of the amount.
	fee := amount / 10
	return amount + fee
}
