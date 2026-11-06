package m

func first(xs []int) int {
	var found []int
	visit := func(x int) { found = append(found, x) }
	for _, x := range xs {
		visit(x)
	}
	b := next(found)
	visit(b)
	return b + len(found)
}

func next(found []int) int {
	a := found[0]
	b := a + 1
	return b
}
