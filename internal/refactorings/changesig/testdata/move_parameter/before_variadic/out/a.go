package m

func f(s string, a int, xs ...int) int { return a + len(s) + len(xs) }

func Use(v []int) int {
	return f("x", 1) + f("y", 2, 3, 4) + f("z", 5, v...)
}
