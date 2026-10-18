package m

func F(a, b map[string]int) int {
	x, ok := a["k"]
	y, ok := b["k"]
	if ok {
		return x + y
	}
	return 0
}
