package m

func Sum(xs []int) int {
	t := 0
	for i := 0; i < len(xs); i++ {
		t += xs[i]
	}
	return t
}
