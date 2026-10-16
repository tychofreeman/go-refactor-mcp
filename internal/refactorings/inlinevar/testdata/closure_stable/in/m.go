package m

func Adder(base int) func(int) int {
	offset := base * 2
	return func(n int) int { return n + offset }
}
