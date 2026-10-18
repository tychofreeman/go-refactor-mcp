package m

func F() int {
	x := 1
	f := func() int { return x }
	x = 3
	return f()
}
