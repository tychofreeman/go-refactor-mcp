package m

func f(m map[string]int) int {
	m["a"] = 1
	return m["a"]
}

func Use() int { return f(map[string]int{}) }
