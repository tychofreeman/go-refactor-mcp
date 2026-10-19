package m

// Sum adds the values.
func Sum(xs []int) int {
	t := 0
	for _, x := range xs {
		t += x
	}
	return t
}

func Other() int {
	t := 5 // a different t
	return t
}
