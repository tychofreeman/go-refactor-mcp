package m

func Send(ch chan uint8, n uint) {
	var bit uint8 = 1 << n
	ch <- bit
}
