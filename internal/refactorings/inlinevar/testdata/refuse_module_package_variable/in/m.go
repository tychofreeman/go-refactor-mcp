package m

var rate = 2

func scaled(n int) int { return n * rate }

func f(a int) int {
	x := scaled(a)
	rate++
	return x + x
}
