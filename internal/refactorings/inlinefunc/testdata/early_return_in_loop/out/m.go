package m

func Use(xs []int) int {
	var v int
	var returned bool
loop:
	for _, x := range xs {
		if x > 0 {
			v = x
			returned = true
			break loop
		}
	}
	if !returned {
		v = -1
	}
	return v + 1
}
