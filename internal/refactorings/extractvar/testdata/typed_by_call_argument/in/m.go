package m

func store(b uint8) uint8 { return b }

func Bit(n uint) uint8 {
	return store(1 << n)
}
