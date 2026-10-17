package m

func f(a int, s string, xs ...int) int { return a + len(s) + len(xs) }

func Use(v []int) int {
	return f(1, "x") + f(2, "y", 3, 4) + f(5, "z", v...)
}
