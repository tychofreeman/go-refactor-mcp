package m

func next() int { return 1 }

func f(b, a int) int { return a - b }

func Use(p *int, x int) int {
	return f(2, next()) + f(*p, x) + f(x, *p)
}
