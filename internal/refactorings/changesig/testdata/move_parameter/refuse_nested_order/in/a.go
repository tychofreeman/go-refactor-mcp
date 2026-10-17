package m

func f(a, b int) int { return a - b }

func Use(x, y int) int {
	return f(x, f(y, 1))
}
