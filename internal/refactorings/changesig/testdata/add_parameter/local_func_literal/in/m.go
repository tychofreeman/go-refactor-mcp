package m

func k() int {
	try := func(a int) int { return a * 2 }
	return try(1) + try(2)
}
