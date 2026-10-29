package m

func Use(xs []int) int {
	s := sum(xs)
	return s
}

func sum(xs []int) int {
	total := 0
	for _, x := range xs {
		if x < 0 {
			return -1
		}
		total += x
	}
	return total
}
