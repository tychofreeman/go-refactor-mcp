package m

func Use(xs []int) int {
	var s int
	var returned bool
	total := 0
loop:
	for _, x := range xs {
		if x < 0 {
			s = -1
			returned = true
			break loop
		}
		total += x
	}
	if !returned {
		s = total
	}
	return s
}
