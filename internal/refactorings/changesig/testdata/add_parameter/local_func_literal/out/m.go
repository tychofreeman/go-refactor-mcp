package m

func k() int {
	try := func(a, b int) int { return a * 2 }
	return try(1, 0) + try(2, 0)
}
