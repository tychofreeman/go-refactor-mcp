package m

var count int

func f(n, v int) int {
	return v
}

func Use() int { return f(1, 1+count) }
