package m

func scale(n, by int) int {
	return n * by
}

func Use() int {
	return scale(2, 3) + scale(4, 5)
}
