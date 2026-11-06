package m

func k(b int) int {
	try := func(a int) int { return a * b }
	return try(1) + try(2)
}
