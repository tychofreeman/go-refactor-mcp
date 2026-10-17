package m

func run(xs []int, total int) int {
	for _, x := range xs {
		total += x
	}
	return total
}
