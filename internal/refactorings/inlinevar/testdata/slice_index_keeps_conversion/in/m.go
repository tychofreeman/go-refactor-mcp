package m

func At(xs []int, n uint) int {
	var i uint8 = 1 << n
	return xs[i]
}
