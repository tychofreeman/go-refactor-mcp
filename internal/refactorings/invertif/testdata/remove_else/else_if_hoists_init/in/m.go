package m

func run(m map[string]int) int {
	if v, ok := m["k"]; !ok {
		return 0
	} else if v > 3 {
		return 1
	}
	return 2
}
