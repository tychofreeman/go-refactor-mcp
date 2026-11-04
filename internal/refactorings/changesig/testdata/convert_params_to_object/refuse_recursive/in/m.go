package m

// count counts down from n by step.
func count(n, step int) int {
	if n <= 0 {
		return 0
	}
	return 1 + count(n-step, step)
}
