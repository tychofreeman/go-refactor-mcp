package m

func next() int { return 1 }

func f(a, b int) int { return a - b }

func Use(p *int, x int) int {
	return f(next(), 2) + f(x, *p) + f(*p, x)
}
