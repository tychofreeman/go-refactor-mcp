package m

func KiB(n float64) float64 {
	const kib = 1 << 10
	return n * kib
}
