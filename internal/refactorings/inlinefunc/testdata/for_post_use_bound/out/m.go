package m

type T struct{ n int }

func caller(p *T, k int) int {
	b := p.n
	for i := 0; i < k; i += b {
		return i
	}
	return -1
}
