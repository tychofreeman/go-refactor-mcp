package m

func double(n, t int) int {
	if n > 0 {
		t := n * 2
		return t
	}
	return 0
}

func Use() int { return double(2, 0) }
