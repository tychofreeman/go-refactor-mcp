package m

func Adder(base int) func(int) int {
	return func(n int) int { return n + base*2 }
}
