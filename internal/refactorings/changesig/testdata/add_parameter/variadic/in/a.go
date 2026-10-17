package m

func sum(label string, xs ...int) int {
	t := 0
	for _, x := range xs {
		t += x
	}
	return t
}

func Use(ys []int) int {
	return sum("a") + sum("b", 1, 2) + sum("c", ys...)
}
