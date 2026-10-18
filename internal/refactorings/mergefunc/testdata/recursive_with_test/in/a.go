package m

func fact(n int) int {
	if n == 0 {
		return 1
	}
	return n * fact(n-1)
}

func factorial(k int) int {
	if k == 0 {
		return 1
	}
	return k * factorial(k-1)
}
