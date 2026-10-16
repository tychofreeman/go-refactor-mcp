package m

func Sum(xs []int, step int) int {
	t := 0
	for i := 0; i < len(xs); i += step * 2 {
		t += xs[i]
	}
	return t
}
