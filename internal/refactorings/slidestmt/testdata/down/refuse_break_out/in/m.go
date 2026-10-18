package m

func F(xs []int) int {
	n := 0
	for range xs {
		if n > 3 {
			break
		}
		n++
	}
	return n
}
