package m

func F(x int) (int, func()) {
	a := x + 1
	inc := func() { x++ }
	return a + (x + 1), inc
}
