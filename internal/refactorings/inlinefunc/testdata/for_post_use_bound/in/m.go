package m

type T struct{ n int }

func f(k int, b int) int {
	for i := 0; i < k; i += b {
		return i
	}
	return -1
}

func caller(p *T, k int) int {
	return f(k, p.n)
}
