package m

func f(a, b int) int { return a - b }

func Use() int {
	x := 1
	inc := func() int { x++; return x }
	return f(x, inc())
}
