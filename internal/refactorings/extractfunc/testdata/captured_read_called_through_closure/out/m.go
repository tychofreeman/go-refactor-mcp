package m

func first(xs []int) int {
	var found []int
	visit := func(x int) { found = append(found, x) }
	again := func() { visit(1) }
	for _, x := range xs {
		visit(x)
	}
	a := next(again, &found)
	return a + len(found)
}

func next(again func(), found *[]int) int {
	again()
	a := (*found)[0]
	return a
}
