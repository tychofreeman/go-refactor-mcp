package m

func F(x any, xs []int) int {
	k := 0
	n := xs[0]
	switch v := x.(type) {
	case int:
		k = v
	}
	return k + n
}
