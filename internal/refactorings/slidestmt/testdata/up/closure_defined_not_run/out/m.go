package m

func F() int {
	x := 1
	x = 3
	f := func() int { return x }
	return f()
}
