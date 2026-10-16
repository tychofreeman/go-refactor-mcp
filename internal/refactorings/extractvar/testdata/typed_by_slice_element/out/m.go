package m

func Bits(n uint) []uint8 {
	var bit uint8 = 1 << n
	return []uint8{bit, 0}
}
