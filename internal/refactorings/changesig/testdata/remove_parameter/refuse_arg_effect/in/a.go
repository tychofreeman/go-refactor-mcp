package m

func f(n, x int) int { return n }

var count int

func next() int { count++; return count }

var X = f(1, next())
