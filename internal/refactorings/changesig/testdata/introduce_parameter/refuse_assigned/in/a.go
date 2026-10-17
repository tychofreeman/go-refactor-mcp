package m

func f(n int) int {
	n++
	return n * 2
}

func Use() int { return f(1) }
