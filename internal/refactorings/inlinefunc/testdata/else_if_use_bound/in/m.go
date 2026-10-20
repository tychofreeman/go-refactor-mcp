package m

type T struct{ n int }

func f(a bool, b int) int {
	if a {
		return 1
	} else if b > 0 {
		return 2
	}
	return 0
}

func caller(p *T, a bool) int {
	return f(a, p.n)
}
