package m

func F(xs []int) int {
	n := 0
	k := 3
	if len(xs) > 2 {
		n = 2
	}
	return n + k
}
