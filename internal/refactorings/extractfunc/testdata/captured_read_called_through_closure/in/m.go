package m

func first(xs []int) int {
	var found []int
	visit := func(x int) { found = append(found, x) }
	again := func() { visit(1) }
	for _, x := range xs {
		visit(x)
	}
	again()
	a := found[0]
	return a + len(found)
}
