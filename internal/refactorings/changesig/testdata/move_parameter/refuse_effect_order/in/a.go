package m

var count int

func f(a, b int) int { return a - b }

func next() int { count++; return count }

var X = f(next(), count)
