package m

func pair(x, y int) int { return x * y }

func Sum2(a, b int) int {
	sum := a + b
	return pair(a+b, sum)
}
