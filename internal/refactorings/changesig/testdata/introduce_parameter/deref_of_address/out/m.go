package m

func h(found *[]int, n, first int) int {
	x := first
	return x + n
}

func use(xs []int) int { return h(&xs, 1, xs[0]) }
