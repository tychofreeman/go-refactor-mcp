package m

func Use(xs []int) int {
	v := find(xs)
	return v + 1
}

func find(xs []int) int {
	for _, x := range xs {
		if x > 0 {
			return x
		}
	}
	println("none")
	return -1
}
