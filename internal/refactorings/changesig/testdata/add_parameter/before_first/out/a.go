package m

func add(verbose bool, a, b int) int { return a + b }

func Use() int { return add(false, 1, 2) }
