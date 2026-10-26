package m

func load() int {
	println("load")
	return 3
}
func other() int {
	println("other")
	return 4
}

func F(a int) bool {
	v := load()
	return a > 0 && v > 0
}
