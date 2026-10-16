package m

func store(b uint8) uint8 { return b }

func Bit(n uint) uint8 {
	var bit uint8 = 1 << n
	return store(bit)
}
