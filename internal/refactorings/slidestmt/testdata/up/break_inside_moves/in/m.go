package m

func F(xs []int) int {
	n := 0
	for _, x := range xs {
		if x > 3 {
			break
		}
		n += x
	}
	k := 7
	return n + k
}
