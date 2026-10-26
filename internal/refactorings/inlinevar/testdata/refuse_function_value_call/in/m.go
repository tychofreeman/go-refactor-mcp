package m

func f(op func(int) int, a int) int {
	x := op(a)
	return x + x
}
