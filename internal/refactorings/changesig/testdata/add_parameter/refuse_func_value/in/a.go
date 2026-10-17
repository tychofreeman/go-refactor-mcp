package m

func inc(n int) int { return n + 1 }

func Use() int {
	f := inc
	return f(1) + inc(2)
}
