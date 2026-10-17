package m

func f(n, x int) int { return n }

func Use(p *int) int { return f(1, *p) }
