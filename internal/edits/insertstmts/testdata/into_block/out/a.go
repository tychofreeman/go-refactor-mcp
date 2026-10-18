package m

func count(xs []int) int {
	n := 0
	for _, x := range xs {
		if x > 0 {
			x--
			n++
		}
	}
	return n
}
