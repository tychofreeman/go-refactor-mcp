package m

func f(n int) int {
	println("f")
	return n + 1
}

func run(n, i int) []int {
	xs := make([]int, 3)
	xs[i] = f(n)
	return xs
}
