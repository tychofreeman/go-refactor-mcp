package m

const scale = 10

func f(n int) int {
	return n * scale
}

func Use(scale int) int { return f(scale) }
