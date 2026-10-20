package m

func run(x int) int { return twice(twice(x) + 1) }

func twice(n int) int { return n * 2 }
