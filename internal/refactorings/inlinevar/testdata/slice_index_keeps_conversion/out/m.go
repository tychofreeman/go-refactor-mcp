package m

func At(xs []int, n uint) int {
	return xs[uint8(1<<n)]
}
