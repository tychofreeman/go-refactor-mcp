package m

func F(n int) int {
	t := 0
	step := n * 2
	for i := 0; i < 3; i++ {
		t += step
		n++
	}
	return t
}
