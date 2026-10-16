package m

func Has(set map[uint8]bool, n uint) bool {
	var bit uint8 = 1 << n
	return set[bit]
}
