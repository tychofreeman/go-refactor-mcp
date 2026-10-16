package m

// KiB converts n to kibibytes.
func KiB(n float64) float64 {
	return n * (1 << 10)
}
