package m

func F(a, b int) int {
	x := a + b
	if x > 0 {
		sum := 1
		return sum + (a + b)
	}
	return x
}
