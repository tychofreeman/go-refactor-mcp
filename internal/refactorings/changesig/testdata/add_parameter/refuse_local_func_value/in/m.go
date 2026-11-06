package m

func k() int {
	try := func(a int) int { return a * 2 }
	f := try
	return f(1)
}
