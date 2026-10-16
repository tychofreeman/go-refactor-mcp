package m

func mapInts(xs []int, f func(int) int) []int {
	out := make([]int, len(xs))
	for i, x := range xs {
		out[i] = f(x)
	}
	return out
}

func Apply(xs []int, k int) []int {
	return mapInts(xs, func(x int) int {
		return x*k + 1
	})
}
