package m

func Has(set map[uint8]bool, n uint) bool {
	return set[1<<n]
}
