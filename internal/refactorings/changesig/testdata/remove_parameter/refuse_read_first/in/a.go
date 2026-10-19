package m

func g(n, last int) int {
	total := n + last
	last = total
	return last
}

var X = g(1, 2)
