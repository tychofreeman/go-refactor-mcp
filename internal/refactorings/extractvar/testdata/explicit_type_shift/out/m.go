package m

func Mask(n uint) uint8 {
	var bit uint8 = 1 << n
	var m uint8 = bit
	return m
}
