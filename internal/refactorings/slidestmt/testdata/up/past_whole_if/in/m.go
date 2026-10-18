package m

func F(xs []int) int {
	n := 0
	if len(xs) > 2 {
		n = 2
	}
	k := 3
	return n + k
}
