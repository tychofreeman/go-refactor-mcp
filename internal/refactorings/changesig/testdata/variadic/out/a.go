package m

func sum(label string, base int, xs ...int) int {
	t := 0
	for _, x := range xs {
		t += x
	}
	return t
}

func Use(ys []int) int {
	return sum("a", 0) + sum("b", 0, 1, 2) + sum("c", 0, ys...)
}
