package m

func Send(ch chan uint8, n uint) {
	ch <- 1 << n
}
