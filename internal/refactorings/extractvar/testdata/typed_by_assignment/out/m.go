package m

func Bit(n uint) uint8 {
	var m uint8
	var bit uint8 = 1 << n
	m = bit
	return m
}
