package m

func f(n, x int) int { return n }

func two() (int, int) { return 1, 2 }

var X = f(two())
