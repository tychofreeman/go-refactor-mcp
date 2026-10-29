package m

func Plant(xs []int) int {
	t := 0
	for _, x := range xs {
		if x < 0 {
			break
		}
		t += x
	}
	return t
}
