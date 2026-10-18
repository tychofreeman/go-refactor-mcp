package m

var count int

func f(n int) int {
	return n + count
}

func Use() int { return f(1) }
