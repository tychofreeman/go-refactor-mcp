package m

var cache = map[string]int{}

func load() int {
	cache = map[string]int{}
	return 1
}

// An index expression's operand on the left is evaluated before the
// right-hand side: cache is read before load() replaces it.
func fill() {
	n := load()
	cache["a"] = n
}
