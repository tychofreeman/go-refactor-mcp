package m

func f(a, b int) int { return a - b }

func two() (int, int) { return 1, 2 }

var X = f(two())
