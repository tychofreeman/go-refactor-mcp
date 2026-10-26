package m

func double(n int) int { return n * 2 }

func f(a int) int {
	x := double(a)
	return x + x
}
