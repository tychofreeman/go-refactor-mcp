package m

func Use(xs []int) int {
	v := -1
loop:
	for _, x := range xs {
		if x > 0 {
			v = x
			break loop
		}
	}
	return v + 1
}
