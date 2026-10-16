package m

// Sum adds the values.
func Sum(xs []int) int {
	total := 0
	for _, x := range xs {
		total += x
	}
	return total
}

func Other() int {
	t := 5 // a different t
	return t
}
