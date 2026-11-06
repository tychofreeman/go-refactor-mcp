package m

func k() int {
	var try func(a int) int
	try = func(a int) int { return a * 2 }
	return try(1)
}
