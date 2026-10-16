package m

func two() (int, int)  { return 1, 2 }
func add(a, b int) int { return a + b }

func F() int {
	return add(two())
}
