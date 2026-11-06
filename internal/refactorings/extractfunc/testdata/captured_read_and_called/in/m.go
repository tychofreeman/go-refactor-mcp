package m

func first(xs []int) int {
	var found []int
	visit := func(x int) { found = append(found, x) }
	for _, x := range xs {
		visit(x)
	}
	visit(1)
	a := found[0]
	return a + len(found)
}
