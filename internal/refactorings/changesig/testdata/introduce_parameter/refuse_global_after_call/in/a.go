package m

var count int

func bump() { count++ }

func f(n int) int {
	bump()
	return n + count
}

func Use() int { return f(1) }
