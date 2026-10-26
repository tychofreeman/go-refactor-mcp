package m

func load() int {
	println("load")
	return 3
}
func other() int {
	println("other")
	return 4
}

func Sum(xs []int) int {
	t := 0
	base := load()
	for _, x := range xs {
		t += x + base
	}
	return t
}
