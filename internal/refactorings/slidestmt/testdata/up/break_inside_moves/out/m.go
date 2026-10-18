package m

func F(xs []int) int {
	n := 0
	k := 7
	for _, x := range xs {
		if x > 3 {
			break
		}
		n += x
	}
	return n + k
}
