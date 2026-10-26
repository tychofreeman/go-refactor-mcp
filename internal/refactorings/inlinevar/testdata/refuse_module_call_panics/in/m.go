package m

func half(n, d int) int { return n / d }

func f(a, b int) int {
	x := half(a, b)
	println("start")
	return x + x
}
