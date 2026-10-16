package m

func Add(x uint8, n uint) uint8 {
	var bit uint8 = 1 << n
	return x + bit
}
