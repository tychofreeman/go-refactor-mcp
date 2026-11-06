package m

func h(found *[]int, n int) int {
	x := (*found)[0]
	return x + n
}

func use(xs []int) int { return h(&xs, 1) }
