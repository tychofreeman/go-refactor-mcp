package m

func F(x any, xs []int) int {
	k := 0
	switch v := x.(type) {
	case int:
		k = v
	}
	n := xs[0]
	return k + n
}
