package m

func count(xs []int) int {
	n := 0
	for _, x := range xs {
		if x > 0 {
			n++
		}
	}
	if n > 10 {
		n = 10
	}
	return n
}
