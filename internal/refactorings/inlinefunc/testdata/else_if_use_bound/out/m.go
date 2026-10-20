package m

type T struct{ n int }

func caller(p *T, a bool) int {
	b := p.n
	if a {
		return 1
	} else if b > 0 {
		return 2
	}
	return 0
}
