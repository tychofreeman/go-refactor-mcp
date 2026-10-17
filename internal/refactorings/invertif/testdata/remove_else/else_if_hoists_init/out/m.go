package m

func run(m map[string]int) int {
	v, ok := m["k"]
	if !ok {
		return 0
	}
	if v > 3 {
		return 1
	}
	return 2
}
