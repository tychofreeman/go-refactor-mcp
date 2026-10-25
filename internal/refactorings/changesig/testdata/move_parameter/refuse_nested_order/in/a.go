package m

func f(a, b int) int { return a - b }

var x int

func Use(y int) int {
	return f(x, f(y, 1))
}
