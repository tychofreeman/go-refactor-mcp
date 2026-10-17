package m

func add(a, b int) int { return a + b }

func pair() (int, int) { return 1, 2 }

func Use() int { return add(pair()) }
