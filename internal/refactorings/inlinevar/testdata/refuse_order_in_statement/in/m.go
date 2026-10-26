package m

func load() int {
	println("load")
	return 3
}
func other() int {
	println("other")
	return 4
}

func F() int {
	v := load()
	return other() + v
}
