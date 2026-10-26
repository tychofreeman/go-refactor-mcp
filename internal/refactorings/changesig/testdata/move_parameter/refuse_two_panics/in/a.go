package m

func f(a, b int) int { return a - b }

func Use(p, q []int) int { return f(p[0], q[0]) }
