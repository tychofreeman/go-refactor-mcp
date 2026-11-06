package m

func first(xs []int) int {
	var found []int
	visit := func(x int) { found = append(found, x) }
	for _, x := range xs {
		visit(x)
	}
	a := found[0]
	b := a + 1
	visit(b)
	return b + len(found)
}
