package m

var cache map[string]int

func load() map[string]int {
	println("load")
	return map[string]int{}
}

// The variable assigned to is not read, so load() may run where m is used.
func fill() {
	m := load()
	cache = m
}
