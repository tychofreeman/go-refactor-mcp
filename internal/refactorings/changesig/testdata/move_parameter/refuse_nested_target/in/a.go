package m

func f(a, b int) int { return a - b }

var X = f(1, f(2, 3))
